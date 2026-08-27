package square

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/vaarde/mise/internal/provider"
)

// Mise resource types map one-to-one onto Square catalog object types.
// Square keeps every one of these in a single Catalog API, distinguished
// by the "type" field and a matching "<type>_data" payload.
const (
	TypeItem         = "square_catalog_item"
	TypeCategory     = "square_catalog_category"
	TypeTax          = "square_catalog_tax"
	TypeDiscount     = "square_catalog_discount"
	TypeModifierList = "square_catalog_modifier_list"
	TypeLocation     = "square_location"
)

// catalogTypeFor maps a Mise resource type to its Square catalog type.
var catalogTypeFor = map[string]string{
	TypeItem:         "ITEM",
	TypeCategory:     "CATEGORY",
	TypeTax:          "TAX",
	TypeDiscount:     "DISCOUNT",
	TypeModifierList: "MODIFIER_LIST",
}

// maxCatalogPages backstops cursor paging. Square returns at most a few
// hundred objects per page, so a real catalog will never approach this.
const maxCatalogPages = 10000

// catalogObject is a Square catalog object. Every type shares the
// envelope; the type-specific payload arrives in one of the *Data fields.
type catalogObject struct {
	Type      string `json:"type"`
	ID        string `json:"id"`
	UpdatedAt string `json:"updated_at"`

	// Version is Square's optimistic-concurrency token. It arrives as a
	// number but Mise stores it as a string, since other platforms use
	// opaque string etags.
	Version   int64 `json:"version"`
	IsDeleted bool  `json:"is_deleted"`

	// Square scopes catalog objects to locations in one of two ways:
	// present everywhere minus an exclusion list, or present only at an
	// explicit list.
	PresentAtAllLocations bool     `json:"present_at_all_locations"`
	PresentAtLocationIDs  []string `json:"present_at_location_ids"`
	AbsentAtLocationIDs   []string `json:"absent_at_location_ids"`

	ItemData         *itemData         `json:"item_data,omitempty"`
	CategoryData     *categoryData     `json:"category_data,omitempty"`
	TaxData          *taxData          `json:"tax_data,omitempty"`
	DiscountData     *discountData     `json:"discount_data,omitempty"`
	ModifierListData *modifierListData `json:"modifier_list_data,omitempty"`
	VariationData    *variationData    `json:"item_variation_data,omitempty"`
	ModifierData     *modifierData     `json:"modifier_data,omitempty"`
}

type money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type itemData struct {
	Name             string             `json:"name"`
	Description      string             `json:"description"`
	Abbreviation     string             `json:"abbreviation"`
	CategoryID       string             `json:"category_id"`
	TaxIDs           []string           `json:"tax_ids"`
	ProductType      string             `json:"product_type"`
	Variations       []catalogObject    `json:"variations"`
	ModifierListInfo []modifierListInfo `json:"modifier_list_info"`
}

type modifierListInfo struct {
	ModifierListID string `json:"modifier_list_id"`
	Enabled        bool   `json:"enabled"`
	MinSelected    *int   `json:"min_selected_modifiers"`
	MaxSelected    *int   `json:"max_selected_modifiers"`
}

type variationData struct {
	Name        string `json:"name"`
	SKU         string `json:"sku"`
	PricingType string `json:"pricing_type"`
	PriceMoney  *money `json:"price_money"`
}

type categoryData struct {
	Name string `json:"name"`
}

type taxData struct {
	Name                   string `json:"name"`
	CalculationPhase       string `json:"calculation_phase"`
	InclusionType          string `json:"inclusion_type"`
	Percentage             string `json:"percentage"`
	AppliesToCustomAmounts bool   `json:"applies_to_custom_amounts"`
	Enabled                bool   `json:"enabled"`
}

type discountData struct {
	Name         string `json:"name"`
	DiscountType string `json:"discount_type"`
	Percentage   string `json:"percentage"`
	AmountMoney  *money `json:"amount_money"`
	PinRequired  bool   `json:"pin_required"`
	LabelColor   string `json:"label_color"`
}

type modifierListData struct {
	Name          string          `json:"name"`
	SelectionType string          `json:"selection_type"`
	Modifiers     []catalogObject `json:"modifiers"`
}

type modifierData struct {
	Name       string `json:"name"`
	PriceMoney *money `json:"price_money"`
}

