package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAcquireAndRelease(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(dir, "apply")
	require.NoError(t, err)
	assert.FileExists(t, LockPath(dir))

	require.NoError(t, handle.Release())
	assert.NoFileExists(t, LockPath(dir))
}

func TestAcquireRecordsWhoHoldsIt(t *testing.T) {
	dir := t.TempDir()

	handle, err := Acquire(dir, "apply")
	require.NoError(t, err)
	t.Cleanup(func() { _ = handle.Release() })

	data, err := os.ReadFile(LockPath(dir))
	require.NoError(t, err)

	var lock Lock
	require.NoError(t, json.Unmarshal(data, &lock))
	assert.Equal(t, os.Getpid(), lock.PID)
	assert.Equal(t, "apply", lock.Operation)
	assert.False(t, lock.AcquiredAt.IsZero())
}

func TestAcquireRefusesWhenHeld(t *testing.T) {
	dir := t.TempDir()

	first, err := Acquire(dir, "apply")
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Release() })

	_, err = Acquire(dir, "apply")
	require.Error(t, err, "two applies must not run at once")
	assert.Contains(t, err.Error(), "another mise process")
	assert.Contains(t, err.Error(), "apply")
}

func TestAcquireBreaksAStaleLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, StateDir), 0o700))

	// A lock left behind by a process that died an hour ago.
	stale := Lock{
		PID:        999999,
		Host:       "gone",
		Operation:  "apply",
		AcquiredAt: time.Now().Add(-time.Hour).UTC(),
	}
	data, err := json.Marshal(stale)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(LockPath(dir), data, 0o600))

	handle, err := Acquire(dir, "apply")
	require.NoError(t, err, "a crashed apply must not block the workspace forever")
	t.Cleanup(func() { _ = handle.Release() })

	current, err := os.ReadFile(LockPath(dir))
	require.NoError(t, err)
	var lock Lock
	require.NoError(t, json.Unmarshal(current, &lock))
	assert.Equal(t, os.Getpid(), lock.PID, "the stale lock should have been replaced")
}

func TestAcquireKeepsAFreshLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, StateDir), 0o700))

	fresh := Lock{PID: 999999, Operation: "apply", AcquiredAt: time.Now().Add(-time.Minute).UTC()}
	data, err := json.Marshal(fresh)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(LockPath(dir), data, 0o600))

	_, err = Acquire(dir, "apply")
	require.Error(t, err, "a lock held for a minute is not stale")
}

func TestAcquireReportsAnUnreadableLock(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, StateDir), 0o700))
	require.NoError(t, os.WriteFile(LockPath(dir), []byte("not json"), 0o600))

	_, err := Acquire(dir, "apply")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Remove it by hand",
		"the operator needs to be told how to recover")
}

func TestReleaseIsSafeOnNilAndTwice(t *testing.T) {
	var handle *LockHandle
	assert.NoError(t, handle.Release(), "deferring Release must be safe when Acquire failed")

	dir := t.TempDir()
	real, err := Acquire(dir, "apply")
	require.NoError(t, err)

	require.NoError(t, real.Release())
	assert.NoError(t, real.Release(), "releasing twice should not error")
}

func TestAcquireAfterRelease(t *testing.T) {
	dir := t.TempDir()

	first, err := Acquire(dir, "apply")
	require.NoError(t, err)
	require.NoError(t, first.Release())

	second, err := Acquire(dir, "apply")
	require.NoError(t, err)
	require.NoError(t, second.Release())
}
