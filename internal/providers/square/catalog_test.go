package square

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

// catalogFixture is a small but realistic Square catalog: two taxes
// (one account-wide, one Georgia-only), a category, and an item that
// references both the category and a tax.
const (
	locAtlanta   = "LOC_ATL"
	locNashville = "LOC_NSH"
)

const taxObjectsJSON = `{
  "objects": [
    {
      "type": "TAX", "id": "TAX_STATE", "version": 1700000000000,
      "present_at_all_locations": true,
      "tax_data": {
        "name": "GA State Sales Tax",
        "calculation_phase": "TAX_SUBTOTAL_PHASE",
        "inclusion_type": "ADDITIVE",
        "percentage": "4.5",
        "applies_to_custom_amounts": true,
        "enabled": true
      }
    },
    {
      "type": "TAX", "id": "TAX_ALCOHOL", "version": 1700000000001,
      "present_at_all_locations": false,
      "present_at_location_ids": ["LOC_ATL"],
      "tax_data": {
        "name": "GA Alcohol Tax",
        "calculation_phase": "TAX_SUBTOTAL_PHASE",
        "inclusion_type": "ADDITIVE",
        "percentage": "6.0",
        "enabled": true
      }
    },
    {
      "type": "TAX", "id": "TAX_DELETED", "version": 1700000000002,
      "is_deleted": true,
      "present_at_all_locations": true,
      "tax_data": {"name": "Retired Tax", "percentage": "9.9"}
    }
  ]
}`

const categoryObjectsJSON = `{
  "objects": [
    {
      "type": "CATEGORY", "id": "CAT_BEV", "version": 1700000000010,
      "present_at_all_locations": true,
      "category_data": {"name": "Beverages"}
    }
  ]
}`

const itemObjectsJSON = `{
  "objects": [
    {
      "type": "ITEM", "id": "ITEM_LEMONADE", "version": 1700000000020,
      "present_at_all_locations": true,
      "absent_at_location_ids": ["LOC_NSH"],
      "item_data": {
        "name": "Summer Lemonade",
        "description": "Fresh-squeezed lemonade with mint",
        "category_id": "CAT_BEV",
        "tax_ids": ["TAX_STATE", "TAX_ALCOHOL"],
        "product_type": "REGULAR",
        "modifier_list_info": [{"modifier_list_id": "MOD_SIZE", "enabled": true}],
        "variations": [
          {
            "type": "ITEM_VARIATION", "id": "VAR_REG",
            "item_variation_data": {
              "name": "Regular", "pricing_type": "FIXED_PRICING",
              "price_money": {"amount": 450, "currency": "USD"}
            }
          },
          {
            "type": "ITEM_VARIATION", "id": "VAR_LRG",
            "item_variation_data": {
              "name": "Large", "pricing_type": "FIXED_PRICING",
              "price_money": {"amount": 595, "currency": "USD"}
            }
          }
        ]
      }
    }
  ]
}`

const modifierListObjectsJSON = `{
  "objects": [
    {
      "type": "MODIFIER_LIST", "id": "MOD_SIZE", "version": 1700000000030,
      "present_at_all_locations": true,
      "modifier_list_data": {
        "name": "Size",
        "selection_type": "SINGLE",
        "modifiers": [
          {"type": "MODIFIER", "id": "MOD_S", "modifier_data": {"name": "Small", "price_money": {"amount": 0, "currency": "USD"}}},
          {"type": "MODIFIER", "id": "MOD_L", "modifier_data": {"name": "Large", "price_money": {"amount": 100, "currency": "USD"}}}
        ]
      }
    }
  ]
}`

const discountObjectsJSON = `{
  "objects": [
    {
      "type": "DISCOUNT", "id": "DISC_HH", "version": 1700000000040,
      "present_at_all_locations": true,
      "discount_data": {
        "name": "Happy Hour",
        "discount_type": "FIXED_PERCENTAGE",
        "percentage": "15",
        "pin_required": false,
        "label_color": "9da2a6"
      }
    }
  ]
}`

const twoLocationsJSON = `{
  "locations": [
    {"id": "LOC_ATL", "name": "Atlanta", "status": "ACTIVE", "type": "PHYSICAL", "timezone": "America/New_York",
     "address": {"locality": "Atlanta", "administrative_district_level_1": "GA"}},
    {"id": "LOC_NSH", "name": "Nashville", "status": "ACTIVE", "type": "PHYSICAL", "timezone": "America/Chicago",
     "address": {"locality": "Nashville", "administrative_district_level_1": "TN"}}
  ]
}`

