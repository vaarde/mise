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

const maxBatchObjects = 10000

type batchUpsertRequest struct {
	IdempotencyKey string         `json:"idempotency_key"`
	Batches        []catalogBatch `json:"batches"`
}

type catalogBatch struct {
	Objects []map[string]interface{} `json:"objects"`
}

type batchUpsertResponse struct {
	Objects    []catalogObject `json:"objects"`
	IDMappings []struct {
		ClientObjectID string `json:"client_object_id"`
		ObjectID       string `json:"object_id"`
	} `json:"id_mappings"`
}

type upsertObjectRequest struct {
	IdempotencyKey string                 `json:"idempotency_key"`
	Object         map[string]interface{} `json:"object"`
}

type upsertObjectResponse struct {
	CatalogObject catalogObject `json:"catalog_object"`
}

func (p *SquareProvider) ApplyBatch(ctx context.Context, ops []provider.WriteOperation) ([]provider.WriteOutcome, error) {
	if p.client == nil { return nil, fmt.Errorf("square provider is not configured") }
	if len(ops) == 0 { return nil, nil }
	if len(ops) > maxBatchObjects { return nil, fmt.Errorf("batch of %d objects exceeds Square's limit of %d", len(ops), maxBatchObjects) }
	if err := p.checkLocationScope(ctx, ops); err != nil { return nil, err }

	outcomes := make([]provider.WriteOutcome, len(ops))
	objects := make([]map[string]interface{}, 0, len(ops))
	clientIDs := make(map[string]int, len(ops))
	for i, op := range ops {
		outcomes[i] = provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}
		object, clientID, err := catalogObjectFor(op)
		if err != nil { outcomes[i].Err = err; continue }
		if clientID != "" { clientIDs[clientID] = i }
		objects = append(objects, object)
	}
	if len(objects) == 0 { return outcomes, nil }

	request := batchUpsertRequest{IdempotencyKey: idempotencyKey(ops), Batches: []catalogBatch{{Objects: objects}}}
	var response batchUpsertResponse
	if err := p.client.Post(ctx, "/catalog/batch-upsert", request, &response); err != nil { return nil, err }
	applyBatchResponse(outcomes, clientIDs, response)
	p.invalidateCatalogCache()
	return outcomes, nil
}

func applyBatchResponse(outcomes []provider.WriteOutcome, clientIDs map[string]int, response batchUpsertResponse) {
	for _, mapping := range response.IDMappings {
		if index, ok := clientIDs[mapping.ClientObjectID]; ok { outcomes[index].ProviderID = mapping.ObjectID }
	}
	byID := make(map[string]catalogObject, len(response.Objects))
	for _, object := range response.Objects { byID[object.ID] = object }
	for i := range outcomes {
		if outcomes[i].Err != nil { continue }
		object, ok := byID[outcomes[i].ProviderID]
		if !ok {
			outcomes[i].Err = fmt.Errorf("Square batch response did not confirm %s", outcomes[i].Name)
			continue
		}
		outcomes[i].Version = strconv.FormatInt(object.Version, 10)
	}
}

func (p *SquareProvider) Create(ctx context.Context, resourceType string, desired *provider.Resource) (string, error) {
	outcome, err := p.upsertOne(ctx, provider.WriteOperation{Action: provider.WriteCreate, Name: resourceType + "." + desired.Name, Resource: withType(desired, resourceType)})
	if err != nil { return "", err }
	return outcome.ProviderID, outcome.Err
}

func (p *SquareProvider) Update(ctx context.Context, resourceType string, id string, desired *provider.Resource) error {
	resource := withType(desired, resourceType); resource.ProviderID = id
	outcome, err := p.upsertOne(ctx, provider.WriteOperation{Action: provider.WriteUpdate, Name: resourceType + "." + desired.Name, Resource: resource})
	if err != nil { return err }
	return outcome.Err
}

func (p *SquareProvider) upsertOne(ctx context.Context, op provider.WriteOperation) (provider.WriteOutcome, error) {
	outcome := provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}
	if p.client == nil { return outcome, fmt.Errorf("square provider is not configured") }
	object, _, err := catalogObjectFor(op)
	if err != nil { return outcome, err }
	var response upsertObjectResponse
	request := upsertObjectRequest{IdempotencyKey: idempotencyKey([]provider.WriteOperation{op}), Object: object}
	if err := p.client.Post(ctx, "/catalog/object", request, &response); err != nil { return outcome, err }
	outcome.ProviderID = response.CatalogObject.ID
	outcome.Version = strconv.FormatInt(response.CatalogObject.Version, 10)
	p.invalidateCatalogCache()
	return outcome, nil
}

