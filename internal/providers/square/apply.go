package square

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/vaarde/mise/internal/provider"
)

// maxBatchObjects is Square's documented ceiling for one batch-upsert.
const maxBatchObjects = 10000

// batchUpsertRequest is the POST /v2/catalog/batch-upsert body.
type batchUpsertRequest struct {
	IdempotencyKey string         `json:"idempotency_key"`
	Batches        []catalogBatch `json:"batches"`
}

type catalogBatch struct {
	Objects []map[string]interface{} `json:"objects"`
}

// batchUpsertResponse is what Square returns from a batch-upsert.
type batchUpsertResponse struct {
	Objects    []catalogObject `json:"objects"`
	IDMappings []struct {
		ClientObjectID string `json:"client_object_id"`
		ObjectID       string `json:"object_id"`
	} `json:"id_mappings"`
}

// upsertObjectRequest is the POST /v2/catalog/object body.
type upsertObjectRequest struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Object         map[string]interface{} `json:"object"`
}

type upsertObjectResponse struct {
	CatalogObject catalogObject `json:"catalog_object"`
}

// ApplyBatch writes a set of resources in a single batch-upsert.
//
// Square's catalog is account-wide, so each resource is one object
// carrying its own location list — not one object per location. A menu
// change across forty locations is therefore one request.
func (p *SquareProvider) ApplyBatch(ctx context.Context, ops []provider.WriteOperation) ([]provider.WriteOutcome, error) {
	if p.client == nil {
		return nil, fmt.Errorf("square provider is not configured")
	}
	if len(ops) == 0 {
		return nil, nil
	}
	if len(ops) > maxBatchObjects {
		return nil, fmt.Errorf("batch of %d objects exceeds Square's limit of %d", len(ops), maxBatchObjects)
	}

	outcomes := make([]provider.WriteOutcome, len(ops))
	objects := make([]map[string]interface{}, 0, len(ops))

	// clientIDs maps the temporary "#name" Square echoes back onto the
	// operation that produced it.
	clientIDs := make(map[string]int, len(ops))

	for i, op := range ops {
		outcomes[i] = provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}

		object, clientID, err := catalogObjectFor(op)
		if err != nil {
			outcomes[i].Err = err
			continue
		}
		if clientID != "" {
			clientIDs[clientID] = i
		}
		objects = append(objects, object)
	}

	if len(objects) == 0 {
		return outcomes, nil
	}

	request := batchUpsertRequest{
		// A deterministic key means a retried apply after a network
		// timeout does not create a second copy of everything.
		IdempotencyKey: idempotencyKey(ops),
		Batches:        []catalogBatch{{Objects: objects}},
	}

	var response batchUpsertResponse
	if err := p.client.Post(ctx, "/catalog/batch-upsert", request, &response); err != nil {
		return nil, err
	}

	applyBatchResponse(outcomes, clientIDs, response)
	p.invalidateCatalogCache()

	return outcomes, nil
}

// applyBatchResponse folds Square's response back into the outcomes.
func applyBatchResponse(
	outcomes []provider.WriteOutcome,
	clientIDs map[string]int,
	response batchUpsertResponse,
) {
	// Newly created objects come back with their real IDs, keyed by the
	// temporary ID Mise sent.
	for _, mapping := range response.IDMappings {
		if index, ok := clientIDs[mapping.ClientObjectID]; ok {
			outcomes[index].ProviderID = mapping.ObjectID
		}
	}

	// Version tokens are needed for the next update's concurrency check.
	byID := make(map[string]catalogObject, len(response.Objects))
	for _, object := range response.Objects {
		byID[object.ID] = object
	}

	for i := range outcomes {
		if object, ok := byID[outcomes[i].ProviderID]; ok {
			outcomes[i].Version = strconv.FormatInt(object.Version, 10)
		}
	}
}

// Create writes a single new resource and returns its Square ID.
func (p *SquareProvider) Create(ctx context.Context, resourceType string, desired *provider.Resource, locationID string) (string, error) {
	outcome, err := p.upsertOne(ctx, provider.WriteOperation{
		Action:   provider.WriteCreate,
		Name:     resourceType + "." + desired.Name,
		Resource: withType(desired, resourceType),
	})
	if err != nil {
		return "", err
	}
	return outcome.ProviderID, outcome.Err
}

