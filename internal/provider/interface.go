// Package provider defines the interface that every POS platform
// adapter must implement. The core engine only talks to this
// interface — it never calls Square, Toast, or any other POS API
// directly.
//
// Think of this like an electrical outlet standard: the engine is
// any appliance, and the provider is the adapter plug that fits
// a specific country's outlet. Same appliance, different plugs.
package provider

import "context"

// Provider is the contract every POS adapter must implement.
// The core engine uses this interface exclusively — it never
// calls platform APIs directly.
type Provider interface {
	// Name returns the provider identifier (e.g. "square", "toast").
	Name() string

	// Configure initializes the provider with credentials and settings
	// parsed from mise.yaml's provider block.
	Configure(cfg ProviderConfig) error

	// ListLocations returns all locations/restaurants in the account.
	// This is the first call Mise makes — it discovers where to look.
	ListLocations(ctx context.Context) ([]Location, error)

	// ResourceTypes returns the resource types this provider supports
	// (e.g. ["square_catalog_item", "square_catalog_tax", ...]).
	ResourceTypes() []string

	// Read fetches a single resource by its provider-assigned ID.
	Read(ctx context.Context, resourceType string, id string, locationID string) (*Resource, error)

	// ReadAll fetches every resource of a given type at a location.
	// This is the workhorse for fetch and drift operations.
	ReadAll(ctx context.Context, resourceType string, locationID string) ([]*Resource, error)

	// Create creates a new resource on the POS and returns its
	// provider-assigned ID. The engine calls this during apply
	// for resources that exist in config but not in the live POS.
	Create(ctx context.Context, resourceType string, desired *Resource, locationID string) (string, error)

	// Update modifies an existing resource on the POS. The engine
	// calls this during apply for resources whose properties have
	// changed between the declared config and the live state.
	Update(ctx context.Context, resourceType string, id string, desired *Resource, locationID string) error

	// Delete removes a resource from the POS. Only called when
	// the operator explicitly uses --destroy. Mise's safety-first
	// default is to never delete resources automatically.
	Delete(ctx context.Context, resourceType string, id string, locationID string) error
}

// ProviderConfig holds the provider block from mise.yaml.
type ProviderConfig struct {
	Platform    string            `yaml:"platform"`
	Environment string            `yaml:"environment"` // "production" or "sandbox"
	Credentials CredentialsConfig `yaml:"credentials"`
}

// CredentialsConfig holds authentication settings.
// Actual secrets are stored in .mise/credentials, never in mise.yaml.
type CredentialsConfig struct {
	Method      string `yaml:"method"` // "oauth2" or "access_token"
	AccessToken string `yaml:"-"`      // loaded from .mise/credentials at runtime, never serialized
}

// Location represents a physical restaurant location on the POS platform.
type Location struct {
	ID       string            `json:"id"`       // Provider-assigned ID (Square location ID, Toast GUID)
	Name     string            `json:"name"`     // Human-readable name ("Atlanta - Peachtree St")
	Address  string            `json:"address"`  // Full address string
	State    string            `json:"state"`    // US state code (for location group filtering)
	Timezone string            `json:"timezone"` // IANA timezone
	Metadata map[string]string `json:"metadata"` // Provider-specific fields
}

// Ref marks a property value that points at another resource by its
// provider-assigned ID.
//
// Adapters emit these instead of raw IDs because only the adapter knows
// which fields are references (Square's category_id, tax_ids, ...) while
// only the engine knows what config name each resource ended up with.
// The engine rewrites every Ref into "ref(type.name)" once naming is
// settled, which is also how it discovers the dependency graph.
type Ref struct {
	ResourceType string // e.g. "square_catalog_category"
	ProviderID   string // the referenced resource's provider-assigned ID
}

// Resource represents a single configurable entity on the POS
// (a tax rate, a menu item, a discount, etc.).
type Resource struct {
	Type       string                 `json:"type"`        // e.g. "square_catalog_tax"
	Name       string                 `json:"name"`        // User-defined name from config file
	ProviderID string                 `json:"provider_id"` // Provider-assigned ID (from API)
	Properties map[string]interface{} `json:"properties"`  // Resource-specific properties
	LocationID string                 `json:"location_id"` // Which location this resource belongs to
	Version    string                 `json:"version"`     // Provider version token (for optimistic concurrency)
}