// catalogListResponse is the GET /v2/catalog/list response.
type catalogListResponse struct {
	Objects []catalogObject `json:"objects"`
	Cursor  string          `json:"cursor"`
}

// catalogObjectResponse is the GET /v2/catalog/object/{id} response.
type catalogObjectResponse struct {
	Object catalogObject `json:"object"`
}

// ListCatalogObjects returns every non-deleted catalog object of one
// Square type, following Square's cursor pagination to the end.
func (c *Client) ListCatalogObjects(ctx context.Context, squareType string) ([]catalogObject, error) {
	var all []catalogObject
	cursor := ""

	// A cursor that repeats means the listing is not advancing. Catching
	// that on the second sighting costs one wasted request; discovering
	// it by exhausting a page budget would cost thousands.
	seenCursors := map[string]bool{}

	for page := 0; page < maxCatalogPages; page++ {
		path := "/catalog/list?types=" + url.QueryEscape(squareType)
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}

		var resp catalogListResponse
		if err := c.Get(ctx, path, &resp); err != nil {
			return nil, err
		}

		for _, obj := range resp.Objects {
			// Square tombstones deleted objects rather than removing
			// them; they are not part of the live configuration.
			if !obj.IsDeleted {
				all = append(all, obj)
			}
		}

		if resp.Cursor == "" {
			return all, nil
		}
		if seenCursors[resp.Cursor] {
			return nil, fmt.Errorf(
				"catalog listing for %s did not finish: Square returned a repeating pagination cursor",
				squareType)
		}
		seenCursors[resp.Cursor] = true
		cursor = resp.Cursor
	}

	return nil, fmt.Errorf("catalog listing for %s did not finish within %d pages", squareType, maxCatalogPages)
}

// GetCatalogObject fetches a single catalog object by its Square ID.
func (c *Client) GetCatalogObject(ctx context.Context, id string) (*catalogObject, error) {
	var resp catalogObjectResponse
	if err := c.Get(ctx, "/catalog/object/"+url.PathEscape(id), &resp); err != nil {
		return nil, err
	}
	return &resp.Object, nil
}