func (p *SquareProvider) Delete(ctx context.Context, resourceType string, id string) error {
	if p.client == nil { return fmt.Errorf("square provider is not configured") }
	if _, ok := catalogTypeFor[resourceType]; !ok { return unsupportedTypeError(resourceType) }
	if err := p.client.Delete(ctx, "/catalog/object/"+url.PathEscape(id), nil); err != nil { return err }
	p.invalidateCatalogCache(); return nil
}

func (p *SquareProvider) invalidateCatalogCache() { p.catalogMu.Lock(); defer p.catalogMu.Unlock(); p.catalogCache = nil }

func catalogObjectFor(op provider.WriteOperation) (map[string]interface{}, string, error) {
	squareType, ok := catalogTypeFor[op.Resource.Type]
	if !ok { return nil, "", unsupportedTypeError(op.Resource.Type) }
	object := map[string]interface{}{"type": squareType}
	clientID := ""
	if op.Action == provider.WriteCreate {
		clientID = "#" + op.Resource.Name; object["id"] = clientID
	} else {
		if op.Resource.ProviderID == "" { return nil, "", fmt.Errorf("%s: cannot update without a provider ID", op.Name) }
		object["id"] = op.Resource.ProviderID
		if version := op.Resource.Version; version != "" {
			parsed, err := strconv.ParseInt(version, 10, 64)
			if err != nil { return nil, "", fmt.Errorf("%s: cannot read stored version %q: %w", op.Name, version, err) }
			object["version"] = parsed
		}
	}
	if len(op.Resource.LocationIDs) == 0 { return nil, "", fmt.Errorf("%s: no locations to write to — Square would read that as every location. Name the locations, or use ${group.all} if that is what you meant", op.Name) }
	applyLocationScope(object, op.Resource.Type, op.Resource.LocationIDs)
	data, err := typeDataFor(op.Resource.Type, op.Resource.Properties, op.Resource.LocationIDs)
	if err != nil { return nil, "", fmt.Errorf("%s: %w", op.Name, err) }
	object[strings.ToLower(squareType)+"_data"] = data
	return object, clientID, nil
}

var accountWideOnlyTypes = map[string]bool{TypeCategory: true}

func applyLocationScope(object map[string]interface{}, resourceType string, locationIDs []string) {
	if accountWideOnlyTypes[resourceType] { object["present_at_all_locations"] = true; return }
	sorted := append([]string(nil), locationIDs...); sort.Strings(sorted)
	object["present_at_all_locations"] = false; object["present_at_location_ids"] = sorted
}

func (p *SquareProvider) checkLocationScope(ctx context.Context, ops []provider.WriteOperation) error {
	var scoped []provider.WriteOperation
	for _, op := range ops { if accountWideOnlyTypes[op.Resource.Type] && len(op.Resource.LocationIDs) > 0 { scoped = append(scoped, op) } }
	if len(scoped) == 0 { return nil }
	account, err := p.accountLocationIDs(ctx); if err != nil { return err }
	for _, op := range scoped {
		if len(op.Resource.LocationIDs) >= len(account) { continue }
		return fmt.Errorf("%s: Square applies categories to every location, so %s cannot be scoped to %d of %d — use ${group.all}", op.Name, op.Resource.Type, len(op.Resource.LocationIDs), len(account))
	}
	return nil
}

func (p *SquareProvider) accountLocationIDs(ctx context.Context) ([]string, error) {
	p.locationMu.Lock(); defer p.locationMu.Unlock()
	if p.locationCache != nil { return p.locationCache, nil }
	locations, err := p.client.ListLocations(ctx); if err != nil { return nil, err }
	ids := make([]string, 0, len(locations)); for _, l := range locations { ids = append(ids, l.ID) }; p.locationCache = ids; return ids, nil
}

func typeDataFor(resourceType string, properties map[string]interface{}, locationIDs []string) (map[string]interface{}, error) {
	switch resourceType {
	case TypeTax:
		return passThrough(properties, "name", "calculation_phase", "inclusion_type", "percentage", "applies_to_custom_amounts", "enabled"), nil
	case TypeCategory:
		return passThrough(properties, "name"), nil
	case TypeDiscount:
		return passThrough(properties, "name", "discount_type", "percentage", "amount_money", "pin_required", "label_color"), nil
	case TypeModifierList:
		data := passThrough(properties, "name", "selection_type"); if modifiers, ok := properties["modifiers"].([]interface{}); ok { data["modifiers"] = buildModifiers(modifiers, resourceType, locationIDs) }; return data, nil
	case TypeItem:
		return buildItemData(properties, resourceType, locationIDs)
	default:
		return nil, unsupportedTypeError(resourceType)
	}
}

