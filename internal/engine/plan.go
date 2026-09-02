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
	result := &PlanResult{Locations: scoped}

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
	desiredLocations := restrictTo(d.LocationIDs, scopeFilter)

	change := ResourceChange{
		ResourceType: d.Type,
		ResourceName: d.Name,
		LocationIDs:  desiredLocations,
		Desired:      d.Properties,
	}

	// A resource that does not apply anywhere in the plan's scope is
	// simply out of scope, not a change.
	if scopeFilter != nil && len(desiredLocations) == 0 {
		change.Action = ActionNoop
		return change
	}

	providerID, known := stateLookup(st)(d.FullName())
	if !known {
		// Nothing in state means Mise has never applied this resource, so
		// it is a create. This matches Terraform: state is the record of
		// what Mise manages, and an unmanaged resource gets created.
		change.Action = ActionCreate
		change.Diffs = createDiffs(d.Properties)
		return change
	}

	entry, ok := live[resourceKey{resourceType: d.Type, providerID: providerID}]
	if !ok {
		// State knows an ID but the POS no longer has it — someone deleted
		// it outside Mise. Recreate it.
		change.Action = ActionCreate
		change.ProviderID = ""
		change.Diffs = createDiffs(d.Properties)
		return change
	}

	change.ProviderID = providerID

	liveProperties := normalizeLiveProperties(entry.resource.Properties)
	diffs := diffProperties(liveProperties, d.Properties)

	liveLocations := restrictTo(sortedKeysOf(entry.locations), scopeFilter)
	if locationDiff, changed := diffLocations(liveLocations, desiredLocations); changed {
		diffs = append(diffs, locationDiff)
	}

	if len(diffs) == 0 {
		change.Action = ActionNoop
		return change
	}

	change.Action = ActionUpdate
	change.Diffs = diffs
	return change
}

// createDiffs renders every property of a new resource as an addition.
func createDiffs(properties map[string]interface{}) []PropertyDiff {
	diffs := make([]PropertyDiff, 0, len(properties))
	for _, key := range sortedPropertyKeys(properties) {
		diffs = append(diffs, PropertyDiff{Path: key, NewValue: properties[key]})
	}
	return diffs
}

// diffProperties compares live against desired, per top-level property.
//
// Properties present on the POS but absent from the config are ignored:
// Mise manages what the config declares and leaves the rest alone, which
// is the same safety-first stance that keeps it from deleting resources.
func diffProperties(live, desired map[string]interface{}) []PropertyDiff {
	var diffs []PropertyDiff

	for _, key := range sortedPropertyKeys(desired) {
		want := canonical(desired[key])
		got, present := live[key]

		if !present {
			diffs = append(diffs, PropertyDiff{Path: key, NewValue: desired[key]})
			continue
		}

		if !reflect.DeepEqual(canonical(got), want) {
			diffs = append(diffs, PropertyDiff{Path: key, OldValue: got, NewValue: desired[key]})
		}
	}

	return diffs
}

// diffLocations reports a change in the set of locations a resource
// applies at.
func diffLocations(live, desired []string) (PropertyDiff, bool) {
	if reflect.DeepEqual(live, desired) {
		return PropertyDiff{}, false
	}
	return PropertyDiff{Path: LocationsProperty, OldValue: live, NewValue: desired}, true
}

// normalizeLiveProperties converts provider.Ref values into the provider
// IDs that the desired side has already been resolved to, so the two are
// comparable.
func normalizeLiveProperties(properties map[string]interface{}) map[string]interface{} {
	normalized, _ := normalizeRefs(properties).(map[string]interface{})
	if normalized == nil {
		return map[string]interface{}{}
	}
	return normalized
}

func normalizeRefs(value interface{}) interface{} {
	switch typed := value.(type) {
	case provider.Ref:
		return typed.ProviderID
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[key] = normalizeRefs(val)
		}
		return out
	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		for _, val := range typed {
			out = append(out, normalizeRefs(val))
		}
		return out
	default:
		return value
	}
}

// canonical puts a value into a shape that compares reliably.
//
// The two sides arrive from different decoders: config values come from
// YAML (an int), live values from JSON (an int64 or float64). Round-tripping
// both through JSON makes 450, int64(450) and 450.0 the same value, so a
// price that has not changed does not read as a change.
func canonical(value interface{}) interface{} {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}

	var out interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

// filterLocations narrows to a single location by ID or name.
func filterLocations(locations []provider.Location, target string) ([]provider.Location, error) {
	if target == "" {
		return locations, nil
	}

	for _, l := range locations {
		if l.ID == target || strings.EqualFold(l.Name, target) {
			return []provider.Location{l}, nil
		}
	}

	names := make([]string, 0, len(locations))
	for _, l := range locations {
		names = append(names, fmt.Sprintf("%s (%s)", l.Name, l.ID))
	}
	return nil, fmt.Errorf("unknown location %q — known locations: %s", target, strings.Join(names, ", "))
}

// filterTargets narrows to a single resource by "type.name" or "name".
func filterTargets(desired []*DesiredResource, target string) ([]*DesiredResource, error) {
	if target == "" {
		return desired, nil
	}

	var matched []*DesiredResource
	for _, d := range desired {
		if d.FullName() == target || d.Name == target {
			matched = append(matched, d)
		}
	}

	if len(matched) == 0 {
		return nil, fmt.Errorf("no declared resource matches target %q", target)
	}
	if len(matched) > 1 {
		names := make([]string, 0, len(matched))
		for _, d := range matched {
			names = append(names, d.FullName())
		}
		return nil, fmt.Errorf("target %q is ambiguous — matches %s", target, strings.Join(names, ", "))
	}

	return matched, nil
}

// sortedPropertyKeys returns a property map's keys in sorted order.
func sortedPropertyKeys(properties map[string]interface{}) []string {
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedKeysOf returns a set's members in sorted order.
func sortedKeysOf(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for key := range set {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