// catalogServer serves the fixture catalog and counts requests per path,
// so tests can assert how many API calls a fetch actually makes.
type catalogServer struct {
	*httptest.Server
	mu    sync.Mutex
	calls map[string]int
}

func newCatalogServer(t *testing.T) *catalogServer {
	t.Helper()

	cs := &catalogServer{calls: map[string]int{}}
	cs.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/locations" {
			cs.record("locations")
			_, _ = w.Write([]byte(twoLocationsJSON))
			return
		}

		if r.URL.Path == "/catalog/list" {
			squareType := r.URL.Query().Get("types")
			cs.record("list:" + squareType)

			switch squareType {
			case "TAX":
				_, _ = w.Write([]byte(taxObjectsJSON))
			case "CATEGORY":
				_, _ = w.Write([]byte(categoryObjectsJSON))
			case "ITEM":
				_, _ = w.Write([]byte(itemObjectsJSON))
			case "MODIFIER_LIST":
				_, _ = w.Write([]byte(modifierListObjectsJSON))
			case "DISCOUNT":
				_, _ = w.Write([]byte(discountObjectsJSON))
			default:
				_, _ = w.Write([]byte(`{"objects": []}`))
			}
			return
		}

		if len(r.URL.Path) > len("/catalog/object/") && r.URL.Path[:len("/catalog/object/")] == "/catalog/object/" {
			id := r.URL.Path[len("/catalog/object/"):]
			cs.record("object:" + id)

			var listing catalogListResponse
			require.NoError(t, json.Unmarshal([]byte(taxObjectsJSON), &listing))
			for _, obj := range listing.Objects {
				if obj.ID == id {
					_ = json.NewEncoder(w).Encode(catalogObjectResponse{Object: obj})
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"errors":[{"code":"NOT_FOUND","detail":"object not found"}]}`))
			return
		}

		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(cs.Close)
	return cs
}

func (cs *catalogServer) record(key string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.calls[key]++
}

func (cs *catalogServer) callCount(key string) int {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.calls[key]
}

// configuredProvider returns a Square provider pointed at a test server.
func configuredProvider(t *testing.T, serverURL string) *SquareProvider {
	t.Helper()

	p := &SquareProvider{}
	require.NoError(t, p.Configure(provider.ProviderConfig{
		Platform:    ProviderName,
		Environment: "sandbox",
		Credentials: provider.CredentialsConfig{AccessToken: "test-token"},
	}))
	p.client = NewClient(serverURL, "test-token")
	return p
}

func TestPresentAtLocation(t *testing.T) {
	tests := []struct {
		name       string
		object     catalogObject
		locationID string
		want       bool
	}{
		{
			name:       "present at all locations",
			object:     catalogObject{PresentAtAllLocations: true},
			locationID: locAtlanta,
			want:       true,
		},
		{
			name: "present at all except this one",
			object: catalogObject{
				PresentAtAllLocations: true,
				AbsentAtLocationIDs:   []string{locNashville},
			},
			locationID: locNashville,
			want:       false,
		},
		{
			name: "explicit list, included",
			object: catalogObject{
				PresentAtLocationIDs: []string{locAtlanta},
			},
			locationID: locAtlanta,
			want:       true,
		},
		{
			name: "explicit list, excluded",
			object: catalogObject{
				PresentAtLocationIDs: []string{locAtlanta},
			},
			locationID: locNashville,
			want:       false,
		},
		{
			name:       "no location filter means anywhere",
			object:     catalogObject{PresentAtLocationIDs: []string{locAtlanta}},
			locationID: "",
			want:       true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.object.presentAt(tc.locationID))
		})
	}
}

func TestReadAllTaxesFiltersByLocation(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	atlanta, err := p.ReadAll(context.Background(), TypeTax, locAtlanta)
	require.NoError(t, err)
	require.Len(t, atlanta, 2, "Atlanta gets the account-wide tax and the Georgia-only one")

	nashville, err := p.ReadAll(context.Background(), TypeTax, locNashville)
	require.NoError(t, err)
	require.Len(t, nashville, 1, "Nashville only gets the account-wide tax")
	assert.Equal(t, "TAX_STATE", nashville[0].ProviderID)
}

func TestReadAllSkipsDeletedObjects(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	taxes, err := p.ReadAll(context.Background(), TypeTax, "")
	require.NoError(t, err)

	for _, tax := range taxes {
		assert.NotEqual(t, "TAX_DELETED", tax.ProviderID,
			"Square tombstones deleted objects; they are not live configuration")
	}
}

func TestReadAllCachesTheCatalogAcrossLocations(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	for _, locationID := range []string{locAtlanta, locNashville, locAtlanta} {
		_, err := p.ReadAll(context.Background(), TypeTax, locationID)
		require.NoError(t, err)
	}

	assert.Equal(t, 1, srv.callCount("list:TAX"),
		"Square's catalog is account-wide, so it must be listed once and filtered per location")
}

func TestReadAllCachesConcurrently(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.ReadAll(context.Background(), TypeTax, locAtlanta)
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	assert.Equal(t, 1, srv.callCount("list:TAX"), "concurrent readers must share one listing")
}

func TestTaxProperties(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	taxes, err := p.ReadAll(context.Background(), TypeTax, locAtlanta)
	require.NoError(t, err)

	var stateTax *provider.Resource
	for _, tax := range taxes {
		if tax.ProviderID == "TAX_STATE" {
			stateTax = tax
		}
	}
	require.NotNil(t, stateTax)

	assert.Equal(t, "GA State Sales Tax", stateTax.Name)
	assert.Equal(t, "1700000000000", stateTax.Version, "the version token is stored as a string")
	assert.Equal(t, map[string]interface{}{
		"name":                      "GA State Sales Tax",
		"percentage":                "4.5",
		"enabled":                   true,
		"applies_to_custom_amounts": true,
		"calculation_phase":         "TAX_SUBTOTAL_PHASE",
		"inclusion_type":            "ADDITIVE",
	}, stateTax.Properties)
}

func TestItemPropertiesEmitReferences(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	items, err := p.ReadAll(context.Background(), TypeItem, locAtlanta)
	require.NoError(t, err)
	require.Len(t, items, 1)

	props := items[0].Properties
	assert.Equal(t, "Summer Lemonade", props["name"])
	assert.Equal(t, "Fresh-squeezed lemonade with mint", props["description"])

	// The category is a reference, not a raw Square ID.
	assert.Equal(t, provider.Ref{ResourceType: TypeCategory, ProviderID: "CAT_BEV"}, props["category"])

	// Tax references are sorted, so fetch output does not churn when
	// Square returns them in a different order.
	assert.Equal(t, []interface{}{
		provider.Ref{ResourceType: TypeTax, ProviderID: "TAX_ALCOHOL"},
		provider.Ref{ResourceType: TypeTax, ProviderID: "TAX_STATE"},
	}, props["tax_ids"])

	assert.Equal(t, []interface{}{
		provider.Ref{ResourceType: TypeModifierList, ProviderID: "MOD_SIZE"},
	}, props["modifier_lists"])

	assert.Equal(t, []interface{}{
		map[string]interface{}{
			"name": "Regular", "pricing_type": "FIXED_PRICING",
			"price_money": map[string]interface{}{"amount": int64(450), "currency": "USD"},
		},
		map[string]interface{}{
			"name": "Large", "pricing_type": "FIXED_PRICING",
			"price_money": map[string]interface{}{"amount": int64(595), "currency": "USD"},
		},
	}, props["variations"])
}

func TestItemLocationScopingUsesAbsentList(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	nashville, err := p.ReadAll(context.Background(), TypeItem, locNashville)
	require.NoError(t, err)
	assert.Empty(t, nashville, "the item is present_at_all_locations but absent at Nashville")
}

func TestModifierListProperties(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	lists, err := p.ReadAll(context.Background(), TypeModifierList, locAtlanta)
	require.NoError(t, err)
	require.Len(t, lists, 1)

	props := lists[0].Properties
	assert.Equal(t, "Size", props["name"])
	assert.Equal(t, "SINGLE", props["selection_type"])
	assert.Equal(t, []interface{}{
		map[string]interface{}{"name": "Small", "price_money": map[string]interface{}{"amount": int64(0), "currency": "USD"}},
		map[string]interface{}{"name": "Large", "price_money": map[string]interface{}{"amount": int64(100), "currency": "USD"}},
	}, props["modifiers"])
}

func TestDiscountProperties(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	discounts, err := p.ReadAll(context.Background(), TypeDiscount, locAtlanta)
	require.NoError(t, err)
	require.Len(t, discounts, 1)

	assert.Equal(t, map[string]interface{}{
		"name":          "Happy Hour",
		"discount_type": "FIXED_PERCENTAGE",
		"percentage":    "15",
		"pin_required":  false,
		"label_color":   "9da2a6",
	}, discounts[0].Properties)
}

func TestEmptyStringPropertiesAreOmitted(t *testing.T) {
	obj := catalogObject{
		ID:   "CAT_X",
		Type: "CATEGORY",
		ItemData: &itemData{
			Name:        "Plain Item",
			Description: "   ",
		},
	}

	props := obj.properties()
	assert.Equal(t, "Plain Item", props["name"])
	assert.NotContains(t, props, "description", "blank values should not clutter the generated YAML")
	assert.NotContains(t, props, "abbreviation")
}

func TestReadAllLocations(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	locations, err := p.ReadAll(context.Background(), TypeLocation, "")
	require.NoError(t, err)
	require.Len(t, locations, 2)
	assert.Equal(t, "GA", locations[0].Properties["state"])

	single, err := p.ReadAll(context.Background(), TypeLocation, locNashville)
	require.NoError(t, err)
	require.Len(t, single, 1)
	assert.Equal(t, locNashville, single[0].ProviderID)
}

func TestReadAllRejectsUnknownType(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	_, err := p.ReadAll(context.Background(), "square_catalog_pizza", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support resource type")
	assert.Contains(t, err.Error(), "square_catalog_tax", "the error should list what is supported")
}

func TestReadSingleObject(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	res, err := p.Read(context.Background(), TypeTax, "TAX_STATE", "")
	require.NoError(t, err)
	assert.Equal(t, "GA State Sales Tax", res.Name)
	assert.Equal(t, "4.5", res.Properties["percentage"])
}

func TestReadRejectsObjectNotAtLocation(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	_, err := p.Read(context.Background(), TypeTax, "TAX_ALCOHOL", locNashville)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not present at location")
}

func TestReadRejectsDeletedObject(t *testing.T) {
	srv := newCatalogServer(t)
	p := configuredProvider(t, srv.URL)

	_, err := p.Read(context.Background(), TypeTax, "TAX_DELETED", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "deleted")
}

func TestListCatalogObjectsFollowsPagination(t *testing.T) {
	var pages int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pages++
		cursor := r.URL.Query().Get("cursor")

		w.Header().Set("Content-Type", "application/json")
		switch cursor {
		case "":
			_, _ = w.Write([]byte(`{"objects":[{"type":"TAX","id":"T1","tax_data":{"name":"One"}}],"cursor":"page2"}`))
		case "page2":
			_, _ = w.Write([]byte(`{"objects":[{"type":"TAX","id":"T2","tax_data":{"name":"Two"}}],"cursor":"page3"}`))
		default:
			_, _ = w.Write([]byte(`{"objects":[{"type":"TAX","id":"T3","tax_data":{"name":"Three"}}]}`))
		}
	}))
	t.Cleanup(srv.Close)

	objects, err := NewClient(srv.URL, "tok").ListCatalogObjects(context.Background(), "TAX")
	require.NoError(t, err)

	assert.Equal(t, 3, pages)
	require.Len(t, objects, 3)
	assert.Equal(t, []string{"T1", "T2", "T3"}, []string{objects[0].ID, objects[1].ID, objects[2].ID})
}

func TestListCatalogObjectsStopsOnRepeatingCursor(t *testing.T) {
	// A cursor that never advances would otherwise loop forever.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"objects":[],"cursor":"always-the-same"}`))
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, "tok").ListCatalogObjects(context.Background(), "TAX")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeating pagination cursor")
}

func TestListCatalogObjectsPropagatesAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","detail":"nope"}]}`))
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, "tok").ListCatalogObjects(context.Background(), "TAX")
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok, fmt.Sprintf("expected *APIError, got %T", err))
	assert.Equal(t, "UNAUTHORIZED", apiErr.Code)
}
