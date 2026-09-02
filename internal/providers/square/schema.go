package square

import "github.com/vaarde/mise/internal/provider"

// resourceSchemas declares which properties each Square resource type
// accepts.
//
// These mirror the fields typeDataFor copies into a Square request, and
// the fields catalogObject.properties reads back out — one list, checked
// at plan time instead of silently applied. When a property is added to
// either side, it belongs here too, or declaring it becomes an error.
//
// Square accepts far more fields than these on most objects. The set
// here is what Mise manages: config files describe intent, not a mirror
// of the API.
var resourceSchemas = map[string]provider.ResourceSchema{
	TypeTax: {
		Required: []string{"name"},
		Properties: map[string]provider.PropertySchema{
			"name":                      {Description: "display name shown on receipts"},
			"percentage":                {Description: `tax rate as a string, e.g. "8.5"`},
			"enabled":                   {Description: "whether the tax is applied"},
			"calculation_phase":         {Description: "TAX_SUBTOTAL_PHASE or TAX_TOTAL_PHASE"},
			"inclusion_type":            {Description: "ADDITIVE or INCLUSIVE"},
			"applies_to_custom_amounts": {Description: "whether custom amounts are taxed"},
		},
	},

	TypeCategory: {
		Required: []string{"name"},
		Properties: map[string]provider.PropertySchema{
			"name": {Description: "category name"},
		},
	},

	TypeDiscount: {
		Required: []string{"name"},
		Properties: map[string]provider.PropertySchema{
			"name":          {Description: "display name"},
			"discount_type": {Description: "FIXED_PERCENTAGE, FIXED_AMOUNT, VARIABLE_PERCENTAGE or VARIABLE_AMOUNT"},
			"percentage":    {Description: `percentage off as a string, e.g. "10.0"`},
			"amount_money":  {Description: "fixed amount off, as amount and currency"},
			"pin_required":  {Description: "whether a manager PIN is needed to apply it"},
			"label_color":   {Description: "hex color for the POS button"},
		},
	},

	TypeModifierList: {
		Required: []string{"name"},
		Properties: map[string]provider.PropertySchema{
			"name":           {Description: "modifier list name"},
			"selection_type": {Description: "SINGLE or MULTIPLE"},
			"modifiers": {
				Description: "the modifiers in this list",
				Elem: &provider.ResourceSchema{
					Required: []string{"name"},
					Properties: map[string]provider.PropertySchema{
						"name":        {Description: "modifier name"},
						"price_money": {Description: "surcharge, as amount and currency"},
					},
				},
			},
		},
	},

	TypeItem: {
		Required: []string{"name"},
		Properties: map[string]provider.PropertySchema{
			"name":           {Description: "item name"},
			"description":    {Description: "menu description"},
			"abbreviation":   {Description: "short name for the POS button"},
			"product_type":   {Description: "REGULAR or APPOINTMENTS_SERVICE"},
			"category":       {Description: "ref() to the category this item belongs to"},
			"tax_ids":        {Description: "list of ref() to the taxes charged on this item"},
			"modifier_lists": {Description: "list of ref() to the modifier lists offered"},
			"variations": {
				Description: "the sizes or options this item is sold as",
				Elem: &provider.ResourceSchema{
					Required: []string{"name"},
					Properties: map[string]provider.PropertySchema{
						"name":         {Description: "variation name, e.g. Regular or Large"},
						"sku":          {Description: "stock keeping unit"},
						"pricing_type": {Description: "FIXED_PRICING or VARIABLE_PRICING"},
						"price_money":  {Description: "price, as amount in cents and currency"},
					},
				},
			},
		},
	},
}

// ResourceSchema implements provider.SchemaProvider.
func (p *SquareProvider) ResourceSchema(resourceType string) (provider.ResourceSchema, bool) {
	schema, ok := resourceSchemas[resourceType]
	return schema, ok
}
