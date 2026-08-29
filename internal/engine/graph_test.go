package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// change builds a ResourceChange with the given desired properties.
func change(fullType, name string, properties map[string]interface{}) ResourceChange {
	return ResourceChange{
		Action:       ActionCreate,
		ResourceType: fullType,
		ResourceName: name,
		Desired:      properties,
	}
}

// waveNames renders waves as name lists, for readable assertions.
func waveNames(waves []Wave) [][]string {
	out := make([][]string, 0, len(waves))
	for _, wave := range waves {
		names := make([]string, 0, len(wave))
		for _, c := range wave {
			names = append(names, c.FullName())
		}
		out = append(out, names)
	}
	return out
}

func TestOrderChangesPutsDependenciesFirst(t *testing.T) {
	changes := []ResourceChange{
		change("square_catalog_item", "lemonade", map[string]interface{}{
			"category": "ref(square_catalog_category.beverages)",
			"tax_ids":  []interface{}{"ref(square_catalog_tax.ga_tax)"},
		}),
		change("square_catalog_category", "beverages", map[string]interface{}{"name": "Beverages"}),
		change("square_catalog_tax", "ga_tax", map[string]interface{}{"percentage": "4.5"}),
	}

	waves, err := OrderChanges(changes, nil)
	require.NoError(t, err)

	// The tax and the category depend on nothing, so they go first
	// together; the item that references both comes after.
	require.Len(t, waves, 2)
	assert.Equal(t, [][]string{
		{"square_catalog_category.beverages", "square_catalog_tax.ga_tax"},
		{"square_catalog_item.lemonade"},
	}, waveNames(waves))
}

func TestOrderChangesHandlesAChain(t *testing.T) {
	changes := []ResourceChange{
		change("t", "c", map[string]interface{}{"needs": "ref(t.b)"}),
		change("t", "b", map[string]interface{}{"needs": "ref(t.a)"}),
		change("t", "a", map[string]interface{}{"name": "root"}),
	}

	waves, err := OrderChanges(changes, nil)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"t.a"}, {"t.b"}, {"t.c"}}, waveNames(waves))
}

func TestOrderChangesIgnoresDependenciesOutsideThePlan(t *testing.T) {
	// The tax already exists on the POS and is not part of this plan, so
	// it must not delay the item.
	changes := []ResourceChange{
		change("square_catalog_item", "lemonade", map[string]interface{}{
			"tax_ids": []interface{}{"ref(square_catalog_tax.already_live)"},
		}),
	}

	waves, err := OrderChanges(changes, nil)
	require.NoError(t, err)
	require.Len(t, waves, 1, "an existing dependency imposes no ordering")
}

func TestOrderChangesBatchesIndependentResources(t *testing.T) {
	changes := []ResourceChange{
		change("t", "a", map[string]interface{}{"x": 1}),
		change("t", "b", map[string]interface{}{"x": 2}),
		change("t", "c", map[string]interface{}{"x": 3}),
	}

	waves, err := OrderChanges(changes, nil)
	require.NoError(t, err)

	require.Len(t, waves, 1, "independent resources should apply in one batch")
	assert.Len(t, waves[0], 3)
}

func TestOrderChangesIsDeterministic(t *testing.T) {
	changes := []ResourceChange{
		change("t", "zebra", map[string]interface{}{"x": 1}),
		change("t", "apple", map[string]interface{}{"x": 2}),
		change("t", "mango", map[string]interface{}{"x": 3}),
	}

	first, err := OrderChanges(changes, nil)
	require.NoError(t, err)
	second, err := OrderChanges(changes, nil)
	require.NoError(t, err)

	assert.Equal(t, waveNames(first), waveNames(second))
	assert.Equal(t, []string{"t.apple", "t.mango", "t.zebra"}, waveNames(first)[0])
}

func TestOrderChangesDetectsCircularReferences(t *testing.T) {
	changes := []ResourceChange{
		change("t", "a", map[string]interface{}{"needs": "ref(t.b)"}),
		change("t", "b", map[string]interface{}{"needs": "ref(t.a)"}),
	}

	_, err := OrderChanges(changes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference")
	assert.Contains(t, err.Error(), "t.a needs t.b")
	assert.Contains(t, err.Error(), "t.b needs t.a")
}

func TestOrderChangesToleratesSelfReference(t *testing.T) {
	// A resource referencing itself would deadlock the sort; it is
	// dropped as a constraint rather than reported as a cycle.
	changes := []ResourceChange{
		change("t", "a", map[string]interface{}{"needs": "ref(t.a)"}),
	}

	waves, err := OrderChanges(changes, nil)
	require.NoError(t, err)
	require.Len(t, waves, 1)
}

func TestOrderChangesEmptyPlan(t *testing.T) {
	waves, err := OrderChanges(nil, nil)
	require.NoError(t, err)
	assert.Empty(t, waves)
}

func TestOrderChangesUsesSuppliedProperties(t *testing.T) {
	// Callers can pass declared properties separately from the plan's
	// resolved Desired map.
	changes := []ResourceChange{
		change("t", "a", nil),
		change("t", "b", nil),
	}
	declared := map[string]map[string]interface{}{
		"t.b": {"needs": "ref(t.a)"},
	}

	waves, err := OrderChanges(changes, declared)
	require.NoError(t, err)
	assert.Equal(t, [][]string{{"t.a"}, {"t.b"}}, waveNames(waves))
}
