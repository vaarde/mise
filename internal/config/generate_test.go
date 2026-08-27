package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/vaarde/mise/internal/provider"
)

func sampleResources() []ResourceDef {
	return []ResourceDef{
		{
			Type:      "square_catalog_tax",
			Name:      "ga_state_sales_tax",
			Locations: GroupAll,
			Properties: map[string]interface{}{
				"name":                      "GA State Sales Tax",
				"percentage":                "4.5",
				"enabled":                   true,
				"calculation_phase":         "TAX_SUBTOTAL_PHASE",
				"inclusion_type":            "ADDITIVE",
				"applies_to_custom_amounts": true,
			},
		},
		{
			Type:      "square_catalog_tax",
			Name:      "ga_alcohol_tax",
			Locations: []string{"LOC_ATL", "LOC_SAV"},
			Properties: map[string]interface{}{
				"name":       "GA Alcohol Tax",
				"percentage": "6.0",
				"enabled":    true,
			},
		},
		{
			Type:      "square_catalog_category",
			Name:      "beverages",
			Locations: GroupAll,
			Properties: map[string]interface{}{
				"name": "Beverages",
			},
		},
		{
			Type:      "square_catalog_item",
			Name:      "summer_lemonade",
			Locations: GroupAll,
			Properties: map[string]interface{}{
				"name":     "Summer Lemonade",
				"category": "ref(square_catalog_category.beverages)",
				"tax_ids":  []interface{}{"ref(square_catalog_tax.ga_state_sales_tax)"},
				"variations": []interface{}{
					map[string]interface{}{
						"name":        "Regular",
						"price_money": map[string]interface{}{"amount": int64(450), "currency": "USD"},
					},
				},
			},
		},
	}
}

func TestFileFor(t *testing.T) {
	assert.Equal(t, "taxes.yaml", FileFor("square_catalog_tax"))
	assert.Equal(t, filepath.Join("menu", "items.yaml"), FileFor("square_catalog_item"))
	assert.Equal(t, "toast_tax_rate.yaml", FileFor("toast_tax_rate"),
		"an unmapped type gets its own file rather than being dropped")
}

func TestGenerateResourceFilesGroupsByType(t *testing.T) {
	dir := t.TempDir()

	written, err := GenerateResourceFiles(dir, sampleResources())
	require.NoError(t, err)

	assert.Equal(t, []string{"menu/categories.yaml", "menu/items.yaml", "taxes.yaml"}, written)
	for _, path := range written {
		assert.FileExists(t, filepath.Join(dir, filepath.FromSlash(path)))
	}
}

func TestGeneratedFilesRoundTripThroughTheLoader(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateResourceFiles(dir, sampleResources())
	require.NoError(t, err)

	// The loader that plan and apply use must be able to read back what
	// fetch writes — otherwise the workspace is broken on arrival.
	loaded, err := LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	require.Len(t, loaded, 4)

	byName := map[string]ResourceDef{}
	for _, res := range loaded {
		byName[res.FullName()] = res
	}

	stateTax := byName["square_catalog_tax.ga_state_sales_tax"]
	assert.Equal(t, GroupAll, stateTax.Locations)
	assert.Equal(t, "4.5", stateTax.Properties["percentage"],
		"a percentage must survive as a string, not become a float")
	assert.Equal(t, true, stateTax.Properties["enabled"])

	alcohol := byName["square_catalog_tax.ga_alcohol_tax"]
	assert.Equal(t, []interface{}{"LOC_ATL", "LOC_SAV"}, alcohol.Locations,
		"an explicit location list must round-trip as a list")

	item := byName["square_catalog_item.summer_lemonade"]
	assert.Equal(t, "ref(square_catalog_category.beverages)", item.Properties["category"])
}

func TestGenerateResourceFilesIsIdempotent(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()

	_, err := GenerateResourceFiles(first, sampleResources())
	require.NoError(t, err)
	_, err = GenerateResourceFiles(second, sampleResources())
	require.NoError(t, err)

	// Re-running fetch when nothing changed must produce a byte-identical
	// file set, or every fetch shows up as a spurious diff in git.
	for _, path := range []string{"taxes.yaml", filepath.Join("menu", "items.yaml")} {
		a, err := os.ReadFile(filepath.Join(first, path))
		require.NoError(t, err)
		b, err := os.ReadFile(filepath.Join(second, path))
		require.NoError(t, err)
		assert.Equal(t, string(a), string(b), "%s should be stable across runs", path)
	}
}

func TestGenerateResourceFilesOverwritesCleanly(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateResourceFiles(dir, sampleResources())
	require.NoError(t, err)
	before, err := os.ReadFile(filepath.Join(dir, "taxes.yaml"))
	require.NoError(t, err)

	// A second write of the same data must not append or duplicate.
	_, err = GenerateResourceFiles(dir, sampleResources())
	require.NoError(t, err)
	after, err := os.ReadFile(filepath.Join(dir, "taxes.yaml"))
	require.NoError(t, err)

	assert.Equal(t, string(before), string(after))
	assert.Equal(t, 1, strings.Count(string(after), "resources:"))
}

