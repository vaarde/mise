package engine

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// DriftReason says why a resource is reported.
type DriftReason string

const (
	// DriftChanged means the live values no longer match what Mise
	// last recorded.
	DriftChanged DriftReason = "changed"

	// DriftDeleted means the resource is gone from the POS entirely.
	DriftDeleted DriftReason = "deleted"
)

// DriftOptions narrows a drift check.
type DriftOptions struct {
	// TargetLocation limits the check to one location, by ID or name.
	TargetLocation string

	// ResourceType limits the check to one resource type.
	ResourceType string

	// Parallelism caps concurrent per-location reads.
	Parallelism int
}

// ResourceDrift is one resource that no longer matches its last-known
// state.
type ResourceDrift struct {
	FullName     string         `json:"full_name"`
	ResourceType string         `json:"resource_type"`
	ResourceName string         `json:"resource_name"`
	ProviderID   string         `json:"provider_id"`
	LocationIDs  []string       `json:"location_ids"`
	Reason       DriftReason    `json:"reason"`
	Diffs        []PropertyDiff `json:"diffs,omitempty"`
}

// DriftResult is the outcome of a drift check.
type DriftResult struct {
	Locations []provider.Location `json:"-"`
	Drifted   []ResourceDrift     `json:"drifted"`

	// Checked is how many tracked resources were compared, so a clean
	// report can say what it actually looked at.
	Checked int `json:"checked"`

	// LastFetch and LastApply say when state was last known good.
	LastFetch string `json:"last_fetch,omitempty"`
	LastApply string `json:"last_apply,omitempty"`
}

// HasDrift reports whether anything changed outside Mise.
func (r *DriftResult) HasDrift() bool { return len(r.Drifted) > 0 }

