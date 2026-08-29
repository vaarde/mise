package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// StaleLockAge is how long a lock may sit before Mise assumes the process
// holding it died. A crashed apply should not block the workspace
// forever, but the window has to be long enough that a slow apply across
// forty locations is never mistaken for a corpse.
const StaleLockAge = 10 * time.Minute

// Lock is the content of .mise/lock.
type Lock struct {
	PID        int       `json:"pid"`
	Host       string    `json:"host"`
	Operation  string    `json:"operation"`
	AcquiredAt time.Time `json:"acquired_at"`
}

// LockHandle is a held lock. Release it when the operation finishes.
type LockHandle struct {
	path string
}

// LockPath returns the lock file path for a workspace.
func LockPath(workDir string) string {
	return filepath.Join(workDir, StateDir, LockFileName)
}

// Acquire takes the workspace lock, so two applies cannot run at once and
// leave the state file describing neither of them.
//
// A lock older than StaleLockAge is broken automatically: the process
// that wrote it is assumed gone.
func Acquire(workDir, operation string) (*LockHandle, error) {
	dir := filepath.Join(workDir, StateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("cannot create %s directory: %w", StateDir, err)
	}

	path := LockPath(workDir)
	hostname, _ := os.Hostname()

	lock := Lock{
		PID:        os.Getpid(),
		Host:       hostname,
		Operation:  operation,
		AcquiredAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("cannot serialize lock: %w", err)
	}

	// O_EXCL makes creation the atomic test-and-set: whoever creates the
	// file wins, with no window between checking and writing.
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err == nil {
		defer file.Close()
		if _, err := file.Write(data); err != nil {
			return nil, fmt.Errorf("cannot write lock file: %w", err)
		}
		return &LockHandle{path: path}, nil
	}
	if !os.IsExist(err) {
		return nil, fmt.Errorf("cannot create lock file: %w", err)
	}

	// Someone holds it. Decide whether they are still alive.
	existing, readErr := readLock(path)
	if readErr != nil {
		return nil, fmt.Errorf("a lock file exists at %s but could not be read: %w\n"+
			"Remove it by hand if no other mise process is running", path, readErr)
	}

	age := time.Since(existing.AcquiredAt)
	if age < StaleLockAge {
		return nil, fmt.Errorf(
			"another mise process holds the workspace lock (%s, pid %d on %s, held for %s)\n"+
				"Wait for it to finish, or remove %s if that process is gone",
			existing.Operation, existing.PID, existing.Host, age.Round(time.Second), path)
	}

	// Stale — break it and take over.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot break stale lock at %s: %w", path, err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return nil, fmt.Errorf("cannot write lock file: %w", err)
	}
	return &LockHandle{path: path}, nil
}

// readLock parses an existing lock file.
func readLock(path string) (*Lock, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var lock Lock
	if err := json.Unmarshal(data, &lock); err != nil {
		return nil, err
	}
	return &lock, nil
}

// Release removes the lock. It is safe to call on a nil handle, so
// callers can defer it without checking.
func (h *LockHandle) Release() error {
	if h == nil || h.path == "" {
		return nil
	}
	if err := os.Remove(h.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("cannot release lock file %s: %w", h.path, err)
	}
	return nil
}
