package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// LocationsProperty is the pseudo-property used to report a change in
// which locations a resource applies at. It is not a real POS field, but
// widening a tax from three locations to forty is exactly the kind of
// change an operator must see before approving it.
const LocationsProperty = "locations"

// PlanOptions narrows what a plan covers.
type PlanOptions struct {
	// TargetResource limits the plan to one resource, given as "type.name"
	// or just "name" when that is unambiguous.
	TargetResource string

	// TargetLocation limits the plan to one location, by ID or by name.
	TargetLocation string

	// Parallelism caps concurrent per-location reads.
	Parallelism int
}

// DesiredResource is a config file entry with its scope and references
// resolved: locations expanded from groups, and ref(type.name) replaced
// with the provider ID recorded in state.
type DesiredResource struct {
	Type        string
	Name        string
	LocationIDs []string
	Properties  map[string]interface{}

	// PendingRefs are references whose target has no provider ID yet
	// because this same plan will create it.
	PendingRefs []string
}

// FullName returns "type.name".
func (d *DesiredResource) FullName() string { return d.Type + "." + d.Name }

// ComputePlan compares the declared configuration against live POS state
// and reports what would change.
//
// Nothing is written and no POS mutation is made — this is a preview.
func ComputePlan(
	ctx context.Context,
	p provider.Provider,
	root *config.RootConfig,
	declared []config.ResourceDef,
	st *state.State,
	opts PlanOptions,
) (*PlanResult, error) {
	if err := config.ValidateRefTargets(declared); err != nil {
		return nil, err
	}

	// A property the adapter does not recognize is dropped on the way to
	// the API, so it has to be caught here or not at all.
	if err := ValidateDeclared(p, declared); err != nil {
		return nil, err
	}

	locations, err := p.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list locations: %w", err)
	}

	scoped, err := filterLocations(locations, opts.TargetLocation)
	if err != nil {
		return nil, err
	}

	desired, err := buildDesired(root, declared, locations, st)
	if err != nil {
		return nil, err
	}

	desired, err = filterTargets(desired, opts.TargetResource)
	if err != nil {
		return nil, err
	}

	live, err := readLiveFor(ctx, p, desired, scoped, opts.Parallelism)
	if err != nil {
		return nil, err
	}

	// When the operator narrowed the plan to one location, both sides of
	// the comparison must be narrowed with it. Otherwise a resource that
	// also applies elsewhere reads as "gained a location", which is an
	// artifact of the filter rather than a change anyone asked about.
	var scopeFilter []string
	if opts.TargetLocation != "" {
		scopeFilter = locationIDsOf(scoped)
	}

	return comparePlan(desired, live, st, scoped, scopeFilter), nil
}

// locationIDsOf extracts the IDs from a location list.
func locationIDsOf(locations []provider.Location) []string {
	ids := make([]string, 0, len(locations))
	for _, l := range locations {
		ids = append(ids, l.ID)
	}
	sort.Strings(ids)
	return ids
}

// restrictTo intersects a location list with the plan's scope. A nil
// scope means "no filter", so the list passes through untouched.
func restrictTo(locationIDs, scope []string) []string {
	if scope == nil {
		return locationIDs
	}

	allowed := make(map[string]bool, len(scope))
	for _, id := range scope {
		allowed[id] = true
	}

	out := make([]string, 0, len(locationIDs))
	for _, id := range locationIDs {
		if allowed[id] {
			out = append(out, id)
		}
	}
	return out
}

// PlanResult is a plan plus the context needed to display and apply it.
type PlanResult struct {
	Plan
	Locations []provider.Location `json:"locations"`
}

// buildDesired resolves groups and references for every declared resource.
func buildDesired(
	root *config.RootConfig,
	declared []config.ResourceDef,
	locations []provider.Location,
	st *state.State,
) ([]*DesiredResource, error) {
	lookup := stateLookup(st)

	desired := make([]*DesiredResource, 0, len(declared))
	for _, def := range declared {
		locationIDs, err := config.ResolveLocations(def.Locations, root.LocationGroups, locations)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", def.FullName(), err)
		}

		resolved, pending := config.ResolveRefs(def.Properties, lookup)
		properties, _ := resolved.(map[string]interface{})
		if properties == nil {
			properties = map[string]interface{}{}
		}

		sort.Strings(pending)
		desired = append(desired, &DesiredResource{
			Type:        def.Type,
			Name:        def.Name,
			LocationIDs: locationIDs,
			Properties:  properties,
			PendingRefs: pending,
		})
	}

	sort.Slice(desired, func(i, j int) bool {
		if desired[i].Type != desired[j].Type {
			return desired[i].Type < desired[j].Type
		}
		return desired[i].Name < desired[j].Name
	})

	return desired, nil
}

// stateLookup resolves a config name to its provider ID via state.
func stateLookup(st *state.State) config.RefLookup {
	return func(fullName string) (string, bool) {
		if st == nil {
			return "", false
		}
		entry, ok := st.Resources[fullName]
		if !ok || entry.ProviderID == "" {
			return "", false
		}
		return entry.ProviderID, true
	}
}

