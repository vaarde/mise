package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveIsAtomicAndLeavesNoDebris(t *testing.T) {
	// The state file is written through a temporary file and a rename,
	// so a crash mid-write leaves the previous state rather than a
	// truncated one. A truncated state.json means Mise no longer knows
	// which live resources it created, and the next apply creates them
	// all a second time.
	dir := t.TempDir()

	st := New("square")
	st.Resources["square_catalog_tax.sales_tax"] = &ResourceState{ProviderID: "TAX_1"}
	require.NoError(t, st.Save(dir))

	entries, err := os.ReadDir(filepath.Join(dir, StateDir))
	require.NoError(t, err)
	require.Len(t, entries, 1, "the temporary file must not survive the write")
	assert.Equal(t, StateFileName, entries[0].Name())

	// The file on disk is complete and parses.
	data, err := os.ReadFile(filepath.Join(dir, StateDir, StateFileName))
	require.NoError(t, err)

	var reloaded State
	require.NoError(t, json.Unmarshal(data, &reloaded))
	assert.Equal(t, "TAX_1", reloaded.Resources["square_catalog_tax.sales_tax"].ProviderID)
}

func TestSaveOverwritesInPlace(t *testing.T) {
	dir := t.TempDir()

	first := New("square")
	first.Resources["a"] = &ResourceState{ProviderID: "ID_1"}
	require.NoError(t, first.Save(dir))

	second := New("square")
	second.Resources["b"] = &ResourceState{ProviderID: "ID_2"}
	require.NoError(t, second.Save(dir))

	loaded, err := Load(dir)
	require.NoError(t, err)
	assert.NotContains(t, loaded.Resources, "a")
	assert.Contains(t, loaded.Resources, "b")

	entries, err := os.ReadDir(filepath.Join(dir, StateDir))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "repeated saves must not accumulate temporary files")
}

func TestSaveBumpsTheSerial(t *testing.T) {
	// A saved plan records the serial it was computed against, so it can
	// tell that something else applied in between.
	dir := t.TempDir()
	st := New("square")

	require.NoError(t, st.Save(dir))
	assert.Equal(t, 1, st.Serial)

	require.NoError(t, st.Save(dir))
	assert.Equal(t, 2, st.Serial)

	loaded, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, 2, loaded.Serial, "the serial survives a round trip")
}

func TestIdentityRoundTrips(t *testing.T) {
	dir := t.TempDir()

	st := New("square")
	st.SetIdentity(Identity{Provider: "square", Environment: "sandbox", AccountID: "MERCHANT_A"})
	require.NoError(t, st.Save(dir))

	loaded, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t,
		Identity{Provider: "square", Environment: "sandbox", AccountID: "MERCHANT_A"},
		loaded.Identity())
}

func TestStateWrittenBeforeIdentitiesExistedStillLoads(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, StateDir), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(dir, StateDir, StateFileName),
		[]byte(`{"version":1,"provider":"square","resources":{},"locations":{}}`), 0o600))

	loaded, err := Load(dir)
	require.NoError(t, err)
	assert.Equal(t, 0, loaded.Serial)
	assert.Equal(t, "square", loaded.Identity().Provider)
	assert.Empty(t, loaded.Identity().AccountID,
		"an older state file names no account, and is checked on what it does know")
}
