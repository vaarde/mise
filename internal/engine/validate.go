package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
)

// ValidateDeclared checks declared configuration against what the
// provider actually supports, before anything is planned.
//
// It catches the class of mistake that is otherwise invisible: a
// misspelled property name. The adapter copies only the names it
// recognizes into an API request, so a typo is dropped on the way out —
// the plan shows the change, the apply reports success, the POS never
// receives it, and the next plan proposes the same change again. A
// misspelled "percentage" on a tax is worse still: the tax goes up with
// no rate at all.
//
// An adapter that declares no schemas is not validated, which is how
// every adapter behaved before schemas existed.
func ValidateDeclared(p provider.Provider, declared []config.ResourceDef) error {
	supported := make(map[string]bool, len(p.ResourceTypes()))
	for _, t := range p.ResourceTypes() {
		supported[t] = true
	}

	schemas, hasSchemas := p.(provider.SchemaProvider)

	var problems []string
	for _, res := range declared {
		if !supported[res.Type] {
			problems = append(problems, fmt.Sprintf("%s: %s does not support resource type %q — supported types are %s",
				describeSource(res), p.Name(), res.Type, strings.Join(sortedStrings(p.ResourceTypes()), ", ")))
			continue
		}

		if !hasSchemas {
			continue
		}
		schema, ok := schemas.ResourceSchema(res.Type)
		if !ok {
			continue
		}

		for _, problem := range validateProperties(res.Properties, schema, "") {
			problems = append(problems, fmt.Sprintf("%s: %s", describeSource(res), problem))
		}
	}

	if len(problems) == 0 {
		return nil
	}

	sort.Strings(problems)
	return fmt.Errorf("the configuration does not match what %s accepts:\n  %s",
		p.Name(), strings.Join(problems, "\n  "))
}

// validateProperties checks one property map against a schema, and
// recurses into lists of objects. path prefixes nested property names so
// an error points at "variations[0].nmae" rather than just "nmae".
func validateProperties(properties map[string]interface{}, schema provider.ResourceSchema, path string) []string {
	var problems []string

	names := make([]string, 0, len(properties))
	for name := range properties {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if !schema.Accepts(name) {
			problems = append(problems, fmt.Sprintf("unknown property %q%s — %s",
				path+name, suggestion(name, schema), acceptedList(schema)))
			continue
		}

		elem := schema.Properties[name].Elem
		if elem == nil {
			continue
		}

		entries, ok := properties[name].([]interface{})
		if !ok {
			problems = append(problems, fmt.Sprintf("%q should be a list", path+name))
			continue
		}

		for i, raw := range entries {
			entry, ok := raw.(map[string]interface{})
			if !ok {
				problems = append(problems, fmt.Sprintf("%s[%d] should be a mapping", path+name, i))
				continue
			}
			problems = append(problems,
				validateProperties(entry, *elem, fmt.Sprintf("%s[%d].", path+name, i))...)
		}
	}

	for _, required := range schema.Required {
		if _, present := properties[required]; !present {
			problems = append(problems, fmt.Sprintf("missing required property %q", path+required))
		}
	}

	return problems
}

// suggestion names the accepted property a typo most likely meant.
func suggestion(name string, schema provider.ResourceSchema) string {
	best, bestDistance := "", 0
	for candidate := range schema.Properties {
		distance := editDistance(name, candidate)

		// Only near-misses are worth suggesting. Anything further away is
		// more likely a property the operator invented than a typo, and a
		// wrong guess is more confusing than none.
		limit := len(candidate) / 3
		if limit < 1 {
			limit = 1
		}
		if distance > limit {
			continue
		}
		if best == "" || distance < bestDistance {
			best, bestDistance = candidate, distance
		}
	}

	if best == "" {
		return ""
	}
	return fmt.Sprintf(" (did you mean %q?)", best)
}

// acceptedList renders a schema's property names for an error message.
func acceptedList(schema provider.ResourceSchema) string {
	names := make([]string, 0, len(schema.Properties))
	for name := range schema.Properties {
		names = append(names, name)
	}
	sort.Strings(names)
	return "accepted properties are " + strings.Join(names, ", ")
}

// editDistance is the Levenshtein distance between two strings, used
// only to suggest a correction for a misspelled property.
func editDistance(a, b string) int {
	previous := make([]int, len(b)+1)
	current := make([]int, len(b)+1)

	for j := range previous {
		previous[j] = j
	}

	for i := 1; i <= len(a); i++ {
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			current[j] = min3(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous, current = current, previous
	}

	return previous[len(b)]
}

func min3(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// describeSource names the resource and, when known, the file it is
// declared in.
func describeSource(res config.ResourceDef) string {
	if res.SourceFile == "" {
		return res.FullName()
	}
	return fmt.Sprintf("%s (%s)", res.FullName(), res.SourceFile)
}

// sortedStrings returns a sorted copy.
func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
