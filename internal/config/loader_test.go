package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeFile creates a file inside dir, making parent directories.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestLoadResourcesRejectsDuplicateDeclarations(t *testing.T) {
	// Everything downstream keys a resource by "type.name": state, the
	// dependency graph, the plan. A second declaration of the same name
	// used to overwrite the first, so which of two conflicting tax rates
	// got applied depended on the order the files were walked in.
	dir := t.TempDir()
	writeFile(t, dir, "taxes.yaml", `
resources:
  - type: square_catalog_tax
    name: sales_tax
    properties:
      name: Sales Tax
      percentage: "8.5"
`)
	writeFile(t, dir, filepath.Join("georgia", "taxes.yaml"), `
resources:
  - type: square_catalog_tax
    name: sales_tax
    properties:
      name: Sales Tax
      percentage: "4.0"
`)

	_, err := LoadResources(dir, "mise.yaml")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "square_catalog_tax.sales_tax")
	assert.Contains(t, err.Error(), "taxes.yaml", "the error names the files to open")
	assert.Contains(t, err.Error(), "declared more than once")
}

func TestLoadResourcesAllowsTheSameNameAcrossTypes(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "config.yaml", `
resources:
  - type: square_catalog_tax
    name: beverages
    properties: {name: Beverage Tax}
  - type: square_catalog_category
    name: beverages
    properties: {name: Beverages}
`)

	resources, err := LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	assert.Len(t, resources, 2, "a name only has to be unique within its type")
}

func TestLoadResourcesRejectsUnknownFields(t *testing.T) {
	// "location" instead of "locations" used to parse cleanly and scope
	// the resource to everywhere, with nothing said about it.
	dir := t.TempDir()
	writeFile(t, dir, "taxes.yaml", `
resources:
  - type: square_catalog_tax
    name: sales_tax
    location: "${group.georgia}"
    properties:
      percentage: "8.5"
`)

	_, err := LoadResources(dir, "mise.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "location")
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadResourcesTagsTheSourceFile(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "taxes.yaml", `
resources:
  - type: square_catalog_tax
    name: sales_tax
    properties: {percentage: "8.5"}
`)

	resources, err := LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	require.Len(t, resources, 1)
	assert.Equal(t, path, resources[0].SourceFile)
}

func TestLoadResourcesSkipsGeneratedLocationsFile(t *testing.T) {
	// locations.yaml is reference data fetch writes, not a resource
	// file. It has its own top-level shape and must not be read as one.
	dir := t.TempDir()
	writeFile(t, dir, LocationsFileName, `
locations:
  - id: LOC_ATL
    name: Atlanta
`)
	writeFile(t, dir, "taxes.yaml", `
resources:
  - type: square_catalog_tax
    name: sales_tax
    properties: {percentage: "8.5"}
`)

	resources, err := LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	assert.Len(t, resources, 1)
}

func TestLoadResourcesAcceptsAnEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "empty.yaml", "")

	resources, err := LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	assert.Empty(t, resources)
}

func TestLoadRootRejectsUnknownFields(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "mise.yaml", `
version: "1"
provider:
  platform: square
  enviroment: sandbox
`)

	_, err := LoadRoot(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "enviroment")
}

func TestDigestIgnoresFileAndOrder(t *testing.T) {
	a := []ResourceDef{
		{Type: "t", Name: "b", SourceFile: "one.yaml", Properties: map[string]interface{}{"x": 1}},
		{Type: "t", Name: "a", SourceFile: "two.yaml", Properties: map[string]interface{}{"y": 2}},
	}
	b := []ResourceDef{
		{Type: "t", Name: "a", SourceFile: "elsewhere.yaml", Properties: map[string]interface{}{"y": 2}},
		{Type: "t", Name: "b", SourceFile: "other.yaml", Properties: map[string]interface{}{"x": 1}},
	}

	digestA, err := Digest(a)
	require.NoError(t, err)
	digestB, err := Digest(b)
	require.NoError(t, err)

	assert.Equal(t, digestA, digestB,
		"moving a resource between files is not a change to the configuration")
}

func TestDigestChangesWithAValue(t *testing.T) {
	before, err := Digest([]ResourceDef{
		{Type: "t", Name: "a", Properties: map[string]interface{}{"percentage": "8.5"}},
	})
	require.NoError(t, err)

	after, err := Digest([]ResourceDef{
		{Type: "t", Name: "a", Properties: map[string]interface{}{"percentage": "9.0"}},
	})
	require.NoError(t, err)

	assert.NotEqual(t, before, after)
}

func TestDigestChangesWithScope(t *testing.T) {
	before, err := Digest([]ResourceDef{{Type: "t", Name: "a", Locations: "${group.georgia}"}})
	require.NoError(t, err)

	after, err := Digest([]ResourceDef{{Type: "t", Name: "a", Locations: "${group.all}"}})
	require.NoError(t, err)

	assert.NotEqual(t, before, after, "widening the scope is a change worth re-reviewing")
}
