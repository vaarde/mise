// Package state manages Mise's state file (.mise/state.json).
//
// The state file is the bridge between your YAML config files and
// the live POS. It records which provider-assigned IDs map to which
// resource names, and what the last-known property values were.
//
// Think of it like a contacts app that maps names to phone numbers:
// your config file says "ga_state_sales_tax should be 4.5%", the
// state file says "ga_state_sales_tax is Square ID ZMBC7XT...",
// and drift detection checks whether ZMBC7XT... is actually 4.5%.
package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	// StateDir is the directory where Mise stores state and credentials.
	StateDir = ".mise"

	// StateFileName is the state file name within StateDir.
	StateFileName = "state.json"

	// LockFileName is used to prevent concurrent applies.
	LockFileName = "lock"
)

// State represents the complete state file (.mise/state.json).
type State struct {
	Version   int                        `json:"version"`
	Provider  string                     `json:"provider"`
	LastFetch *time.Time                 `json:"last_fetch,omitempty"`
	LastApply *time.Time                 `json:"last_apply,omitempty"`
	Resources map[string]*ResourceState  `json:"resources"`  // key: "type.name"
	Locations map[string]*LocationState  `json:"locations"`  // key: provider location ID
}

// ResourceState tracks a single resource's relationship to the POS.
type ResourceState struct {
	ProviderID string                 `json:"provider_id"`
	Type       string                 `json:"type"`
	Name       string                 `json:"name"`
	Locations  []string               `json:"locations"`
	Properties map[string]interface{} `json:"properties"`
	Version    string                 `json:"version,omitempty"` // optimistic concurrency token
	LastSynced time.Time              `json:"last_synced"`
}

// LocationState tracks metadata about a known location.
type LocationState struct {
	Name     string `json:"name"`
	State    string `json:"state"`
	Timezone string `json:"timezone"`
}

// New creates a fresh, empty state.
func New(providerName string) *State {
	return &State{
		Version:   1,
		Provider:  providerName,
		Resources: make(map[string]*ResourceState),
		Locations: make(map[string]*LocationState),
	}
}

// Load reads the state file from disk. Returns a fresh state
// if the file doesn't exist yet (first run).
func Load(workDir string) (*State, error) {
	path := filepath.Join(workDir, StateDir, StateFileName)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run — no state file yet. Return empty state.
		return New(""), nil
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read state file: %w", err)
	}

	var s State
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("cannot parse state file: %w", err)
	}

	// Ensure maps are initialized (they'll be nil if empty in JSON)
	if s.Resources == nil {
		s.Resources = make(map[string]*ResourceState)
	}
	if s.Locations == nil {
		s.Locations = make(map[string]*LocationState)
	}

	return &s, nil
}

// Save writes the state file to disk. Creates the .mise/ directory
// if it doesn't exist.
func (s *State) Save(workDir string) error {
	dir := filepath.Join(workDir, StateDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create state directory: %w", err)
	}

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize state: %w", err)
	}

	path := filepath.Join(dir, StateFileName)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("cannot write state file: %w", err)
	}

	return nil
}

// ResourceKey returns the state map key for a resource: "type.name"
func ResourceKey(resourceType, name string) string {
	return resourceType + "." + name
}