// AffectedLocations returns every location touched by drift, sorted.
func (r *DriftResult) AffectedLocations() []string {
	seen := map[string]bool{}
	for _, drift := range r.Drifted {
		for _, id := range drift.LocationIDs {
			seen[id] = true
		}
	}

	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Drift compares the last-known state against the live POS and reports
// what changed outside Mise.
//
// This is the audit command: plan asks "what would I change?", drift
// asks "what did someone else change?". It reads only — nothing is
// written, including state, because a drift report is evidence of a
// discrepancy, not permission to accept it.
func Drift(
	ctx context.Context,
	p provider.Provider,
	st *state.State,
	opts DriftOptions,
) (*DriftResult, error) {
	if st == nil || len(st.Resources) == 0 {
		return nil, fmt.Errorf("no state to compare against — run 'mise fetch' to record your current configuration first")
	}

	locations, err := p.ListLocations(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot list locations: %w", err)
	}

	scoped, err := filterLocations(locations, opts.TargetLocation)
	if err != nil {
		return nil, err
	}

	tracked, err := trackedResources(st, opts.ResourceType)
	if err != nil {
		return nil, err
	}

	result := &DriftResult{Locations: scoped}
	if st.LastFetch != nil {
		result.LastFetch = st.LastFetch.Format("2006-01-02 15:04:05 MST")
	}
	if st.LastApply != nil {
		result.LastApply = st.LastApply.Format("2006-01-02 15:04:05 MST")
	}

	if len(tracked) == 0 {
		return result, nil
	}

	types := typesOf(tracked)
	live, err := readAllLocations(ctx, p, types, scoped, opts.Parallelism)
	if err != nil {
		return nil, err
	}

	var scopeFilter []string
	if opts.TargetLocation != "" {
		scopeFilter = locationIDsOf(scoped)
	}

	lookup := stateLookup(st)

	for _, entry := range tracked {
		drift, drifted := driftFor(entry, live, lookup, scopeFilter)
		if !drifted {
			result.Checked++
			continue
		}
		result.Checked++
		result.Drifted = append(result.Drifted, drift)
	}

	sort.Slice(result.Drifted, func(i, j int) bool {
		return result.Drifted[i].FullName < result.Drifted[j].FullName
	})

	return result, nil
}

// trackedResources returns the state entries to check, sorted by name.
func trackedResources(st *state.State, resourceType string) ([]*state.ResourceState, error) {
	names := make([]string, 0, len(st.Resources))
	for name := range st.Resources {
		names = append(names, name)
	}
	sort.Strings(names)

	var tracked []*state.ResourceState
	for _, name := range names {
		entry := st.Resources[name]
		if entry == nil || entry.ProviderID == "" {
			continue
		}
		if resourceType != "" && entry.Type != resourceType {
			continue
		}
		tracked = append(tracked, entry)
	}

	if resourceType != "" && len(tracked) == 0 {
		return nil, fmt.Errorf("no tracked resources of type %q — known types: %s",
			resourceType, strings.Join(trackedTypes(st), ", "))
	}

	return tracked, nil
}

// trackedTypes lists the resource types present in state.
func trackedTypes(st *state.State) []string {
	seen := map[string]bool{}
	for _, entry := range st.Resources {
		if entry != nil && entry.Type != "" {
			seen[entry.Type] = true
		}
	}

	types := make([]string, 0, len(seen))
	for t := range seen {
		types = append(types, t)
	}
	sort.Strings(types)
	return types
}

// typesOf returns the distinct resource types to read, sorted.
func typesOf(tracked []*state.ResourceState) []string {
	seen := map[string]bool{}
	types := make([]string, 0, 4)
	for _, entry := range tracked {
		if !seen[entry.Type] {
			seen[entry.Type] = true
			types = append(types, entry.Type)
		}
	}
	sort.Strings(types)
	return types
}

// driftFor compares one tracked resource against the live POS.
func driftFor(
	entry *state.ResourceState,
	live map[resourceKey]*aggregate,
	lookup config.RefLookup,
	scopeFilter []string,
) (ResourceDrift, bool) {
	fullName := state.ResourceKey(entry.Type, entry.Name)

	expectedLocations := restrictTo(entry.Locations, scopeFilter)

	drift := ResourceDrift{
		FullName:     fullName,
		ResourceType: entry.Type,
		ResourceName: entry.Name,
		ProviderID:   entry.ProviderID,
		LocationIDs:  expectedLocations,
	}

	// Resources are matched by provider ID, never by config name: a
	// rename in YAML is not drift, and a name collision must not make
	// two resources look like each other.
	actual, found := live[resourceKey{resourceType: entry.Type, providerID: entry.ProviderID}]
	if !found {
		if scopeFilter != nil && len(expectedLocations) == 0 {
			// The resource does not apply in the scope being checked.
			return drift, false
		}
		drift.Reason = DriftDeleted
		return drift, true
	}

	expected := normalizeReferences(entry.Properties, lookup)
	observed := normalizeReferences(actual.resource.Properties, lookup)

	diffs := driftDiffs(expected, observed)

	actualLocations := restrictTo(sortedKeysOf(actual.locations), scopeFilter)
	if !reflect.DeepEqual(expectedLocations, actualLocations) {
		diffs = append(diffs, PropertyDiff{
			Path:     LocationsProperty,
			OldValue: expectedLocations,
			NewValue: actualLocations,
		})
	}

	if len(diffs) == 0 {
		return drift, false
	}

	drift.Reason = DriftChanged
	drift.Diffs = diffs
	return drift, true
}

// driftDiffs compares recorded properties against live ones.
//
// Only properties Mise recorded are checked. A field the POS added that
// Mise never tracked is not drift — it was never part of the declared
// configuration.
func driftDiffs(expected, observed interface{}) []PropertyDiff {
	expectedMap, _ := expected.(map[string]interface{})
	observedMap, _ := observed.(map[string]interface{})

	var diffs []PropertyDiff
	for _, key := range sortedPropertyKeys(expectedMap) {
		want := canonical(expectedMap[key])
		got, present := observedMap[key]

		if !present {
			diffs = append(diffs, PropertyDiff{Path: key, OldValue: expectedMap[key]})
			continue
		}
		if !reflect.DeepEqual(canonical(got), want) {
			diffs = append(diffs, PropertyDiff{Path: key, OldValue: expectedMap[key], NewValue: got})
		}
	}

	return diffs
}

// normalizeReferences puts references from either side into one shape:
// the raw provider ID.
//
// The two sides disagree on representation. Live reads carry
// provider.Ref values. State carries whichever form last wrote it —
// fetch records "ref(type.name)", while apply records the resolved
// provider ID — so both string forms have to be handled or a resource
// would appear to have drifted purely because of which command ran last.
func normalizeReferences(value interface{}, lookup config.RefLookup) interface{} {
	switch typed := value.(type) {
	case provider.Ref:
		return typed.ProviderID

	case string:
		fullName, isRef := config.ParseRef(typed)
		if !isRef {
			return typed
		}
		if providerID, ok := lookup(fullName); ok {
			return providerID
		}
		// Unresolvable: keep the ref text rather than inventing an ID.
		return typed

	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[key] = normalizeReferences(val, lookup)
		}
		return out

	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, val := range typed {
			out = append(out, normalizeReferences(val, lookup))
		}
		return out

	default:
		return value
	}
}