// Update writes changes to an existing resource.
func (p *SquareProvider) Update(ctx context.Context, resourceType string, id string, desired *provider.Resource, locationID string) error {
	resource := withType(desired, resourceType)
	resource.ProviderID = id

	outcome, err := p.upsertOne(ctx, provider.WriteOperation{
		Action:   provider.WriteUpdate,
		Name:     resourceType + "." + desired.Name,
		Resource: resource,
	})
	if err != nil {
		return err
	}
	return outcome.Err
}

// upsertOne writes one object via POST /v2/catalog/object.
func (p *SquareProvider) upsertOne(ctx context.Context, op provider.WriteOperation) (provider.WriteOutcome, error) {
	outcome := provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}

	if p.client == nil {
		return outcome, fmt.Errorf("square provider is not configured")
	}

	object, _, err := catalogObjectFor(op)
	if err != nil {
		return outcome, err
	}

	var response upsertObjectResponse
	request := upsertObjectRequest{
		IdempotencyKey: idempotencyKey([]provider.WriteOperation{op}),
		Object:         object,
	}
	if err := p.client.Post(ctx, "/catalog/object", request, &response); err != nil {
		return outcome, err
	}

	outcome.ProviderID = response.CatalogObject.ID
	outcome.Version = strconv.FormatInt(response.CatalogObject.Version, 10)
	p.invalidateCatalogCache()

	return outcome, nil
}

// Delete removes a resource. Mise only calls this when the operator
// explicitly passes --destroy.
func (p *SquareProvider) Delete(ctx context.Context, resourceType string, id string, locationID string) error {
	if p.client == nil {
		return fmt.Errorf("square provider is not configured")
	}
	if _, ok := catalogTypeFor[resourceType]; !ok {
		return unsupportedTypeError(resourceType)
	}

	if err := p.client.Delete(ctx, "/catalog/object/"+url.PathEscape(id), nil); err != nil {
		return err
	}
	p.invalidateCatalogCache()
	return nil
}

// invalidateCatalogCache drops the cached listing after a write, so a
// read later in the same run sees what was just written rather than the
// pre-apply snapshot.
func (p *SquareProvider) invalidateCatalogCache() {
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()
	p.catalogCache = nil
}

// catalogObjectFor builds the Square catalog object for a write.
//
// It returns the object and, for a create, the temporary "#name" ID
// Square uses to report back the real ID it assigned.
func catalogObjectFor(op provider.WriteOperation) (map[string]interface{}, string, error) {
	squareType, ok := catalogTypeFor[op.Resource.Type]
	if !ok {
		return nil, "", unsupportedTypeError(op.Resource.Type)
	}

	object := map[string]interface{}{"type": squareType}

	clientID := ""
	if op.Action == provider.WriteCreate {
		// Square assigns real IDs; a new object is named with a "#"
		// placeholder that comes back in the response's id_mappings.
		clientID = "#" + op.Resource.Name
		object["id"] = clientID
	} else {
		if op.Resource.ProviderID == "" {
			return nil, "", fmt.Errorf("%s: cannot update without a provider ID", op.Name)
		}
		object["id"] = op.Resource.ProviderID

		// Passing the version back is what makes the update fail rather
		// than silently overwrite a change made since the last read.
		if version := op.Resource.Version; version != "" {
			parsed, err := strconv.ParseInt(version, 10, 64)
			if err != nil {
				return nil, "", fmt.Errorf("%s: cannot read stored version %q: %w", op.Name, version, err)
			}
			object["version"] = parsed
		}
	}

	applyLocationScope(object, op.Resource.LocationIDs)

	data, err := typeDataFor(op.Resource.Type, op.Resource.Properties)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", op.Name, err)
	}
	object[strings.ToLower(squareType)+"_data"] = data

	return object, clientID, nil
}

// applyLocationScope sets the location fields Square uses for scoping.
func applyLocationScope(object map[string]interface{}, locationIDs []string) {
	if len(locationIDs) == 0 {
		object["present_at_all_locations"] = true
		return
	}

	sorted := append([]string(nil), locationIDs...)
	sort.Strings(sorted)

	object["present_at_all_locations"] = false
	object["present_at_location_ids"] = sorted
}

