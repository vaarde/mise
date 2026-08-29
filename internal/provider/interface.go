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
	// LocationID is the location this resource was read at. It is a
	// read-side field: ReadAll fills it in so the engine knows where a
	// resource was seen.
	LocationID string `json:"location_id"`

	// LocationIDs is the complete set of locations the resource should
	// apply at. It is the write-side counterpart of LocationID: Square
	// creates one catalog object carrying its own location list, not one
	// object per location, so Create and Update need the whole set.
	LocationIDs []string `json:"location_ids,omitempty"`

	Version string `json:"version"` // Provider version token (for optimistic concurrency)
}

// BatchApplier is an optional capability. An adapter implements it when
// the platform can write many resources in one API call — Square's
// catalog batch-upsert takes up to 10,000 objects, so applying a menu
// change across a chain is one request rather than hundreds.
//
// The engine uses this when the adapter provides it and falls back to
// Create and Update otherwise, so a simpler adapter stays correct
// without implementing it.
type BatchApplier interface {
	// ApplyBatch writes a set of resources. Every operation in one batch
	// is independent: the engine only groups resources whose
	// dependencies are already satisfied.
	//
	// It returns one outcome per operation, in the same order. A batch
	// that partially succeeds reports per-operation errors rather than
	// failing wholesale, so apply can record what did land.
	ApplyBatch(ctx context.Context, ops []WriteOperation) ([]WriteOutcome, error)
}

// WriteAction is what a WriteOperation does.
type WriteAction string

const (
	WriteCreate WriteAction = "create"
	WriteUpdate WriteAction = "update"
)

// WriteOperation is one resource write.
type WriteOperation struct {
	Action WriteAction

	// Name is the config name ("type.name"), used for idempotency keys
	// and for naming the resource in errors.
	Name string

	// Resource carries the desired properties, the location set, the
	// provider ID (empty on create), and the version token to pass back
	// for optimistic concurrency on update.
	Resource *Resource
}

// WriteOutcome is the result of one WriteOperation.
type WriteOutcome struct {
	Name string

	// ProviderID is the resource's ID after the write. On a create this
	// is the newly assigned one.
	ProviderID string

	// Version is the new concurrency token, when the platform returns one.
	Version string

	// Err is non-nil when this particular operation failed, even if
	// others in the same batch succeeded.
	Err error
}
