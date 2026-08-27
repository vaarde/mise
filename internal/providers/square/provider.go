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
	"square_location",
	"square_catalog_item",
	"square_catalog_category",
	"square_catalog_tax",
	"square_catalog_discount",
	"square_catalog_modifier_list",
}

// SquareProvider implements provider.Provider for Square POS.
type SquareProvider struct {
	client  *Client
	baseURL string
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
	// TODO: Milestone 2 — dispatch to type-specific read function
	return nil, fmt.Errorf("not yet implemented")
}

func (p *SquareProvider) ReadAll(ctx context.Context, resourceType string, locationID string) ([]*provider.Resource, error) {
	// TODO: Milestone 2 — dispatch to type-specific list function
	return nil, fmt.Errorf("not yet implemented")
}

func (p *SquareProvider) Create(ctx context.Context, resourceType string, desired *provider.Resource, locationID string) (string, error) {
	// TODO: Milestone 4 — dispatch to type-specific create function
	return "", fmt.Errorf("not yet implemented")
}

func (p *SquareProvider) Update(ctx context.Context, resourceType string, id string, desired *provider.Resource, locationID string) error {
	// TODO: Milestone 4 — dispatch to type-specific update function
	return fmt.Errorf("not yet implemented")
}

func (p *SquareProvider) Delete(ctx context.Context, resourceType string, id string, locationID string) error {
	// TODO: Milestone 4 — dispatch to type-specific delete function
	return fmt.Errorf("not yet implemented")
}
