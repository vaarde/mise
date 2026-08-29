package square

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

func TestCategoriesAreAlwaysWrittenAccountWide(t *testing.T) {
	object := map[string]interface{}{}
	applyLocationScope(object, TypeCategory, []string{"LOC_A", "LOC_B"})

	// Square rejects an explicit list on a CATEGORY outright:
	// "Unexpected unit-overridden attribute for definition
	// present_at_all_locations".
	assert.Equal(t, true, object["present_at_all_locations"])
	assert.NotContains(t, object, "present_at_location_ids")
}

func TestOtherTypesKeepTheirExplicitLocationList(t *testing.T) {
	for _, resourceType := range []string{TypeTax, TypeDiscount, TypeItem, TypeModifierList} {
		t.Run(resourceType, func(t *testing.T) {
			object := map[string]interface{}{}
			applyLocationScope(object, resourceType, []string{"LOC_B", "LOC_A"})

			assert.Equal(t, false, object["present_at_all_locations"])
			assert.Equal(t, []string{"LOC_A", "LOC_B"}, object["present_at_location_ids"],
				"the list is sorted so a re-ordering is not a change")
		})
	}
}

func TestNoLocationsMeansAccountWide(t *testing.T) {
	object := map[string]interface{}{}
	applyLocationScope(object, TypeTax, nil)

	assert.Equal(t, true, object["present_at_all_locations"])
	assert.NotContains(t, object, "present_at_location_ids")
}

func TestVariationsInheritTheItemsLocationScope(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeItem, "peach_sweet_tea", map[string]interface{}{
			"name": "Peach Sweet Tea",
			"variations": []interface{}{
				map[string]interface{}{"name": "Regular"},
			},
		}, []string{"LOC_ATL", "LOC_SAV"}),
	})
	require.NoError(t, err)

	item := srv.lastObjects(t)[0]
	data := item["item_data"].(map[string]interface{})
	variation := data["variations"].([]interface{})[0].(map[string]interface{})

	// Square refuses a variation that is present at more locations than
	// its item: "enabled at all future locations, but the referenced
	// object ... is not".
	assert.Equal(t, item["present_at_all_locations"], variation["present_at_all_locations"])
	assert.Equal(t, item["present_at_location_ids"], variation["present_at_location_ids"])
}

func TestModifiersInheritTheListsLocationScope(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeModifierList, "size", map[string]interface{}{
			"name": "Size",
			"modifiers": []interface{}{
				map[string]interface{}{"name": "Large"},
			},
		}, []string{"LOC_NSH"}),
	})
	require.NoError(t, err)

	list := srv.lastObjects(t)[0]
	data := list["modifier_list_data"].(map[string]interface{})
	modifier := data["modifiers"].([]interface{})[0].(map[string]interface{})

	assert.Equal(t, list["present_at_all_locations"], modifier["present_at_all_locations"])
	assert.Equal(t, list["present_at_location_ids"], modifier["present_at_location_ids"])
}

// locationServer answers ListLocations, then the write.
func locationServer(t *testing.T, locationIDs []string) *writeServer {
	t.Helper()

	body := `{"locations":[`
	for i, id := range locationIDs {
		if i > 0 {
			body += ","
		}
		body += `{"id":"` + id + `","name":"` + id + `"}`
	}
	body += `]}`

	return newWriteServer(t, body)
}

func TestCategoryScopedToEveryLocationIsAccepted(t *testing.T) {
	srv := locationServer(t, []string{"LOC_A", "LOC_B"})
	p := configuredProvider(t, srv.URL)

	// ${group.all} resolves to the full list, which Square can express
	// exactly as present_at_all_locations.
	err := p.checkLocationScope(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeCategory, "beverages",
			map[string]interface{}{"name": "Beverages"}, []string{"LOC_A", "LOC_B"}),
	})
	assert.NoError(t, err)
}

func TestCategoryScopedToSomeLocationsIsRejected(t *testing.T) {
	srv := locationServer(t, []string{"LOC_A", "LOC_B", "LOC_C"})
	p := configuredProvider(t, srv.URL)

	err := p.checkLocationScope(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeCategory, "beverages",
			map[string]interface{}{"name": "Beverages"}, []string{"LOC_A"}),
	})

	// Silently widening would be worse than refusing: the operator would
	// see the apply succeed and only learn from the next fetch that the
	// category had gone up everywhere.
	require.Error(t, err)
	assert.Contains(t, err.Error(), "every location")
	assert.Contains(t, err.Error(), "${group.all}")
}

func TestScopeCheckIgnoresTypesSquareCanScope(t *testing.T) {
	srv := newWriteServer(t, `{}`)
	p := configuredProvider(t, srv.URL)

	// A narrowly-scoped tax is legitimate, and must not cost a
	// locations lookup.
	require.NoError(t, p.checkLocationScope(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "city_tax",
			map[string]interface{}{"name": "City Tax"}, []string{"LOC_A"}),
	}))
	assert.Empty(t, srv.requests, "no location lookup should have been needed")
}

func TestAccountLocationIDsAreCachedForTheRun(t *testing.T) {
	srv := locationServer(t, []string{"LOC_A"})
	p := configuredProvider(t, srv.URL)

	first, err := p.accountLocationIDs(context.Background())
	require.NoError(t, err)
	second, err := p.accountLocationIDs(context.Background())
	require.NoError(t, err)

	assert.Equal(t, first, second)
	assert.Len(t, srv.requests, 1, "a batch must not re-list locations per operation")
}
