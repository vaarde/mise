package engine

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/vaarde/mise/internal/provider"
)

// DefaultParallelism bounds how many locations Mise reads at once. It
// sits well under Square's ~40 req/s ceiling; the client's rate limiter
// is the real guard, this just keeps goroutine count sane.
const DefaultParallelism = 10

// FetchOptions tunes a fetch run.
type FetchOptions struct {
	// Parallelism caps concurrent per-location reads. Zero means
	// DefaultParallelism.
	Parallelism int

	// SkipTypes are resource types the caller handles separately.
	// Locations arrive through ListLocations, so the location type is
	// skipped here rather than read twice.
	SkipTypes []string
}

// FetchedResource is one resource as it exists on the POS right now,
// merged across every location it appears at.
//
// A Square tax charged at twelve locations is one catalog object with
// one ID, so it becomes one FetchedResource listing twelve locations —
// not twelve copies.
type FetchedResource struct {
	Type        string
	Name        string // config name, assigned by the engine
	DisplayName string // the platform's human-readable name
	ProviderID  string
	Version     string
	Properties  map[string]interface{}
	LocationIDs []string // sorted
}

// FullName returns "type.name", the key used in state and in ref().
func (r *FetchedResource) FullName() string {
	return r.Type + "." + r.Name
}

// FetchResult is everything a fetch discovered.
type FetchResult struct {
	Locations []provider.Location
	Resources []*FetchedResource
	Warnings  []string
}

// Total returns the number of resources fetched.
func (r *FetchResult) Total() int { return len(r.Resources) }

// CountsByType returns how many resources of each type were fetched.
func (r *FetchResult) CountsByType() map[string]int {
	counts := map[string]int{}
	for _, res := range r.Resources {
		counts[res.Type]++
	}
	return counts
}

// resourceKey identifies one live resource. Provider IDs are unique per
// type, and this is what collapses the same object seen at many
// locations into a single entry.
type resourceKey struct {
	resourceType string
	providerID   string
}

// Fetch reads the complete live configuration from a provider.
func Fetch(ctx context.Context, p provider.Provider, opts FetchOptions) (*FetchResult, error) {
	locations, err := p.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list locations: %w", err)
	}

	types := filterTypes(p.ResourceTypes(), opts.SkipTypes)

	merged, err := readAllLocations(ctx, p, types, locations, opts.Parallelism)
	if err != nil {
		return nil, err
	}

	resources := assignNames(merged)
	warnings := resolveAllRefs(resources)

	return &FetchResult{
		Locations: locations,
		Resources: resources,
		Warnings:  warnings,
	}, nil
}

// filterTypes removes skipped types, preserving order.
func filterTypes(all, skip []string) []string {
	skipped := make(map[string]bool, len(skip))
	for _, s := range skip {
		skipped[s] = true
	}

	out := make([]string, 0, len(all))
	for _, t := range all {
		if !skipped[t] {
			out = append(out, t)
		}
	}
	return out
}

// aggregate accumulates one resource and the set of locations it was
// seen at.
type aggregate struct {
	resource  *provider.Resource
	locations map[string]bool
}

// readAllLocations reads every resource type at every location, running
// locations in parallel under a semaphore.
func readAllLocations(
	ctx context.Context,
	p provider.Provider,
	types []string,
	locations []provider.Location,
	parallelism int,
) (map[resourceKey]*aggregate, error) {
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}

	// An account with no locations still has a catalog. Reading with an
	// empty location ID means "unscoped", so the configuration is still
	// captured rather than silently coming back empty.
	locationIDs := make([]string, 0, len(locations))
	for _, l := range locations {
		locationIDs = append(locationIDs, l.ID)
	}
	if len(locationIDs) == 0 {
		locationIDs = []string{""}
	}

	var (
		mu       sync.Mutex
		merged   = map[resourceKey]*aggregate{}
		firstErr error
		wg       sync.WaitGroup
		sem      = make(chan struct{}, parallelism)
	)

	// A cancellable context so one failure stops the rest promptly
	// instead of letting every other location run to completion.
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	for _, locationID := range locationIDs {
		wg.Add(1)

		go func(locationID string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-readCtx.Done():
				return
			}

			for _, resourceType := range types {
				resources, err := p.ReadAll(readCtx, resourceType, locationID)
				if err != nil {
					mu.Lock()
					if firstErr == nil {
						firstErr = fmt.Errorf("cannot read %s at location %s: %w",
							resourceType, describeLocation(locationID), err)
						cancel()
					}
					mu.Unlock()
					return
				}

				mu.Lock()
				for _, res := range resources {
					mergeResource(merged, res, locationID)
				}
				mu.Unlock()
			}
		}(locationID)
	}

	wg.Wait()

	// The operator's own Ctrl-C surfaces here as a read failure at
	// whichever location noticed first, which reads like a fault at that
	// location. Report the cancellation itself instead.
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if firstErr != nil {
		return nil, firstErr
	}
	return merged, nil
}