func TestGeneratedYAMLIsReadable(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateResourceFiles(dir, sampleResources())
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, "taxes.yaml"))
	require.NoError(t, err)
	content := string(raw)

	assert.Contains(t, content, "# Generated by mise fetch", "the file should say where it came from")

	// Within a resource, identity comes before detail.
	typeIdx := strings.Index(content, "- type: square_catalog_tax")
	nameIdx := strings.Index(content, "name: ga_alcohol_tax")
	propsIdx := strings.Index(content, "properties:")
	require.NotEqual(t, -1, typeIdx)
	require.NotEqual(t, -1, nameIdx)
	assert.Less(t, typeIdx, nameIdx)
	assert.Less(t, nameIdx, propsIdx)

	// Within properties, the human-readable name leads.
	displayNameIdx := strings.Index(content, `name: GA State Sales Tax`)
	phaseIdx := strings.Index(content, "calculation_phase:")
	require.NotEqual(t, -1, displayNameIdx)
	require.NotEqual(t, -1, phaseIdx)
	assert.Less(t, displayNameIdx, phaseIdx, "name should lead a property block")

	// Explicit location lists read better inline.
	assert.Contains(t, content, "locations: [LOC_ATL, LOC_SAV]")
	assert.Contains(t, content, "locations: ${group.all}")
}

func TestGenerateLocationsFile(t *testing.T) {
	dir := t.TempDir()

	locations := []provider.Location{
		{ID: "LOC_NSH", Name: "Nashville", State: "TN", Timezone: "America/Chicago",
			Metadata: map[string]string{"status": "ACTIVE"}},
		{ID: "LOC_ATL", Name: "Atlanta", State: "GA", Timezone: "America/New_York",
			Address: "1100 Peachtree St NE, Atlanta, GA", Metadata: map[string]string{"status": "ACTIVE"}},
	}

	path, err := GenerateLocationsFile(dir, locations)
	require.NoError(t, err)
	assert.Equal(t, LocationsFileName, path)

	raw, err := os.ReadFile(filepath.Join(dir, LocationsFileName))
	require.NoError(t, err)
	content := string(raw)

	assert.Contains(t, content, "LOC_ATL")
	assert.Contains(t, content, "1100 Peachtree St NE")
	assert.Less(t, strings.Index(content, "Atlanta"), strings.Index(content, "Nashville"),
		"locations are sorted by name so the file is stable")

	var parsed struct {
		Locations []struct {
			ID       string `yaml:"id"`
			Name     string `yaml:"name"`
			State    string `yaml:"state"`
			Timezone string `yaml:"timezone"`
		} `yaml:"locations"`
	}
	require.NoError(t, unmarshalYAML(raw, &parsed))
	require.Len(t, parsed.Locations, 2)
	assert.Equal(t, "LOC_ATL", parsed.Locations[0].ID)
	assert.Equal(t, "GA", parsed.Locations[0].State)
}

func TestGenerateLocationsFileOmitsBlankFields(t *testing.T) {
	dir := t.TempDir()

	_, err := GenerateLocationsFile(dir, []provider.Location{{ID: "LOC_X", Name: "Food Truck"}})
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, LocationsFileName))
	require.NoError(t, err)

	assert.NotContains(t, string(raw), "state:")
	assert.NotContains(t, string(raw), "address:")
}

func TestExistingGeneratedFiles(t *testing.T) {
	dir := t.TempDir()
	types := []string{"square_catalog_tax", "square_catalog_item"}

	assert.Empty(t, ExistingGeneratedFiles(dir, types))

	require.NoError(t, os.WriteFile(filepath.Join(dir, "taxes.yaml"), []byte("resources: []\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "menu"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "menu", "items.yaml"), []byte("resources: []\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(dir, LocationsFileName), []byte("locations: []\n"), 0o644))

	assert.Equal(t, []string{"locations.yaml", "menu/items.yaml", "taxes.yaml"},
		ExistingGeneratedFiles(dir, types))
}

func TestGenerateResourceFilesHandlesEmptyInput(t *testing.T) {
	dir := t.TempDir()

	written, err := GenerateResourceFiles(dir, nil)
	require.NoError(t, err)
	assert.Empty(t, written, "nothing fetched means nothing written")
}

func TestOrderedKeys(t *testing.T) {
	props := map[string]interface{}{
		"zebra": 1, "name": 2, "enabled": 3, "alpha": 4, "percentage": 5,
	}

	assert.Equal(t,
		[]string{"name", "percentage", "enabled", "alpha", "zebra"},
		orderedKeys(props),
		"preferred keys lead, the rest are alphabetical for stability")
}

// unmarshalYAML is a small helper so the test does not need its own
// yaml import alias.
func unmarshalYAML(data []byte, target interface{}) error {
	return yaml.Unmarshal(data, target)
}
