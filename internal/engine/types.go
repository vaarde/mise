// Package engine implements the core plan/apply/drift logic.
// It computes diffs between declared config and live POS state,
// resolves dependencies, and orchestrates changes.
package engine

// Action represents what Mise will do with a resource.
type Action int

const (
	ActionNoop   Action = iota // No change needed
	ActionCreate               // Resource exists in config but not on POS
	ActionUpdate               // Resource exists on both but properties differ
	ActionDelete               // Resource marked for explicit destruction
)

// String returns a human-readable label for the action.
func (a Action) String() string {
	switch a {
	case ActionNoop:
		return "no-op"
	case ActionCreate:
		return "create"
	case ActionUpdate:
		return "update"
	case ActionDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// Symbol returns the single-character prefix used in plan output:
// + for create, ~ for update, - for delete.
func (a Action) Symbol() string {
	switch a {
	case ActionCreate:
		return "+"
	case ActionUpdate:
		return "~"
	case ActionDelete:
		return "-"
	default:
		return " "
	}
}

// Plan represents the complete set of changes Mise will make.
type Plan struct {
	Changes  []ResourceChange `json:"changes"`
	Summary  PlanSummary      `json:"summary"`
}

// ResourceChange describes a single resource that will be created,
// updated, or deleted, along with per-property diffs.
type ResourceChange struct {
	Action       Action            `json:"action"`
	ResourceType string            `json:"resource_type"` // e.g. "square_catalog_tax"
	ResourceName string            `json:"resource_name"` // e.g. "ga_state_sales_tax"
	ProviderID   string            `json:"provider_id"`   // existing ID (empty for creates)
	LocationIDs  []string          `json:"location_ids"`  // affected locations
	Diffs        []PropertyDiff    `json:"diffs"`         // property-level changes
}

// FullName returns "type.name" for display.
func (rc ResourceChange) FullName() string {
	return rc.ResourceType + "." + rc.ResourceName
}

// PropertyDiff describes a single property change within a resource.
type PropertyDiff struct {
	Path     string      `json:"path"`      // property name (e.g. "percentage")
	OldValue interface{} `json:"old_value"` // current value (nil for creates)
	NewValue interface{} `json:"new_value"` // desired value (nil for deletes)
}

// PlanSummary counts the changes by action type.
type PlanSummary struct {
	ToCreate int `json:"to_create"`
	ToUpdate int `json:"to_update"`
	ToDelete int `json:"to_delete"`
}

// HasChanges returns true if the plan contains any actions.
func (p *Plan) HasChanges() bool {
	return p.Summary.ToCreate > 0 || p.Summary.ToUpdate > 0 || p.Summary.ToDelete > 0
}