// describeLocation renders a location ID for an error message.
func describeLocation(locationID string) string {
	if locationID == "" {
		return "(account-wide)"
	}
	return locationID
}

// mergeResource folds one read into the aggregate, adding the location
// rather than duplicating the resource.
func mergeResource(merged map[resourceKey]*aggregate, res *provider.Resource, locationID string) {
	key := resourceKey{resourceType: res.Type, providerID: res.ProviderID}

	entry, ok := merged[key]
	if !ok {
		entry = &aggregate{resource: res, locations: map[string]bool{}}
		merged[key] = entry
	}
	if locationID != "" {
		entry.locations[locationID] = true
	}
}

// assignNames turns provider resources into named config resources.
//
// Names come from the platform's display name, slugified. Two taxes both
// called "Sales Tax" would collide, so duplicates get a numeric suffix,
// assigned in provider-ID order to keep output stable across runs.
func assignNames(merged map[resourceKey]*aggregate) []*FetchedResource {
	keys := make([]resourceKey, 0, len(merged))
	for key := range merged {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].resourceType != keys[j].resourceType {
			return keys[i].resourceType < keys[j].resourceType
		}
		return keys[i].providerID < keys[j].providerID
	})

	// Names only need to be unique within a resource type, since the
	// config key is "type.name".
	taken := map[string]map[string]bool{}
	resources := make([]*FetchedResource, 0, len(keys))

	for _, key := range keys {
		entry := merged[key]
		res := entry.resource

		if taken[key.resourceType] == nil {
			taken[key.resourceType] = map[string]bool{}
		}
		name := uniqueName(slugify(res.Name), taken[key.resourceType])
		taken[key.resourceType][name] = true

		locationIDs := make([]string, 0, len(entry.locations))
		for id := range entry.locations {
			locationIDs = append(locationIDs, id)
		}
		sort.Strings(locationIDs)

		resources = append(resources, &FetchedResource{
			Type:        res.Type,
			Name:        name,
			DisplayName: res.Name,
			ProviderID:  res.ProviderID,
			Version:     res.Version,
			Properties:  res.Properties,
			LocationIDs: locationIDs,
		})
	}

	sort.Slice(resources, func(i, j int) bool {
		if resources[i].Type != resources[j].Type {
			return resources[i].Type < resources[j].Type
		}
		return resources[i].Name < resources[j].Name
	})

	return resources
}

// slugify converts a display name into a config identifier:
// "GA State Sales Tax" becomes "ga_state_sales_tax".
func slugify(name string) string {
	var b strings.Builder
	lastUnderscore := false

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		default:
			// Collapse any run of punctuation or spaces into one "_".
			if !lastUnderscore && b.Len() > 0 {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return "unnamed"
	}
	return slug
}

// uniqueName appends a numeric suffix until the name is free.
func uniqueName(base string, taken map[string]bool) string {
	if !taken[base] {
		return base
	}
	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s_%d", base, i)
		if !taken[candidate] {
			return candidate
		}
	}
}

// resolveAllRefs rewrites every provider.Ref into "ref(type.name)".
//
// This runs after naming because a reference can only be written once
// the target resource has a config name. Any reference Mise cannot
// resolve — an object outside the fetched set, or one deleted between
// reads — keeps its raw provider ID and produces a warning rather than
// failing the whole fetch.
func resolveAllRefs(resources []*FetchedResource) []string {
	index := make(map[resourceKey]string, len(resources))
	for _, res := range resources {
		index[resourceKey{resourceType: res.Type, providerID: res.ProviderID}] = res.FullName()
	}

	var warnings []string
	for _, res := range resources {
		resolved, unresolved := resolveRefs(res.Properties, index)
		res.Properties, _ = resolved.(map[string]interface{})

		for _, ref := range unresolved {
			warnings = append(warnings, fmt.Sprintf(
				"%s references %s %s, which is not in the fetched configuration — left as a raw ID",
				res.FullName(), ref.ResourceType, ref.ProviderID))
		}
	}

	sort.Strings(warnings)
	return warnings
}

// resolveRefs walks a property value, replacing refs. It returns the
// rewritten value and any references that could not be resolved.
func resolveRefs(value interface{}, index map[resourceKey]string) (interface{}, []provider.Ref) {
	switch typed := value.(type) {
	case provider.Ref:
		if fullName, ok := index[resourceKey{resourceType: typed.ResourceType, providerID: typed.ProviderID}]; ok {
			return "ref(" + fullName + ")", nil
		}
		return typed.ProviderID, []provider.Ref{typed}

	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		var unresolved []provider.Ref
		for key, val := range typed {
			resolved, missing := resolveRefs(val, index)
			out[key] = resolved
			unresolved = append(unresolved, missing...)
		}
		return out, unresolved

	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		var unresolved []provider.Ref
		for _, val := range typed {
			resolved, missing := resolveRefs(val, index)
			out = append(out, resolved)
			unresolved = append(unresolved, missing...)
		}
		return out, unresolved

	default:
		return value, nil
	}
}