// typeDataFor renders Mise properties into Square's "<type>_data" shape.
func typeDataFor(resourceType string, properties map[string]interface{}) (map[string]interface{}, error) {
	switch resourceType {
	case TypeTax:
		return passThrough(properties, "name", "calculation_phase", "inclusion_type",
			"percentage", "applies_to_custom_amounts", "enabled"), nil

	case TypeCategory:
		return passThrough(properties, "name"), nil

	case TypeDiscount:
		return passThrough(properties, "name", "discount_type", "percentage",
			"amount_money", "pin_required", "label_color"), nil

	case TypeModifierList:
		data := passThrough(properties, "name", "selection_type")
		if modifiers, ok := properties["modifiers"].([]interface{}); ok {
			data["modifiers"] = buildModifiers(modifiers)
		}
		return data, nil

	case TypeItem:
		return buildItemData(properties)

	default:
		return nil, unsupportedTypeError(resourceType)
	}
}

// buildItemData assembles item_data, including the nested variations and
// the reference fields that arrive as provider IDs.
func buildItemData(properties map[string]interface{}) (map[string]interface{}, error) {
	data := passThrough(properties, "name", "description", "abbreviation", "product_type")

	if category, ok := properties["category"].(string); ok && category != "" {
		data["category_id"] = category
	}

	if taxIDs := stringsFrom(properties["tax_ids"]); len(taxIDs) > 0 {
		data["tax_ids"] = taxIDs
	}

	if lists := stringsFrom(properties["modifier_lists"]); len(lists) > 0 {
		info := make([]map[string]interface{}, 0, len(lists))
		for _, id := range lists {
			info = append(info, map[string]interface{}{"modifier_list_id": id, "enabled": true})
		}
		data["modifier_list_info"] = info
	}

	if variations, ok := properties["variations"].([]interface{}); ok {
		built, err := buildVariations(variations)
		if err != nil {
			return nil, err
		}
		data["variations"] = built
	}

	return data, nil
}

// buildVariations renders an item's variations as ITEM_VARIATION objects.
func buildVariations(variations []interface{}) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(variations))

	for i, raw := range variations {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("variation %d is not a mapping", i)
		}

		name, _ := entry["name"].(string)
		if name == "" {
			return nil, fmt.Errorf("variation %d has no name", i)
		}

		data := passThrough(entry, "name", "sku", "pricing_type", "price_money")
		if _, ok := data["pricing_type"]; !ok {
			data["pricing_type"] = "FIXED_PRICING"
		}

		out = append(out, map[string]interface{}{
			"type":                     "ITEM_VARIATION",
			"id":                       "#variation_" + slugForID(name) + "_" + strconv.Itoa(i),
			"present_at_all_locations": true,
			"item_variation_data":      data,
		})
	}

	return out, nil
}

// buildModifiers renders a modifier list's modifiers.
func buildModifiers(modifiers []interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(modifiers))

	for i, raw := range modifiers {
		entry, ok := raw.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := entry["name"].(string)

		out = append(out, map[string]interface{}{
			"type":                     "MODIFIER",
			"id":                       "#modifier_" + slugForID(name) + "_" + strconv.Itoa(i),
			"present_at_all_locations": true,
			"modifier_data":            passThrough(entry, "name", "price_money"),
		})
	}

	return out
}

// passThrough copies the named keys when present.
func passThrough(properties map[string]interface{}, keys ...string) map[string]interface{} {
	out := make(map[string]interface{}, len(keys))
	for _, key := range keys {
		if value, ok := properties[key]; ok && value != nil {
			out[key] = value
		}
	}
	return out
}

// stringsFrom reads a property that holds a list of strings.
func stringsFrom(value interface{}) []string {
	switch typed := value.(type) {
	case []string:
		return typed
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if str, ok := item.(string); ok && str != "" {
				out = append(out, str)
			}
		}
		return out
	default:
		return nil
	}
}

// slugForID makes a name safe to embed in a temporary Square object ID.
func slugForID(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		return "unnamed"
	}
	return slug
}

// idempotencyKey derives a stable key from what is being written.
//
// Square requires a key on every catalog mutation. Deriving it from the
// operations themselves means a retry after a timeout replays the same
// key and Square returns the original result, rather than creating a
// duplicate set of objects.
func idempotencyKey(ops []provider.WriteOperation) string {
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		properties, err := json.Marshal(op.Resource.Properties)
		if err != nil {
			properties = []byte(fmt.Sprintf("%v", op.Resource.Properties))
		}
		locations := append([]string(nil), op.Resource.LocationIDs...)
		sort.Strings(locations)

		parts = append(parts, strings.Join([]string{
			string(op.Action),
			op.Name,
			op.Resource.ProviderID,
			strings.Join(locations, ","),
			string(properties),
		}, "\x00"))
	}
	sort.Strings(parts)

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x01")))
	return "mise-" + hex.EncodeToString(sum[:16])
}
