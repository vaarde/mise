package config

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/provider"
)

// groupRefPrefix and groupRefSuffix bracket a location group reference,
// e.g. "${group.georgia}".
const (
	groupRefPrefix = "${group."
	groupRefSuffix = "}"

	// FilterAll is the wildcard filter that matches every location.
	FilterAll = "*"
)

// ParseGroupRef extracts the group name from "${group.georgia}".
func ParseGroupRef(value string) (name string, ok bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, groupRefPrefix) || !strings.HasSuffix(trimmed, groupRefSuffix) {
		return "", false
	}

	name = trimmed[len(groupRefPrefix) : len(trimmed)-len(groupRefSuffix)]
	if name == "" {
		return "", false
	}
	return name, true
}

// ResolveGroup returns the IDs of the locations a group matches.
//
// A filter is either the wildcard "*" or a map of matchers:
//
//	state: "GA"                 one state
//	state: ["CA", "OR", "WA"]   any of several states
//	ids:   ["loc_abc123"]       explicit IDs, regardless of metadata
func ResolveGroup(group LocationGroup, locations []provider.Location) ([]string, error) {
	if group.Filter == nil {
		return nil, fmt.Errorf("group has no filter")
	}

	if literal, ok := group.Filter.(string); ok {
		if strings.TrimSpace(literal) != FilterAll {
			return nil, fmt.Errorf("unknown filter %q — use %q or a map of matchers", literal, FilterAll)
		}
		return locationIDs(locations), nil
	}

	matchers, err := toStringMap(group.Filter)
	if err != nil {
		return nil, err
	}

	var matched []string
	for _, location := range locations {
		ok, err := locationMatches(location, matchers)
		if err != nil {
			return nil, err
		}
		if ok {
			matched = append(matched, location.ID)
		}
	}

	sort.Strings(matched)
	return matched, nil
}

// locationMatches reports whether a location satisfies every matcher.
// Multiple matchers are ANDed; a list of values within one matcher is ORed.
func locationMatches(location provider.Location, matchers map[string]interface{}) (bool, error) {
	for field, want := range matchers {
		values, err := toStringSlice(want)
		if err != nil {
			return false, fmt.Errorf("filter %q: %w", field, err)
		}

		var actual string
		switch field {
		case "ids", "id":
			actual = location.ID
		case "state":
			actual = location.State
		case "name":
			actual = location.Name
		default:
			// Anything else is looked up in provider-specific metadata,
			// so adapters can expose their own filterable fields without
			// the config package knowing about them.
			actual = location.Metadata[field]
		}

		if !containsFold(values, actual) {
			return false, nil
		}
	}
	return true, nil
}

// ResolveLocations turns a resource's `locations:` field into location IDs.
//
// It accepts a group reference ("${group.georgia}"), the wildcard "*", an
// explicit list of location IDs, or nothing at all — which means every
// location, since a resource with no stated scope applies everywhere.
func ResolveLocations(field interface{}, groups map[string]LocationGroup, locations []provider.Location) ([]string, error) {
	if field == nil {
		return locationIDs(locations), nil
	}

	switch typed := field.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" || trimmed == FilterAll {
			return locationIDs(locations), nil
		}

		name, ok := ParseGroupRef(trimmed)
		if !ok {
			return nil, fmt.Errorf("cannot read locations %q — expected ${group.name}, %q, or a list of location IDs",
				typed, FilterAll)
		}

		group, ok := groups[name]
		if !ok {
			return nil, fmt.Errorf("unknown location group %q — defined groups: %v", name, groupNames(groups))
		}

		resolved, err := ResolveGroup(group, locations)
		if err != nil {
			return nil, fmt.Errorf("location group %q: %w", name, err)
		}
		return resolved, nil

	default:
		ids, err := toStringSlice(field)
		if err != nil {
			return nil, fmt.Errorf("cannot read locations: %w", err)
		}

		known := make(map[string]bool, len(locations))
		for _, l := range locations {
			known[l.ID] = true
		}
		for _, id := range ids {
			if !known[id] {
				return nil, fmt.Errorf("unknown location ID %q — run 'mise fetch' if you have added locations", id)
			}
		}

		sorted := append([]string(nil), ids...)
		sort.Strings(sorted)
		return sorted, nil
	}
}

// locationIDs returns every location's ID, sorted.
func locationIDs(locations []provider.Location) []string {
	ids := make([]string, 0, len(locations))
	for _, l := range locations {
		ids = append(ids, l.ID)
	}
	sort.Strings(ids)
	return ids
}

// groupNames lists the defined group names, sorted, for error messages.
func groupNames(groups map[string]LocationGroup) []string {
	names := make([]string, 0, len(groups))
	for name := range groups {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// toStringMap normalizes a YAML mapping into map[string]interface{}.
// yaml.v3 decodes nested maps as map[string]interface{} already, but a
// map[interface{}]interface{} can still arrive from other decoders.
func toStringMap(value interface{}) (map[string]interface{}, error) {
	switch typed := value.(type) {
	case map[string]interface{}:
		return typed, nil
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(typed))
		for key, val := range typed {
			out[fmt.Sprint(key)] = val
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a filter map, got %T", value)
	}
}

// toStringSlice accepts a single scalar or a list and returns strings.
func toStringSlice(value interface{}) ([]string, error) {
	switch typed := value.(type) {
	case string:
		return []string{typed}, nil
	case []string:
		return typed, nil
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			str, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected a string, got %T (%v)", item, item)
			}
			out = append(out, str)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected a string or list of strings, got %T", value)
	}
}

// containsFold reports whether values contains actual, case-insensitively.
// State codes in particular get typed both ways ("GA" and "ga").
func containsFold(values []string, actual string) bool {
	for _, v := range values {
		if strings.EqualFold(v, actual) {
			return true
		}
	}
	return false
}
