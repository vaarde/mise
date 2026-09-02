package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
)

// schemaProvider is a stub adapter that declares property schemas.
type schemaProvider struct {
	stubProvider
}

func (s *schemaProvider) ResourceTypes() []string {
	return []string{"fake_tax", "fake_item"}
}

func (s *schemaProvider) ResourceSchema(resourceType string) (provider.ResourceSchema, bool) {
	switch resourceType {
	case "fake_tax":
		return provider.ResourceSchema{
			Required: []string{"name"},
			Properties: map[string]provider.PropertySchema{
				"name":       {Description: "display name"},
				"percentage": {Description: "rate"},
				"enabled":    {Description: "whether it applies"},
			},
		}, true
	case "fake_item":
		return provider.ResourceSchema{
			Required: []string{"name"},
			Properties: map[string]provider.PropertySchema{
				"name": {Description: "item name"},
				"variations": {
					Description: "sizes",
					Elem: &provider.ResourceSchema{
						Required: []string{"name"},
						Properties: map[string]provider.PropertySchema{
							"name":        {Description: "variation name"},
							"price_money": {Description: "price"},
						},
					},
				},
			},
		}, true
	}
	return provider.ResourceSchema{}, false
}

func TestValidateCatchesAMisspelledProperty(t *testing.T) {
	// This is the silent one. The adapter copies only the names it
	// recognizes into the API request, so "percentag" is dropped on the
	// way out: the plan shows the change, the apply reports success, the
	// POS gets a tax with no rate, and the next plan proposes the same
	// change again forever.
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{{
		Type: "fake_tax", Name: "sales_tax", SourceFile: "taxes.yaml",
		Properties: map[string]interface{}{"name": "Sales Tax", "percentag": "8.5"},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property "percentag"`)
	assert.Contains(t, err.Error(), `did you mean "percentage"?`)
	assert.Contains(t, err.Error(), "taxes.yaml", "the error names the file to open")
}

func TestValidateListsAcceptedPropertiesForAnInventedName(t *testing.T) {
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{{
		Type: "fake_tax", Name: "sales_tax",
		Properties: map[string]interface{}{"name": "T", "colour": "red"},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property "colour"`)
	assert.Contains(t, err.Error(), "accepted properties are enabled, name, percentage")
	assert.NotContains(t, err.Error(), "did you mean",
		"a name nothing resembles should not get a misleading guess")
}

func TestValidateCatchesAMissingRequiredProperty(t *testing.T) {
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{{
		Type: "fake_tax", Name: "sales_tax",
		Properties: map[string]interface{}{"percentage": "8.5"},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `missing required property "name"`)
}

func TestValidateChecksNestedProperties(t *testing.T) {
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{{
		Type: "fake_item", Name: "tea",
		Properties: map[string]interface{}{
			"name": "Sweet Tea",
			"variations": []interface{}{
				map[string]interface{}{"name": "Regular", "price_monie": 450},
			},
		},
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown property "variations[0].price_monie"`,
		"the path points at the entry, not just the property name")
	assert.Contains(t, err.Error(), `did you mean "price_money"?`)
}

func TestValidateRejectsAnUnsupportedResourceType(t *testing.T) {
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{{
		Type: "fake_pizza", Name: "margherita",
	}})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `does not support resource type "fake_pizza"`)
	assert.Contains(t, err.Error(), "fake_item, fake_tax")
}

func TestValidateReportsEveryProblemAtOnce(t *testing.T) {
	// Fixing config one error per run is a miserable way to work.
	err := ValidateDeclared(&schemaProvider{}, []config.ResourceDef{
		{Type: "fake_tax", Name: "a", Properties: map[string]interface{}{"name": "A", "percentag": "1"}},
		{Type: "fake_tax", Name: "b", Properties: map[string]interface{}{"name": "B", "enable": true}},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "percentag")
	assert.Contains(t, err.Error(), "enable")
}

func TestValidateAcceptsGoodConfiguration(t *testing.T) {
	assert.NoError(t, ValidateDeclared(&schemaProvider{}, []config.ResourceDef{
		{Type: "fake_tax", Name: "sales_tax", Properties: map[string]interface{}{
			"name": "Sales Tax", "percentage": "8.5", "enabled": true,
		}},
		{Type: "fake_item", Name: "tea", Properties: map[string]interface{}{
			"name": "Sweet Tea",
			"variations": []interface{}{
				map[string]interface{}{"name": "Regular", "price_money": 450},
			},
		}},
	}))
}

func TestValidateSkipsAdaptersWithoutSchemas(t *testing.T) {
	// An adapter that declares no schemas behaves exactly as every
	// adapter did before schemas existed.
	p := &stubProvider{types: []string{"fake_tax"}}

	assert.NoError(t, ValidateDeclared(p, []config.ResourceDef{{
		Type: "fake_tax", Name: "sales_tax",
		Properties: map[string]interface{}{"anything": "at all"},
	}}))
}
