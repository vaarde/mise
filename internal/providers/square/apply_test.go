package square

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

// writeServer captures the request bodies Mise sends and replies with a
// canned response.
type writeServer struct {
	*httptest.Server
	requests []map[string]interface{}
	paths    []string
	status   int
	response string
}

func newWriteServer(t *testing.T, response string) *writeServer {
	t.Helper()

	ws := &writeServer{status: http.StatusOK, response: response}
	ws.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var parsed map[string]interface{}
		if len(body) > 0 {
			require.NoError(t, json.Unmarshal(body, &parsed))
		}
		ws.requests = append(ws.requests, parsed)
		ws.paths = append(ws.paths, r.URL.Path)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(ws.status)
		_, _ = w.Write([]byte(ws.response))
	}))
	t.Cleanup(ws.Close)
	return ws
}

// lastObjects returns the catalog objects from the most recent batch.
func (ws *writeServer) lastObjects(t *testing.T) []map[string]interface{} {
	t.Helper()
	require.NotEmpty(t, ws.requests)

	batches, ok := ws.requests[len(ws.requests)-1]["batches"].([]interface{})
	require.True(t, ok, "request should contain batches")
	require.Len(t, batches, 1)

	objects, ok := batches[0].(map[string]interface{})["objects"].([]interface{})
	require.True(t, ok)

	out := make([]map[string]interface{}, 0, len(objects))
	for _, o := range objects {
		out = append(out, o.(map[string]interface{}))
	}
	return out
}

func writeOp(action provider.WriteAction, resourceType, name string, props map[string]interface{}, locations []string) provider.WriteOperation {
	return provider.WriteOperation{
		Action: action,
		Name:   resourceType + "." + name,
		Resource: &provider.Resource{
			Type:        resourceType,
			Name:        name,
			Properties:  props,
			LocationIDs: locations,
		},
	}
}

func TestApplyBatchCreatesTax(t *testing.T) {
	srv := newWriteServer(t, `{
      "objects": [{"type":"TAX","id":"TAX_REAL","version":1700000000999,
                   "tax_data":{"name":"GA State Sales Tax","percentage":"4.5"}}],
      "id_mappings": [{"client_object_id":"#ga_state_sales_tax","object_id":"TAX_REAL"}]
    }`)
	p := configuredProvider(t, srv.URL)

	ops := []provider.WriteOperation{writeOp(provider.WriteCreate, TypeTax, "ga_state_sales_tax",
		map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
			"calculation_phase": "TAX_SUBTOTAL_PHASE", "inclusion_type": "ADDITIVE",
		}, []string{"LOC_ATL"})}

	outcomes, err := p.ApplyBatch(context.Background(), ops)
	require.NoError(t, err)

	assert.Equal(t, "/catalog/batch-upsert", srv.paths[0])

	objects := srv.lastObjects(t)
	require.Len(t, objects, 1)
	assert.Equal(t, "TAX", objects[0]["type"])
	assert.Equal(t, "#ga_state_sales_tax", objects[0]["id"],
		"a create uses a temporary ID that Square maps to a real one")

	data := objects[0]["tax_data"].(map[string]interface{})
	assert.Equal(t, "GA State Sales Tax", data["name"])
	assert.Equal(t, "4.5", data["percentage"])
	assert.Equal(t, "TAX_SUBTOTAL_PHASE", data["calculation_phase"])

	// The real ID and version come back for state.
	require.Len(t, outcomes, 1)
	assert.NoError(t, outcomes[0].Err)
	assert.Equal(t, "TAX_REAL", outcomes[0].ProviderID)
	assert.Equal(t, "1700000000999", outcomes[0].Version)
}

func TestApplyBatchSetsLocationScope(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t",
			map[string]interface{}{"percentage": "1.0"}, []string{"LOC_B", "LOC_A"}),
	})
	require.NoError(t, err)

	object := srv.lastObjects(t)[0]
	assert.Equal(t, false, object["present_at_all_locations"])
	assert.Equal(t, []interface{}{"LOC_A", "LOC_B"}, object["present_at_location_ids"],
		"location IDs are sorted so the request is stable")
}

func TestApplyBatchRefusesAnEmptyLocationScope(t *testing.T) {
	// Square reads an object with no location list as present at every
	// location. A location group that matched nothing — one typo in a
	// state code — would therefore widen the write to the whole account
	// instead of narrowing it. Nothing may be sent.
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	outcomes, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "1.0"}, nil),
	})
	require.NoError(t, err, "one bad operation does not fail the whole batch")

	require.Len(t, outcomes, 1)
	require.Error(t, outcomes[0].Err)
	assert.Contains(t, outcomes[0].Err.Error(), "no locations to write to")

	assert.Empty(t, srv.requests, "nothing reaches Square when every operation is rejected")
}

