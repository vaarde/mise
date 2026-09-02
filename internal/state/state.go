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
	Version  int    `json:"version"`
	Provider string `json:"provider"`

	// Environment and AccountID name the POS account these provider IDs
	// belong to. See Identity — provider IDs mean nothing outside the
	// account that issued them, so every command checks these before
	// trusting the rest of the file.
	Environment string `json:"environment,omitempty"`
	AccountID   string `json:"account_id,omitempty"`

	// Serial counts saves. A saved plan records the serial it was
	// computed against, so applying a stale plan can be caught rather
	// than executed against state that has moved on since.
	Serial int `json:"serial"`

	LastFetch *time.Time                `json:"last_fetch,omitempty"`
	LastApply *time.Time                `json:"last_apply,omitempty"`
	Resources map[string]*ResourceState `json:"resources"` // key: "type.name"
	Locations map[string]*LocationState `json:"locations"` // key: provider location ID
}

// Identity returns the account this state describes.
func (s *State) Identity() Identity {
	return Identity{
		Provider:    s.Provider,
		Environment: s.Environment,
		AccountID:   s.AccountID,
	}
}

// SetIdentity records which account the state describes.
func (s *State) SetIdentity(id Identity) {
	s.Provider = id.Provider
	s.Environment = id.Environment
	s.AccountID = id.AccountID
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
//
// The write is atomic: the new contents go to a temporary file in the
// same directory, are flushed to disk, and are then renamed over the
// old file. A crash or a pulled plug therefore leaves either the
// previous state or the new one, never a half-written file. That
// matters more here than in most places — a truncated state.json means
// Mise no longer knows which live resources it created, and the next
// apply would create them all again.
//
// Each save bumps Serial, which is how a saved plan can tell that state
// has moved since it was computed.
func (s *State) Save(workDir string) error {
	dir := filepath.Join(workDir, StateDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create state directory: %w", err)
	}

	s.Serial++

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		s.Serial--
		return fmt.Errorf("cannot serialize state: %w", err)
	}

	if err := writeFileAtomic(filepath.Join(dir, StateFileName), data, 0600); err != nil {
		s.Serial--
		return fmt.Errorf("cannot write state file: %w", err)
	}

	return nil
}

// writeFileAtomic writes data to path via a temporary file and a rename,
// so a reader never sees a partial write and a crash never destroys what
// was already there.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)

	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("cannot create temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	// Any failure from here on leaves the temporary file behind, so
	// clean it up on every path that does not rename it into place.
	defer func() {
		tmp.Close()
		os.Remove(tmpName)
	}()

	if err := tmp.Chmod(perm); err != nil {
		return fmt.Errorf("cannot set permissions on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("cannot write %s: %w", tmpName, err)
	}
	// Without the sync, the rename can land before the contents do, and
	// a power loss leaves a correctly named empty file.
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("cannot flush %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot close %s: %w", tmpName, err)
	}

	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("cannot replace %s: %w", path, err)
	}
	return nil
}

// ResourceKey returns the state map key for a resource: "type.name"
func ResourceKey(resourceType, name string) string {
	return resourceType + "." + name
}
