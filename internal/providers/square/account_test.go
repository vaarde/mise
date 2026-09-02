package square

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

func TestAccountIDIsTheMerchantID(t *testing.T) {
	srv := newWriteServer(t, `{"locations":[
      {"id":"LOC_ATL","name":"Atlanta","merchant_id":"MERCHANT_A"},
      {"id":"LOC_SAV","name":"Savannah","merchant_id":"MERCHANT_A"}
    ]}`)
	p := configuredProvider(t, srv.URL)

	id, err := p.AccountID(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "MERCHANT_A", id)
	assert.Equal(t, "/locations", srv.paths[0], "no extra endpoint is needed")
}

func TestAccountIDRefusesATokenSpanningMerchants(t *testing.T) {
	// Mise manages one account per workspace. Guessing which of two
	// merchants a workspace belongs to would defeat the check entirely.
	srv := newWriteServer(t, `{"locations":[
      {"id":"LOC_A","merchant_id":"MERCHANT_A"},
      {"id":"LOC_B","merchant_id":"MERCHANT_B"}
    ]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.AccountID(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MERCHANT_A")
	assert.Contains(t, err.Error(), "MERCHANT_B")
	assert.Contains(t, err.Error(), "one account per workspace")
}

func TestAccountIDReportsAMissingMerchantID(t *testing.T) {
	srv := newWriteServer(t, `{"locations":[{"id":"LOC_A","name":"Atlanta"}]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.AccountID(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "merchant ID")
}

func TestSquareImplementsAccountIdentifier(t *testing.T) {
	// The engine looks this capability up by interface, so losing it is
	// a silent downgrade rather than a compile error.
	var p provider.Provider = &SquareProvider{}
	_, ok := p.(provider.AccountIdentifier)
	assert.True(t, ok, "Square can name its merchant, so it must advertise that")
}

func TestSquareImplementsSchemaProvider(t *testing.T) {
	var p provider.Provider = &SquareProvider{}
	_, ok := p.(provider.SchemaProvider)
	assert.True(t, ok)
}

func TestEverySupportedTypeHasASchema(t *testing.T) {
	// A type without a schema is not validated, so a typo in one of its
	// properties goes back to being silently discarded.
	p := &SquareProvider{}

	for _, resourceType := range p.ResourceTypes() {
		if resourceType == TypeLocation {
			// Locations are read-only reference data; Mise never writes
			// one, so there is no property set to validate.
			continue
		}

		t.Run(resourceType, func(t *testing.T) {
			schema, ok := p.ResourceSchema(resourceType)
			require.True(t, ok, "every writable type needs a schema")
			assert.NotEmpty(t, schema.Properties)
			assert.Contains(t, schema.Required, "name",
				"every Square catalog object is named")
		})
	}
}

func TestSchemaCoversWhatTheWriterSends(t *testing.T) {
	// The schema and the writer's allowlist have to agree. If the writer
	// accepts a property the schema does not, declaring it is an error
	// even though it would have worked.
	cases := []struct {
		resourceType string
		properties   map[string]interface{}
	}{
		{TypeTax, map[string]interface{}{
			"name": "GA Tax", "percentage": "4.5", "enabled": true,
			"calculation_phase": "TAX_SUBTOTAL_PHASE", "inclusion_type": "ADDITIVE",
			"applies_to_custom_amounts": true,
		}},
		{TypeCategory, map[string]interface{}{"name": "Beverages"}},
		{TypeDiscount, map[string]interface{}{
			"name": "Staff", "discount_type": "FIXED_PERCENTAGE", "percentage": "10.0",
			"amount_money": map[string]interface{}{"amount": 100, "currency": "USD"},
			"pin_required": true, "label_color": "9da2a6",
		}},
		{TypeModifierList, map[string]interface{}{
			"name": "Size", "selection_type": "SINGLE",
			"modifiers": []interface{}{map[string]interface{}{"name": "Large"}},
		}},
		{TypeItem, map[string]interface{}{
			"name": "Tea", "description": "Sweet", "abbreviation": "TEA",
			"product_type": "REGULAR", "category": "CAT_1",
			"tax_ids": []interface{}{"TAX_1"}, "modifier_lists": []interface{}{"MOD_1"},
			"variations": []interface{}{map[string]interface{}{
				"name": "Regular", "sku": "T-1", "pricing_type": "FIXED_PRICING",
				"price_money": map[string]interface{}{"amount": 450, "currency": "USD"},
			}},
		}},
	}

	p := &SquareProvider{}
	for _, tc := range cases {
		t.Run(tc.resourceType, func(t *testing.T) {
			schema, ok := p.ResourceSchema(tc.resourceType)
			require.True(t, ok)

			for name := range tc.properties {
				assert.True(t, schema.Accepts(name),
					"the writer sends %q but the schema rejects it", name)
			}
		})
	}
}