func buildItemData(properties map[string]interface{}, resourceType string, locationIDs []string) (map[string]interface{}, error) {
	data := passThrough(properties, "name", "description", "abbreviation", "product_type")
	if category, ok := properties["category"].(string); ok && category != "" { data["categories"] = []map[string]interface{}{{"id": category, "ordinal": 0}}; data["reporting_category"] = map[string]interface{}{"id": category, "ordinal": 0} }
	if taxIDs := stringsFrom(properties["tax_ids"]); len(taxIDs) > 0 { data["tax_ids"] = taxIDs }
	if lists := stringsFrom(properties["modifier_lists"]); len(lists) > 0 { info := make([]map[string]interface{}, 0, len(lists)); for _, id := range lists { info = append(info, map[string]interface{}{"modifier_list_id": id, "enabled": true}) }; data["modifier_list_info"] = info }
	if variations, ok := properties["variations"].([]interface{}); ok { built, err := buildVariations(variations, resourceType, locationIDs); if err != nil { return nil, err }; data["variations"] = built }
	return data, nil
}

func buildVariations(variations []interface{}, resourceType string, locationIDs []string) ([]map[string]interface{}, error) {
	out := make([]map[string]interface{}, 0, len(variations))
	for i, raw := range variations {
		entry, ok := raw.(map[string]interface{}); if !ok { return nil, fmt.Errorf("variation %d is not a mapping", i) }
		name, _ := entry["name"].(string); if name == "" { return nil, fmt.Errorf("variation %d has no name", i) }
		data := passThrough(entry, "name", "sku", "pricing_type", "price_money"); if _, ok := data["pricing_type"]; !ok { data["pricing_type"] = "FIXED_PRICING" }
		variation := map[string]interface{}{"type": "ITEM_VARIATION", "id": "#variation_" + slugForID(name) + "_" + strconv.Itoa(i), "item_variation_data": data}; applyLocationScope(variation, resourceType, locationIDs); out = append(out, variation)
	}
	return out, nil
}

func buildModifiers(modifiers []interface{}, resourceType string, locationIDs []string) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(modifiers))
	for i, raw := range modifiers { entry, ok := raw.(map[string]interface{}); if !ok { continue }; name, _ := entry["name"].(string); modifier := map[string]interface{}{"type": "MODIFIER", "id": "#modifier_" + slugForID(name) + "_" + strconv.Itoa(i), "modifier_data": passThrough(entry, "name", "price_money")}; applyLocationScope(modifier, resourceType, locationIDs); out = append(out, modifier) }
	return out
}

func passThrough(properties map[string]interface{}, keys ...string) map[string]interface{} { out := make(map[string]interface{}, len(keys)); for _, key := range keys { if value, ok := properties[key]; ok && value != nil { out[key] = value } }; return out }
func stringsFrom(value interface{}) []string { switch typed := value.(type) { case []string: return typed; case []interface{}: out := make([]string, 0, len(typed)); for _, item := range typed { if str, ok := item.(string); ok && str != "" { out = append(out, str) } }; return out; default: return nil } }
func slugForID(name string) string { var b strings.Builder; for _, r := range strings.ToLower(name) { if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') { b.WriteRune(r) } else { b.WriteByte('_') } }; slug := strings.Trim(b.String(), "_"); if slug == "" { return "unnamed" }; return slug }

func idempotencyKey(ops []provider.WriteOperation) string {
	parts := make([]string, 0, len(ops))
	for _, op := range ops {
		properties, err := json.Marshal(op.Resource.Properties); if err != nil { properties = []byte(fmt.Sprintf("%v", op.Resource.Properties)) }
		locations := append([]string(nil), op.Resource.LocationIDs...); sort.Strings(locations)
		parts = append(parts, strings.Join([]string{string(op.Action), op.Name, op.Resource.ProviderID, op.Resource.Version, strings.Join(locations, ","), string(properties)}, "\x00"))
	}
	sort.Strings(parts); sum := sha256.Sum256([]byte(strings.Join(parts, "\x01"))); return "mise-" + hex.EncodeToString(sum[:16])
}
