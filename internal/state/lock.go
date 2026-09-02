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

	// Taking over a stale lock is itself a race: two processes can both
	// judge the same lock dead. Both attempts below go through O_EXCL
	// creation, so only one of them can succeed — the loser is told to
	// wait rather than silently writing its own lock over the winner's.
	handle, err := createLock(path, data)
	if err == nil {
		return handle, nil
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
		return nil, heldError(path, existing, age)
	}

	// Stale — break it and take over.
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("cannot break stale lock at %s: %w", path, err)
	}

	handle, err = createLock(path, data)
	if err == nil {
		return handle, nil
	}
	if os.IsExist(err) {
		// Another process broke the same stale lock first. Its lock is
		// fresh, so this is an ordinary "someone else holds it".
		if winner, readErr := readLock(path); readErr == nil {
			return nil, heldError(path, winner, time.Since(winner.AcquiredAt))
		}
		return nil, fmt.Errorf("another mise process took the workspace lock at %s while it was being broken\n"+
			"Wait for it to finish and try again", path)
	}
	return nil, fmt.Errorf("cannot write lock file: %w", err)
}

// createLock writes a lock file, failing if one already exists.
//
// O_EXCL makes creation the atomic test-and-set: whoever creates the
// file wins, with no window between checking and writing. The returned
// error satisfies os.IsExist when someone else holds the lock.
func createLock(path string, data []byte) (*LockHandle, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if _, err := file.Write(data); err != nil {
		os.Remove(path)
		return nil, fmt.Errorf("cannot write lock file: %w", err)
	}
	return &LockHandle{path: path}, nil
}

// heldError explains that another process holds the lock.
func heldError(path string, held *Lock, age time.Duration) error {
	return fmt.Errorf(
		"another mise process holds the workspace lock (%s, pid %d on %s, held for %s)\n"+
			"Wait for it to finish, or remove %s if that process is gone",
		held.Operation, held.PID, held.Host, age.Round(time.Second), path)
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