func TestApplyBatchUpdatePassesVersionForConcurrency(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[{"type":"TAX","id":"TAX_1","version":1700000001000}],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	op := writeOp(provider.WriteUpdate, TypeTax, "ga_tax",
		map[string]interface{}{"percentage": "5.0"}, []string{"LOC_ATL"})
	op.Resource.ProviderID = "TAX_1"
	op.Resource.Version = "1700000000999"

	outcomes, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{op})
	require.NoError(t, err)

	object := srv.lastObjects(t)[0]
	assert.Equal(t, "TAX_1", object["id"], "an update targets the real ID")
	assert.Equal(t, float64(1700000000999), object["version"],
		"passing the version back is what makes a concurrent change fail instead of being overwritten")

	assert.Equal(t, "1700000001000", outcomes[0].Version, "the new version is stored for next time")
}

func TestApplyBatchUpdateRejectsMissingProviderID(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	op := writeOp(provider.WriteUpdate, TypeTax, "ga_tax", map[string]interface{}{}, nil)

	outcomes, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{op})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	require.Error(t, outcomes[0].Err)
	assert.Contains(t, outcomes[0].Err.Error(), "without a provider ID")
}

func TestApplyBatchBuildsItemWithReferencesAndVariations(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeItem, "summer_lemonade", map[string]interface{}{
			"name":           "Summer Lemonade",
			"description":    "Fresh-squeezed",
			"category":       "CAT_BEV",
			"tax_ids":        []interface{}{"TAX_1", "TAX_2"},
			"modifier_lists": []interface{}{"MOD_SIZE"},
			"variations": []interface{}{
				map[string]interface{}{
					"name":        "Regular",
					"price_money": map[string]interface{}{"amount": 450, "currency": "USD"},
				},
			},
		}, []string{"LOC_ATL"}),
	})
	require.NoError(t, err)

	data := srv.lastObjects(t)[0]["item_data"].(map[string]interface{})
	assert.Equal(t, "Summer Lemonade", data["name"])
	// Square discards category_id as of 2024-06-04, so the association
	// has to go up as categories[] plus reporting_category.
	assert.Nil(t, data["category_id"], "the retired field must not be sent")
	assert.Equal(t,
		[]interface{}{map[string]interface{}{"id": "CAT_BEV", "ordinal": float64(0)}},
		data["categories"])
	assert.Equal(t,
		map[string]interface{}{"id": "CAT_BEV", "ordinal": float64(0)},
		data["reporting_category"])
	assert.Equal(t, []interface{}{"TAX_1", "TAX_2"}, data["tax_ids"])

	info := data["modifier_list_info"].([]interface{})
	require.Len(t, info, 1)
	assert.Equal(t, "MOD_SIZE", info[0].(map[string]interface{})["modifier_list_id"])

	variations := data["variations"].([]interface{})
	require.Len(t, variations, 1)
	variation := variations[0].(map[string]interface{})
	assert.Equal(t, "ITEM_VARIATION", variation["type"])

	variationData := variation["item_variation_data"].(map[string]interface{})
	assert.Equal(t, "Regular", variationData["name"])
	assert.Equal(t, "FIXED_PRICING", variationData["pricing_type"], "pricing type defaults when unset")
}

func TestApplyBatchRejectsUnsupportedType(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	outcomes, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, "square_catalog_pizza", "x", map[string]interface{}{}, nil),
	})
	require.NoError(t, err)
	require.Len(t, outcomes, 1)
	require.Error(t, outcomes[0].Err)
	assert.Contains(t, outcomes[0].Err.Error(), "does not support resource type")
}

func TestApplyBatchSendsIdempotencyKey(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	ops := []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "1.0"}, []string{"LOC_A"}),
	}

	_, err := p.ApplyBatch(context.Background(), ops)
	require.NoError(t, err)

	key, ok := srv.requests[0]["idempotency_key"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, key)
	assert.Contains(t, key, "mise-")
}

func TestIdempotencyKeyIsDeterministic(t *testing.T) {
	ops := []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "1.0"}, []string{"LOC_A"}),
	}

	// A retry after a network timeout must replay the same key, or
	// Square creates a second copy of everything.
	assert.Equal(t, idempotencyKey(ops), idempotencyKey(ops))
}

func TestIdempotencyKeyChangesWithContent(t *testing.T) {
	base := []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "1.0"}, []string{"LOC_A"}),
	}
	changed := []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "2.0"}, []string{"LOC_A"}),
	}
	moreLocations := []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "1.0"}, []string{"LOC_A", "LOC_B"}),
	}

	assert.NotEqual(t, idempotencyKey(base), idempotencyKey(changed),
		"a different rate is a different write")
	assert.NotEqual(t, idempotencyKey(base), idempotencyKey(moreLocations),
		"a different location set is a different write")
}

