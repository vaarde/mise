package square

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

func TestPrimaryCategoryPrefersTheReportingCategory(t *testing.T) {
	id := primaryCategoryID(&itemData{
		ReportingCategory: &categoryRef{ID: "CAT_REPORTING"},
		Categories:        []categoryRef{{ID: "CAT_OTHER"}},
		CategoryID:        "CAT_LEGACY",
	})

	// reporting_category is Square's answer to "which category does this
	// item belong to", so it outranks both the list and the old field.
	assert.Equal(t, "CAT_REPORTING", id)
}

func TestPrimaryCategoryFallsBackToTheCategoriesList(t *testing.T) {
	id := primaryCategoryID(&itemData{
		Categories: []categoryRef{{ID: "CAT_FOOD", Ordinal: 1}, {ID: "CAT_BEV", Ordinal: 0}},
	})

	// Lowest ordinal wins, not first-in-array: Square is free to return
	// the list in any order, and position would make a re-ordering read
	// as a change on the next fetch.
	assert.Equal(t, "CAT_BEV", id)
}

func TestPrimaryCategoryFallsBackToTheRetiredField(t *testing.T) {
	// Objects written before the API pin moved still carry category_id,
	// and must keep resolving.
	assert.Equal(t, "CAT_LEGACY", primaryCategoryID(&itemData{CategoryID: "CAT_LEGACY"}))
}

func TestPrimaryCategoryIsEmptyWhenUncategorized(t *testing.T) {
	assert.Empty(t, primaryCategoryID(&itemData{}))
	assert.Empty(t, primaryCategoryID(&itemData{
		ReportingCategory: &categoryRef{},
		Categories:        []categoryRef{{ID: ""}},
	}))
}

// itemWithCategoryJSON renders a catalog list response for an item whose
// category is expressed however the caller asks for.
func itemWithCategoryJSON(categoryFields string) string {
	return fmt.Sprintf(`{
  "objects": [
    {
      "type": "ITEM", "id": "ITEM_1", "version": 12,
      "present_at_all_locations": true,
      "item_data": {"name": "Peach Sweet Tea", %s}
    }
  ]
}`, categoryFields)
}

func readItemProps(t *testing.T, body string) map[string]interface{} {
	t.Helper()

	srv := quietServer(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, body)
	})
	t.Cleanup(srv.Close)

	p := &SquareProvider{}
	require.NoError(t, p.Configure(provider.ProviderConfig{
		Platform:    ProviderName,
		Environment: "sandbox",
		Credentials: provider.CredentialsConfig{Method: "access_token", AccessToken: "tok"},
	}))
	p.client = NewClient(srv.URL, "tok")

	resources, err := p.ReadAll(context.Background(), TypeItem, "")
	require.NoError(t, err)
	require.Len(t, resources, 1)

	return resources[0].Properties
}

func TestReadItemResolvesCategoryFromTheCurrentShape(t *testing.T) {
	props := readItemProps(t, itemWithCategoryJSON(
		`"categories": [{"id": "CAT_SEASONAL", "ordinal": 0}],
         "reporting_category": {"id": "CAT_SEASONAL", "ordinal": 0}`))

	assert.Equal(t,
		provider.Ref{ResourceType: TypeCategory, ProviderID: "CAT_SEASONAL"},
		props["category"])
}

func TestReadItemResolvesCategoryFromTheRetiredShape(t *testing.T) {
	props := readItemProps(t, itemWithCategoryJSON(`"category_id": "CAT_OLD"`))

	assert.Equal(t,
		provider.Ref{ResourceType: TypeCategory, ProviderID: "CAT_OLD"},
		props["category"])
}

func TestReadItemOmitsCategoryWhenUncategorized(t *testing.T) {
	props := readItemProps(t, itemWithCategoryJSON(`"product_type": "REGULAR"`))

	// An absent category must not become an empty ref, or every
	// uncategorized item would carry a reference to nothing.
	assert.NotContains(t, props, "category")
}

func TestCategoryWriteRoundTripsThroughRead(t *testing.T) {
	// What buildItemData sends must be what parsing reads back, or a
	// fetch straight after an apply would report a spurious change.
	data, err := buildItemData(map[string]interface{}{
		"name":     "Peach Sweet Tea",
		"category": "CAT_SEASONAL",
	}, TypeItem, nil)
	require.NoError(t, err)

	encoded, err := json.Marshal(data)
	require.NoError(t, err)

	var parsed itemData
	require.NoError(t, json.Unmarshal(encoded, &parsed))

	assert.Equal(t, "CAT_SEASONAL", primaryCategoryID(&parsed))
}