// readLiveFor reads the live state of every type the plan touches.
func readLiveFor(
	ctx context.Context,
	p provider.Provider,
	desired []*DesiredResource,
	locations []provider.Location,
	parallelism int,
) (map[resourceKey]*aggregate, error) {
	seen := map[string]bool{}
	types := make([]string, 0, 4)
	for _, d := range desired {
		if !seen[d.Type] {
			seen[d.Type] = true
			types = append(types, d.Type)
		}
	}
	if len(types) == 0 {
		return map[resourceKey]*aggregate{}, nil
	}
	sort.Strings(types)

	return readAllLocations(ctx, p, types, locations, parallelism)
}

// comparePlan diffs desired against live and assembles the plan.
func comparePlan(
	desired []*DesiredResource,
	live map[resourceKey]*aggregate,
	st *state.State,
	scoped []provider.Location,
	scopeFilter []string,
) *PlanResult {
	// Keep empty collections non-nil so the machine-readable plan contract is
	// stable: a no-op plan must serialize as `"changes": []`, never null. The
	// Python/AgentCore boundary deliberately validates this strict shape.
	result := &PlanResult{
		Plan: Plan{Changes: make([]ResourceChange, 0)},
		Locations: scoped,
	}

	for _, d := range desired {
		change := planFor(d, live, st, scopeFilter)
		if change.Action == ActionNoop {
			continue
		}

		result.Changes = append(result.Changes, change)
		switch change.Action {
		case ActionCreate:
			result.Summary.ToCreate++
		case ActionUpdate:
			result.Summary.ToUpdate++
		case ActionDelete:
			result.Summary.ToDelete++
		}
	}

	return result
}

// planFor decides what must happen to one declared resource.
func planFor(
	d *DesiredResource,
	live map[resourceKey]*aggregate,
	st *state.State,
	scopeFilter []string,
) ResourceChange {
	// A resource tracked in state must be matched by provider ID, never by
	// display name. This avoids accidentally adopting a manually-created POS
	// object with the same name after the managed object was deleted.
	var providerID string
	if st != nil {
		if entry, ok := st.Resources[d.FullName()]; ok {
			providerID = entry.ProviderID
		}
	}

	var current *aggregate
	if providerID != "" {
		for _, candidate := range live {
			if candidate.ProviderID == providerID {
				current = candidate
				break
			}
		}
	} else {
		for key, candidate := range live {
			if key.ResourceType == d.Type && key.ResourceName == d.Name {
				current = candidate
				break
			}
		}
	}

	if current == nil {
		return ResourceChange{
			Action:       ActionCreate,
			ResourceType: d.Type,
			ResourceName: d.Name,
			LocationIDs:  restrictTo(d.LocationIDs, scopeFilter),
			Diffs:        createDiffs(d.Properties),
			Desired:      d.Properties,
		}
	}

	desiredLocations := restrictTo(d.LocationIDs, scopeFilter)
	currentLocations := restrictTo(current.LocationIDs, scopeFilter)
	diffs := propertyDiffs(d.Properties, current.Properties)
	if !sameStrings(desiredLocations, currentLocations) {
		diffs = append(diffs, PropertyDiff{
			Path:     LocationsProperty,
			OldValue: currentLocations,
			NewValue: desiredLocations,
		})
	}

	if len(diffs) == 0 {
		return ResourceChange{
			Action:       ActionNoop,
			ResourceType: d.Type,
			ResourceName: d.Name,
			ProviderID:   current.ProviderID,
			LocationIDs:  desiredLocations,
			Desired:      d.Properties,
		}
	}

	return ResourceChange{
		Action:       ActionUpdate,
		ResourceType: d.Type,
		ResourceName: d.Name,
		ProviderID:   current.ProviderID,
		LocationIDs:  desiredLocations,
		Diffs:        diffs,
		Desired:      d.Properties,
	}
}

func createDiffs(properties map[string]interface{}) []PropertyDiff {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	diffs := make([]PropertyDiff, 0, len(keys))
	for _, key := range keys {
		diffs = append(diffs, PropertyDiff{Path: key, NewValue: properties[key]})
	}
	return diffs
}

func propertyDiffs(desired, current map[string]interface{}) []PropertyDiff {
	keys := make([]string, 0, len(desired))
	for key := range desired {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	diffs := make([]PropertyDiff, 0)
	for _, key := range keys {
		want := desired[key]
		have, ok := current[key]
		if !ok || !reflect.DeepEqual(normalizeJSONNumber(want), normalizeJSONNumber(have)) {
			diffs = append(diffs, PropertyDiff{Path: key, OldValue: have, NewValue: want})
		}
	}
	return diffs
}

func normalizeJSONNumber(value interface{}) interface{} {
	if number, ok := value.(json.Number); ok {
		return number.String()
	}
	return value
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	left := append([]string(nil), a...)
	right := append([]string(nil), b...)
	sort.Strings(left)
	sort.Strings(right)
	return strings.Join(left, "\x00") == strings.Join(right, "\x00")
}
