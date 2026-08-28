package config

import (
	"fmt"
	"strings"
)

// Resources reference each other by config name:
//
//	tax_ids:
//	  - ref(square_catalog_tax.ga_state_sales_tax)
//
// This is what keeps config files readable and portable — a name survives
// being applied to a different account, an opaque provider ID does not.
const (
	refPrefix = "ref("
	refSuffix = ")"
)

// ParseRef extracts "type.name" from "ref(type.name)".
func ParseRef(value string) (fullName string, ok bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, refPrefix) || !strings.HasSuffix(trimmed, refSuffix) {
		return "", false
	}

	fullName = strings.TrimSpace(trimmed[len(refPrefix) : len(trimmed)-len(refSuffix)])
	if fullName == "" || !strings.Contains(fullName, ".") {
		return "", false
	}
	return fullName, true
}

// FormatRef renders a reference for writing into a config file.
func FormatRef(fullName string) string {
	return refPrefix + fullName + refSuffix
}

// RefLookup resolves a "type.name" to the provider-assigned ID recorded in
// state. It returns false when the resource has never been applied.
type RefLookup func(fullName string) (providerID string, ok bool)

// ResolveRefs walks a property value and replaces every ref(type.name)
// with the provider ID it points at.
//
// Plan compares declared config against live POS state, and the live side
// only knows provider IDs. Resolving to IDs — rather than comparing the
// ref strings themselves — means renaming a resource in YAML is not
// mistaken for a change to the resource it points at.
//
// References that state cannot resolve are returned in unresolved. They
// are not an error on their own: a brand-new tax that this same plan will
// create has no provider ID yet, and the item referencing it is simply
// planned after it.
func ResolveRefs(value interface{}, lookup RefLookup) (resolved interface{}, unresolved []string) {
	switch typed := value.(type) {
	case string:
		fullName, ok := ParseRef(typed)
		if !ok {
			return typed, nil
		}
		providerID, found := lookup(fullName)
		if !found {
			return typed, []string{fullName}
		}
		return providerID, nil

	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		var missing []string
		for key, val := range typed {
			res, miss := ResolveRefs(val, lookup)
			out[key] = res
			missing = append(missing, miss...)
		}
		return out, missing

	case []interface{}:
		out := make([]interface{}, 0, len(typed))
		var missing []string
		for _, val := range typed {
			res, miss := ResolveRefs(val, lookup)
			out = append(out, res)
			missing = append(missing, miss...)
		}
		return out, missing

	default:
		return value, nil
	}
}

// Dependencies returns the "type.name" of every resource referenced by a
// property tree. Apply uses this to order work: a tax must exist before
// the item that charges it.
func Dependencies(value interface{}) []string {
	seen := map[string]bool{}
	collectDependencies(value, seen)

	deps := make([]string, 0, len(seen))
	for name := range seen {
		deps = append(deps, name)
	}
	return deps
}

func collectDependencies(value interface{}, seen map[string]bool) {
	switch typed := value.(type) {
	case string:
		if fullName, ok := ParseRef(typed); ok {
			seen[fullName] = true
		}
	case map[string]interface{}:
		for _, val := range typed {
			collectDependencies(val, seen)
		}
	case []interface{}:
		for _, val := range typed {
			collectDependencies(val, seen)
		}
	}
}

// ValidateRefTargets checks that every reference points at a resource that
// is actually declared somewhere in the workspace. A typo in a ref would
// otherwise surface much later as a confusing API error during apply.
func ValidateRefTargets(resources []ResourceDef) error {
	declared := make(map[string]bool, len(resources))
	for _, res := range resources {
		declared[res.FullName()] = true
	}

	for _, res := range resources {
		for _, dep := range Dependencies(res.Properties) {
			if !declared[dep] {
				return fmt.Errorf("%s references %s, which is not declared in any config file",
					res.FullName(), FormatRef(dep))
			}
		}
	}
	return nil
}
