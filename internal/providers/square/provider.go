// Package square implements the Mise provider interface for
// Square POS. It uses Square's Catalog API, Locations API,
// and related endpoints to manage restaurant configuration.
//
// Square's API is fully read-write out of the box — no partner
// program or special approval needed. This makes it the ideal
// first adapter for Mise.
package square

import (
	"context"
	"fmt"
	"sync"

	"github.com/vaarde/mise/internal/provider"
)

const (
	ProviderName = "square"

	// API base URLs
	ProductionBaseURL = ProductionHost + "/v2"
	SandboxBaseURL    = SandboxHost + "/v2"
)

// Supported resource types for the Square adapter.
var supportedResourceTypes = []string{
	TypeLocation,
	TypeItem,
	TypeCategory,
	TypeTax,
	TypeDiscount,
	TypeModifierList,
}

// SquareProvider implements provider.Provider for Square POS.
type SquareProvider struct {
	client  *Client
	baseURL string

	// Square's catalog is account-wide, not location-scoped: one tax
	// object carries the list of locations it applies at. A fetch across
	// 15 locations would otherwise download the whole catalog 15 times,
	// so each catalog type is listed once per provider instance and
	// filtered per location from memory. Commands build a fresh provider,
	// so the cache never outlives a single run.
	catalogMu    sync.Mutex
	catalogCache map[string][]catalogObject

	// locationCache backs the account-wide-scope check on write, so a
	// batch does not re-list locations per operation.
	locationMu    sync.Mutex
	locationCache []string
}

// Register the Square provider with the provider registry.
// This runs automatically when the package is imported.
func init() {
	provider.Register(ProviderName, func() provider.Provider {
		return &SquareProvider{}
	})
}

func (p *SquareProvider) Name() string {
	return ProviderName
}

func (p *SquareProvider) Configure(cfg provider.ProviderConfig) error {
	baseURL, err := BaseURLFor(cfg.Environment)
	if err != nil {
		return err
	}
	p.baseURL = baseURL

	if cfg.Credentials.AccessToken == "" {
		return fmt.Errorf("no Square access token available — run 'mise init' or set SQUARE_ACCESS_TOKEN")
	}

	// Initialize HTTP client with credentials
	p.client = NewClient(p.baseURL, cfg.Credentials.AccessToken)

	return nil
}

func (p *SquareProvider) ListLocations(ctx context.Context) ([]provider.Location, error) {
	if p.client == nil {
		return nil, fmt.Errorf("square provider is not configured")
	}
	return p.client.ListLocations(ctx)
}

func (p *SquareProvider) ResourceTypes() []string {
	return supportedResourceTypes
}

func (p *SquareProvider) Read(ctx context.Context, resourceType string, id string, locationID string) (*provider.Resource, error) {
	if p.client == nil {
		return nil, fmt.Errorf("square provider is not configured")
	}

	if resourceType == TypeLocation {
		locations, err := p.client.ListLocations(ctx)
		if err != nil {
			return nil, err
		}
		for _, l := range locations {
			if l.ID == id {
				return locationResource(l), nil
			}
		}
		return nil, fmt.Errorf("location %s not found", id)
	}

	if _, ok := catalogTypeFor[resourceType]; !ok {
		return nil, unsupportedTypeError(resourceType)
	}

	obj, err := p.client.GetCatalogObject(ctx, id)
	if err != nil {
		return nil, err
	}
	if obj.IsDeleted {
		return nil, fmt.Errorf("%s %s has been deleted in Square", resourceType, id)
	}
	if locationID != "" && !obj.presentAt(locationID) {
		return nil, fmt.Errorf("%s %s is not present at location %s", resourceType, id, locationID)
	}

	return obj.toResource(resourceType, locationID), nil
}

func (p *SquareProvider) ReadAll(ctx context.Context, resourceType string, locationID string) ([]*provider.Resource, error) {
	if p.client == nil {
		return nil, fmt.Errorf("square provider is not configured")
	}

	if resourceType == TypeLocation {
		locations, err := p.client.ListLocations(ctx)
		if err != nil {
			return nil, err
		}
		resources := make([]*provider.Resource, 0, len(locations))
		for _, l := range locations {
			if locationID != "" && l.ID != locationID {
				continue
			}
			resources = append(resources, locationResource(l))
		}
		return resources, nil
	}

	squareType, ok := catalogTypeFor[resourceType]
	if !ok {
		return nil, unsupportedTypeError(resourceType)
	}

	objects, err := p.catalogObjects(ctx, squareType)
	if err != nil {
		return nil, err
	}

	resources := make([]*provider.Resource, 0, len(objects))
	for _, obj := range objects {
		if !obj.presentAt(locationID) {
			continue
		}
		resources = append(resources, obj.toResource(resourceType, locationID))
	}
	return resources, nil
}

// catalogObjects returns every object of a Square catalog type, listing
// it from the API the first time and serving it from memory afterwards.
func (p *SquareProvider) catalogObjects(ctx context.Context, squareType string) ([]catalogObject, error) {
	p.catalogMu.Lock()
	defer p.catalogMu.Unlock()

	if objects, ok := p.catalogCache[squareType]; ok {
		return objects, nil
	}

	objects, err := p.client.ListCatalogObjects(ctx, squareType)
	if err != nil {
		return nil, err
	}

	if p.catalogCache == nil {
		p.catalogCache = map[string][]catalogObject{}
	}
	p.catalogCache[squareType] = objects
	return objects, nil
}

// withType returns a copy of a resource with its type set, so callers
// that pass the type separately still produce a complete resource.
func withType(resource *provider.Resource, resourceType string) *provider.Resource {
	clone := *resource
	clone.Type = resourceType
	return &clone
}

// locationResource renders a location as a Mise resource, so locations
// can be read through the same interface as catalog objects.
func locationResource(l provider.Location) *provider.Resource {
	props := map[string]interface{}{"name": l.Name}
	setIfNotEmpty(props, "address", l.Address)
	setIfNotEmpty(props, "state", l.State)
	setIfNotEmpty(props, "timezone", l.Timezone)
	for _, key := range []string{"status", "type", "country", "currency", "business_name"} {
		setIfNotEmpty(props, key, l.Metadata[key])
	}

	return &provider.Resource{
		Type:       TypeLocation,
		Name:       l.Name,
		ProviderID: l.ID,
		Properties: props,
		LocationID: l.ID,
	}
}

// unsupportedTypeError explains which types the adapter does handle.
func unsupportedTypeError(resourceType string) error {
	return fmt.Errorf("square provider does not support resource type %q — supported types: %v",
		resourceType, supportedResourceTypes)
}

// Create, Update, Delete, and ApplyBatch live in apply.go.
