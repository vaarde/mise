package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRef(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"ref(square_catalog_tax.ga_state_sales_tax)", "square_catalog_tax.ga_state_sales_tax", true},
		{"  ref(a.b)  ", "a.b", true},
		{"ref(no_dot)", "", false},
		{"ref()", "", false},
		{"square_catalog_tax.ga_state_sales_tax", "", false},
		{"ZMBC7XTQAHBBXE3AOCPZX5LN", "", false},
		{"", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseRef(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestFormatRef(t *testing.T) {
	assert.Equal(t, "ref(square_catalog_tax.ga_tax)", FormatRef("square_catalog_tax.ga_tax"))

	// Round-trips.
	name, ok := ParseRef(FormatRef("a.b"))
	require.True(t, ok)
	assert.Equal(t, "a.b", name)
}

// testLookup resolves a fixed set of names to provider IDs.
func testLookup(known map[string]string) RefLookup {
	return func(fullName string) (string, bool) {
		id, ok := known[fullName]
		return id, ok
	}
}

func TestResolveRefsReplacesWithProviderIDs(t *testing.T) {
	lookup := testLookup(map[string]string{
		"square_catalog_category.beverages": "CAT_BEV",
		"square_catalog_tax.ga_tax":         "TAX_GA",
	})

	properties := map[string]interface{}{
		"name":     "Summer Lemonade",
		"category": "ref(square_catalog_category.beverages)",
		"tax_ids":  []interface{}{"ref(square_catalog_tax.ga_tax)"},
	}

	resolved, unresolved := ResolveRefs(properties, lookup)
	assert.Empty(t, unresolved)

	out := resolved.(map[string]interface{})
	assert.Equal(t, "Summer Lemonade", out["name"], "plain strings are untouched")
	assert.Equal(t, "CAT_BEV", out["category"])
	assert.Equal(t, []interface{}{"TAX_GA"}, out["tax_ids"])
}

func TestResolveRefsReportsUnresolved(t *testing.T) {
	// A reference to a resource this same plan will create has no
	// provider ID yet. That is expected, not an error.
	properties := map[string]interface{}{
		"category": "ref(square_catalog_category.brand_new)",
	}

	resolved, unresolved := ResolveRefs(properties, testLookup(nil))

	assert.Equal(t, []string{"square_catalog_category.brand_new"}, unresolved)
	out := resolved.(map[string]interface{})
	assert.Equal(t, "ref(square_catalog_category.brand_new)", out["category"],
		"an unresolved reference keeps its ref form rather than becoming a bogus ID")
}

func TestResolveRefsWalksNestedStructures(t *testing.T) {
	lookup := testLookup(map[string]string{"a.b": "REAL_ID"})

	value := map[string]interface{}{
		"outer": []interface{}{
			map[string]interface{}{"inner": "ref(a.b)"},
		},
	}

	resolved, unresolved := ResolveRefs(value, lookup)
	assert.Empty(t, unresolved)

	outer := resolved.(map[string]interface{})["outer"].([]interface{})
	assert.Equal(t, "REAL_ID", outer[0].(map[string]interface{})["inner"])
}

func TestResolveRefsLeavesNonStringsAlone(t *testing.T) {
	value := map[string]interface{}{
		"amount":  450,
		"enabled": true,
		"nothing": nil,
	}

	resolved, unresolved := ResolveRefs(value, testLookup(nil))
	assert.Empty(t, unresolved)
	assert.Equal(t, value, resolved)
}

func TestDependencies(t *testing.T) {
	properties := map[string]interface{}{
		"name":     "Summer Lemonade",
		"category": "ref(square_catalog_category.beverages)",
		"tax_ids": []interface{}{
			"ref(square_catalog_tax.ga_tax)",
			"ref(square_catalog_tax.alcohol_tax)",
		},
		"nested": map[string]interface{}{
			"deep": "ref(square_catalog_modifier_list.size)",
		},
	}

	assert.ElementsMatch(t, []string{
		"square_catalog_category.beverages",
		"square_catalog_tax.ga_tax",
		"square_catalog_tax.alcohol_tax",
		"square_catalog_modifier_list.size",
	}, Dependencies(properties))
}

func TestDependenciesDeduplicates(t *testing.T) {
	properties := map[string]interface{}{
		"a": "ref(t.x)",
		"b": "ref(t.x)",
	}
	assert.Equal(t, []string{"t.x"}, Dependencies(properties))
}

func TestDependenciesOfPlainValues(t *testing.T) {
	assert.Empty(t, Dependencies(map[string]interface{}{"name": "Plain", "amount": 450}))
}

func TestValidateRefTargets(t *testing.T) {
	resources := []ResourceDef{
		{
			Type: "square_catalog_category", Name: "beverages",
			Properties: map[string]interface{}{"name": "Beverages"},
		},
		{
			Type: "square_catalog_item", Name: "lemonade",
			Properties: map[string]interface{}{
				"category": "ref(square_catalog_category.beverages)",
			},
		},
	}

	assert.NoError(t, ValidateRefTargets(resources))
}

func TestValidateRefTargetsCatchesTypos(t *testing.T) {
	resources := []ResourceDef{
		{
			Type: "square_catalog_category", Name: "beverages",
			Properties: map[string]interface{}{"name": "Beverages"},
		},
		{
			Type: "square_catalog_item", Name: "lemonade",
			Properties: map[string]interface{}{
				"category": "ref(square_catalog_category.beverage)", // missing "s"
			},
		},
	}

	err := ValidateRefTargets(resources)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "square_catalog_item.lemonade")
	assert.Contains(t, err.Error(), "square_catalog_category.beverage")
	assert.Contains(t, err.Error(), "not declared")
}
