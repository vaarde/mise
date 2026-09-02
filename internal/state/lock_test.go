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

func TestStaleLockIsOnlyBrokenOnce(t *testing.T) {
	// Two processes can both judge the same lock dead. Taking over used
	// to be a plain write, so both would "succeed" and two applies would
	// run against one workspace. The takeover now goes through the same
	// O_EXCL creation as a first acquire, so only one can win.
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, StateDir), 0o700))

	stale := Lock{
		PID:        99999,
		Host:       "gone",
		Operation:  "apply",
		AcquiredAt: time.Now().UTC().Add(-2 * StaleLockAge),
	}
	data, err := json.Marshal(stale)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(LockPath(dir), data, 0o600))

	first, err := Acquire(dir, "apply")
	require.NoError(t, err, "the first process breaks the stale lock and takes over")
	require.NotNil(t, first)

	// The lock it now holds is fresh, so the second process must wait.
	second, err := Acquire(dir, "apply")
	require.Error(t, err, "a second process must not also take over")
	assert.Nil(t, second)
	assert.Contains(t, err.Error(), "another mise process holds the workspace lock")

	require.NoError(t, first.Release())
}

func TestLockIsFreeAfterRelease(t *testing.T) {
	dir := t.TempDir()

	first, err := Acquire(dir, "apply")
	require.NoError(t, err)
	require.NoError(t, first.Release())

	second, err := Acquire(dir, "apply")
	require.NoError(t, err, "releasing a lock frees the workspace")
	require.NoError(t, second.Release())
}