func TestIdempotencyKeyIgnoresOperationOrder(t *testing.T) {
	a := writeOp(provider.WriteCreate, TypeTax, "a", map[string]interface{}{"x": "1"}, nil)
	b := writeOp(provider.WriteCreate, TypeTax, "b", map[string]interface{}{"x": "2"}, nil)

	assert.Equal(t,
		idempotencyKey([]provider.WriteOperation{a, b}),
		idempotencyKey([]provider.WriteOperation{b, a}),
		"the same batch in a different order is the same write")
}

func TestApplyBatchPropagatesAPIErrors(t *testing.T) {
	srv := newWriteServer(t, `{"errors":[{"code":"BAD_REQUEST","detail":"percentage must be numeric"}]}`)
	srv.status = http.StatusBadRequest
	p := configuredProvider(t, srv.URL)

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{"percentage": "abc"}, []string{"LOC_ATL"}),
	})
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok)
	assert.Equal(t, "BAD_REQUEST", apiErr.Code)
}

func TestApplyBatchRejectsOversizedBatch(t *testing.T) {
	srv := newWriteServer(t, `{"objects":[],"id_mappings":[]}`)
	p := configuredProvider(t, srv.URL)

	ops := make([]provider.WriteOperation, maxBatchObjects+1)
	for i := range ops {
		ops[i] = writeOp(provider.WriteCreate, TypeTax, "t", map[string]interface{}{}, nil)
	}

	_, err := p.ApplyBatch(context.Background(), ops)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds Square's limit")
}

func TestApplyBatchInvalidatesTheCatalogCache(t *testing.T) {
	// A read after a write must not serve the pre-apply snapshot.
	catalog := newCatalogServer(t)
	p := configuredProvider(t, catalog.URL)

	_, err := p.ReadAll(context.Background(), TypeTax, locAtlanta)
	require.NoError(t, err)
	require.Equal(t, 1, catalog.callCount("list:TAX"))

	p.invalidateCatalogCache()

	_, err = p.ReadAll(context.Background(), TypeTax, locAtlanta)
	require.NoError(t, err)
	assert.Equal(t, 2, catalog.callCount("list:TAX"), "the cache must be dropped after a write")
}

func TestCreateAndUpdateSingleObject(t *testing.T) {
	srv := newWriteServer(t, `{"catalog_object":{"type":"TAX","id":"TAX_NEW","version":1700000000123}}`)
	p := configuredProvider(t, srv.URL)

	id, err := p.Create(context.Background(), TypeTax, &provider.Resource{
		Name:        "ga_tax",
		Properties:  map[string]interface{}{"name": "GA Tax", "percentage": "4.5"},
		LocationIDs: []string{"LOC_ATL"},
	})
	require.NoError(t, err)
	assert.Equal(t, "TAX_NEW", id)
	assert.Equal(t, "/catalog/object", srv.paths[0])

	err = p.Update(context.Background(), TypeTax, "TAX_NEW", &provider.Resource{
		Name:        "ga_tax",
		Properties:  map[string]interface{}{"percentage": "5.0"},
		LocationIDs: []string{"LOC_ATL"},
		Version:     "1700000000123",
	})
	require.NoError(t, err)

	object := srv.requests[1]["object"].(map[string]interface{})
	assert.Equal(t, "TAX_NEW", object["id"])
	assert.Equal(t, float64(1700000000123), object["version"])
}

func TestDeleteRequiresExplicitCall(t *testing.T) {
	srv := newWriteServer(t, `{"deleted_object_ids":["TAX_1"]}`)
	p := configuredProvider(t, srv.URL)

	require.NoError(t, p.Delete(context.Background(), TypeTax, "TAX_1"))
	assert.Equal(t, "/catalog/object/TAX_1", srv.paths[0])

	err := p.Delete(context.Background(), "square_catalog_pizza", "X")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not support resource type")
}

func TestSlugForID(t *testing.T) {
	assert.Equal(t, "regular", slugForID("Regular"))
	assert.Equal(t, "extra_large", slugForID("Extra Large"))
	assert.Equal(t, "unnamed", slugForID("!!!"))
	assert.Equal(t, "unnamed", slugForID(""))
}

func TestWriteRequiresConfiguredProvider(t *testing.T) {
	p := &SquareProvider{}

	_, err := p.ApplyBatch(context.Background(), []provider.WriteOperation{
		writeOp(provider.WriteCreate, TypeTax, "t", nil, nil),
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")

	require.Error(t, p.Delete(context.Background(), TypeTax, "X"))
}