// presentAt reports whether a catalog object is active at a location.
// An empty locationID means "anywhere", used when Mise wants the whole
// catalog regardless of scoping.
func (o catalogObject) presentAt(locationID string) bool {
	if locationID == "" {
		return true
	}
	if o.PresentAtAllLocations {
		return !containsString(o.AbsentAtLocationIDs, locationID)
	}
	return containsString(o.PresentAtLocationIDs, locationID)
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// displayName returns the human-readable name Square shows for an
// object, which Mise uses as the basis for its config name.
func (o catalogObject) displayName() string {
	switch {
	case o.ItemData != nil:
		return o.ItemData.Name
	case o.CategoryData != nil:
		return o.CategoryData.Name
	case o.TaxData != nil:
		return o.TaxData.Name
	case o.DiscountData != nil:
		return o.DiscountData.Name
	case o.ModifierListData != nil:
		return o.ModifierListData.Name
	default:
		return ""
	}
}

// toResource converts a Square catalog object into Mise's
// platform-agnostic Resource, for the given Mise resource type.
func (o catalogObject) toResource(resourceType, locationID string) *provider.Resource {
	return &provider.Resource{
		Type:       resourceType,
		Name:       o.displayName(),
		ProviderID: o.ID,
		Properties: o.properties(),
		LocationID: locationID,
		Version:    strconv.FormatInt(o.Version, 10),
	}
}

// properties extracts the type-specific payload into a clean property
// map. Square fields that Mise does not manage (timestamps, image IDs,
// reporting categories) are deliberately dropped: config files should
// describe intent, not mirror the API.
func (o catalogObject) properties() map[string]interface{} {
	props := map[string]interface{}{}

	switch {
	case o.TaxData != nil:
		d := o.TaxData
		props["name"] = d.Name
		props["percentage"] = d.Percentage
		props["enabled"] = d.Enabled
		props["applies_to_custom_amounts"] = d.AppliesToCustomAmounts
		setIfNotEmpty(props, "calculation_phase", d.CalculationPhase)
		setIfNotEmpty(props, "inclusion_type", d.InclusionType)

	case o.CategoryData != nil:
		props["name"] = o.CategoryData.Name

	case o.DiscountData != nil:
		d := o.DiscountData
		props["name"] = d.Name
		props["pin_required"] = d.PinRequired
		setIfNotEmpty(props, "discount_type", d.DiscountType)
		setIfNotEmpty(props, "percentage", d.Percentage)
		setIfNotEmpty(props, "label_color", d.LabelColor)
		if d.AmountMoney != nil {
			props["amount_money"] = moneyProperties(d.AmountMoney)
		}

	case o.ModifierListData != nil:
		d := o.ModifierListData
		props["name"] = d.Name
		setIfNotEmpty(props, "selection_type", d.SelectionType)
		if mods := modifierProperties(d.Modifiers); len(mods) > 0 {
			props["modifiers"] = mods
		}

	case o.ItemData != nil:
		d := o.ItemData
		props["name"] = d.Name
		setIfNotEmpty(props, "description", d.Description)
		setIfNotEmpty(props, "abbreviation", d.Abbreviation)
		setIfNotEmpty(props, "product_type", d.ProductType)

		// References are emitted as provider.Ref so the engine can turn
		// them into ref(type.name) once every resource has a name.
		if d.CategoryID != "" {
			props["category"] = provider.Ref{ResourceType: TypeCategory, ProviderID: d.CategoryID}
		}
		if taxes := refsFor(TypeTax, d.TaxIDs); len(taxes) > 0 {
			props["tax_ids"] = taxes
		}
		if lists := modifierListRefs(d.ModifierListInfo); len(lists) > 0 {
			props["modifier_lists"] = lists
		}
		if variations := variationProperties(d.Variations); len(variations) > 0 {
			props["variations"] = variations
		}
	}

	return props
}

// variationProperties flattens Square's nested ITEM_VARIATION objects
// into the shape the PRD's example config uses.
func variationProperties(variations []catalogObject) []interface{} {
	out := make([]interface{}, 0, len(variations))
	for _, v := range variations {
		if v.VariationData == nil {
			continue
		}
		entry := map[string]interface{}{"name": v.VariationData.Name}
		setIfNotEmpty(entry, "sku", v.VariationData.SKU)
		setIfNotEmpty(entry, "pricing_type", v.VariationData.PricingType)
		if v.VariationData.PriceMoney != nil {
			entry["price_money"] = moneyProperties(v.VariationData.PriceMoney)
		}
		out = append(out, entry)
	}
	return out
}

// modifierProperties flattens the modifiers nested inside a modifier list.
func modifierProperties(modifiers []catalogObject) []interface{} {
	out := make([]interface{}, 0, len(modifiers))
	for _, m := range modifiers {
		if m.ModifierData == nil {
			continue
		}
		entry := map[string]interface{}{"name": m.ModifierData.Name}
		if m.ModifierData.PriceMoney != nil {
			entry["price_money"] = moneyProperties(m.ModifierData.PriceMoney)
		}
		out = append(out, entry)
	}
	return out
}

// modifierListRefs converts an item's modifier list attachments into refs.
func modifierListRefs(infos []modifierListInfo) []interface{} {
	out := make([]interface{}, 0, len(infos))
	for _, info := range infos {
		if info.ModifierListID == "" {
			continue
		}
		out = append(out, provider.Ref{ResourceType: TypeModifierList, ProviderID: info.ModifierListID})
	}
	return out
}

// refsFor wraps a list of provider IDs as references of one type,
// sorted so that fetch output does not churn between runs.
func refsFor(resourceType string, ids []string) []interface{} {
	sorted := append([]string(nil), ids...)
	sort.Strings(sorted)

	out := make([]interface{}, 0, len(sorted))
	for _, id := range sorted {
		if id == "" {
			continue
		}
		out = append(out, provider.Ref{ResourceType: resourceType, ProviderID: id})
	}
	return out
}

// moneyProperties renders a Square Money value. Amounts stay in the
// smallest currency unit (cents), matching the API and the PRD example.
func moneyProperties(m *money) map[string]interface{} {
	out := map[string]interface{}{"amount": m.Amount}
	setIfNotEmpty(out, "currency", m.Currency)
	return out
}

// setIfNotEmpty adds a key only when the value is non-empty, keeping
// generated YAML free of noise like `description: ""`.
func setIfNotEmpty(props map[string]interface{}, key, value string) {
	if strings.TrimSpace(value) != "" {
		props[key] = value
	}
}
