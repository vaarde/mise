package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/state"
)

// plannedWorkspace builds a fetched workspace with one pending change
// and saves a plan for it, which is the setup every test below starts
// from.
func plannedWorkspace(t *testing.T, dir string) string {
	t.Helper()

	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "7.25")

	planFile := filepath.Join(dir, "change.plan")
	_, err := runPlanInWorkspace(t, dir, "", "", planFile)
	require.NoError(t, err)

	return planFile
}

func TestSavedPlanRecordsWhereItCameFrom(t *testing.T) {
	dir := t.TempDir()
	planFile := plannedWorkspace(t, dir)

	saved, err := LoadPlan(planFile)
	require.NoError(t, err)

	assert.Equal(t, savedPlanFormat, saved.FormatVersion)
	assert.Equal(t, fakeProviderName, saved.Identity.Provider)
	assert.Equal(t, envSandbox, saved.Identity.Environment)
	assert.Equal(t, "ACCOUNT_A", saved.Identity.AccountID)
	assert.NotEmpty(t, saved.ConfigDigest)
	assert.False(t, saved.CreatedAt.IsZero())
	assert.NotEmpty(t, saved.MiseVersion)

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, st.Serial, saved.StateSerial)

	require.Len(t, saved.Plan.Changes, 1, "the plan itself is still there")
}

func TestApplySavedPlanRefusesADifferentAccount(t *testing.T) {
	// A plan is a list of provider IDs, and those exist only within the
	// account that issued them. Applied elsewhere, every ID misses and
	// apply re-creates the entire configuration in the wrong place.
	dir := t.TempDir()
	planFile := plannedWorkspace(t, dir)

	fakeState.accountID = "ACCOUNT_B"

	_, err := runApplyInWorkspace(t, dir, true, planFile, "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "ACCOUNT_A")
	assert.Contains(t, err.Error(), "ACCOUNT_B")
	assert.Empty(t, fakeState.written, "nothing may be written to the wrong account")
}

func TestApplySavedPlanRefusesStaleState(t *testing.T) {
	// State moving on means something else applied in between, so the
	// plan's recorded IDs and version tokens may already be stale.
	dir := t.TempDir()
	planFile := plannedWorkspace(t, dir)

	st, err := state.Load(dir)
	require.NoError(t, err)
	require.NoError(t, st.Save(dir)) // someone else applied

	_, err = runApplyInWorkspace(t, dir, true, planFile, "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "changed since this plan was made")
	assert.Contains(t, err.Error(), "mise plan")
	assert.Empty(t, fakeState.written)
}

func TestApplySavedPlanRefusesEditedConfig(t *testing.T) {
	// The plan would apply what it previewed, which is no longer what
	// the files say — so the reviewed change is not the one happening.
	dir := t.TempDir()
	planFile := plannedWorkspace(t, dir)

	editTaxRate(t, dir, "7.25", "9.0")

	_, err := runApplyInWorkspace(t, dir, true, planFile, "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "edited since this plan was made")
	assert.Empty(t, fakeState.written)
}

func TestApplySavedPlanWorksWhenNothingHasMoved(t *testing.T) {
	dir := t.TempDir()
	planFile := plannedWorkspace(t, dir)

	_, err := runApplyInWorkspace(t, dir, true, planFile, "")
	require.NoError(t, err)

	require.Len(t, fakeState.written, 1)
	assert.Equal(t, "7.25", fakeState.written[0].Properties["percentage"],
		"a verified plan applies exactly what it previewed")
}

func TestApplyRefusesAPlanFileFromBeforeProvenance(t *testing.T) {
	// Plans written before Mise recorded which account they were built
	// for cannot be verified, so they are refused rather than trusted.
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	planFile := filepath.Join(dir, "old.plan")
	require.NoError(t, os.WriteFile(planFile,
		[]byte(`{"changes":[],"summary":{},"locations":[]}`), 0o644))

	_, err := LoadPlan(planFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be verified")
	assert.Contains(t, err.Error(), "mise plan --out")
}

func TestApplyRefusesAPlanFileFromANewerMise(t *testing.T) {
	dir := t.TempDir()
	planFile := filepath.Join(dir, "future.plan")

	require.NoError(t, os.WriteFile(planFile,
		[]byte(`{"format_version":99,"plan":{"changes":[],"summary":{}}}`), 0o644))

	_, err := LoadPlan(planFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "newer Mise")
}

func TestFetchRecordsTheAccountInState(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	st, err := state.Load(dir)
	require.NoError(t, err)

	assert.Equal(t, state.Identity{
		Provider:    fakeProviderName,
		Environment: envSandbox,
		AccountID:   "ACCOUNT_A",
	}, st.Identity())
}

func TestPlanRefusesStateFromAnotherAccount(t *testing.T) {
	// Without this, every recorded provider ID misses on the new
	// account, so plan reads each resource as deleted outside Mise and
	// proposes to create it — duplicating the whole configuration.
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.accountID = "ACCOUNT_B"

	_, err := runPlanInWorkspace(t, dir, "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "re-create every resource")
}

func TestDriftRefusesStateFromAnotherAccount(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.accountID = "ACCOUNT_B"

	_, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ACCOUNT_B")
}

func TestFetchRefusesToOverwriteAnotherAccountsWorkspace(t *testing.T) {
	// Replacing the state file wholesale would orphan every resource the
	// first account owns: nothing afterwards would know those live
	// objects were ever managed.
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.accountID = "ACCOUNT_B"

	_, err := runFetchInWorkspace(t, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force", "the error says how to proceed on purpose")

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "ACCOUNT_A", st.Identity().AccountID, "state is untouched")
}

func TestFetchForceReplacesTheWorkspaceAccount(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.accountID = "ACCOUNT_B"

	_, err := runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "ACCOUNT_B", st.Identity().AccountID)
}

func TestWorkspaceWithoutAnIdentityIsNotBlocked(t *testing.T) {
	// State written by an older Mise records no account. There is
	// nothing to compare against, so the run proceeds and the next
	// apply stamps it.
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	stripIdentity(t, dir)
	fakeState.accountID = "ACCOUNT_B"

	_, err := runPlanInWorkspace(t, dir, "", "", "")
	assert.NoError(t, err, "an unstamped workspace is not refused")
}

// stripIdentity rewrites state.json without its identity fields, as an
// older Mise would have written it.
func stripIdentity(t *testing.T, dir string) {
	t.Helper()

	path := filepath.Join(dir, state.StateDir, state.StateFileName)
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var generic map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &generic))
	delete(generic, "provider")
	delete(generic, "environment")
	delete(generic, "account_id")

	edited, err := json.Marshal(generic)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, edited, 0o600))
}

func TestPlanRefusesAGroupThatMatchesNoLocation(t *testing.T) {
	// End to end for the bug this all started with: a filter matching no
	// location resolved to an empty list, and Square reads an empty
	// location list as present_at_all_locations. The plan would have
	// shown the scope shrinking to nothing while the apply widened it to
	// every location on the account.
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	addLocationGroup(t, dir, "florida", `      state: "FL"`)
	scopeTaxToGroup(t, dir, "florida")

	_, err := runPlanInWorkspace(t, dir, "", "", "")
	require.Error(t, err)

	assert.Contains(t, err.Error(), "florida")
	assert.Contains(t, err.Error(), "matched none")
	assert.Empty(t, fakeState.written)
}

// addLocationGroup appends a group definition to mise.yaml.
func addLocationGroup(t *testing.T, dir, name, filterBody string) {
	t.Helper()

	path := filepath.Join(dir, "mise.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	block := "\n  " + name + ":\n    filter:\n" + filterBody + "\n"
	require.NoError(t, os.WriteFile(path, append(raw, []byte(block)...), 0o644))
}

// scopeTaxToGroup points the generated tax file at a location group.
func scopeTaxToGroup(t *testing.T, dir, group string) {
	t.Helper()

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	edited := strings.Replace(string(raw), "locations: ${group.all}",
		"locations: ${group."+group+"}", 1)
	require.NotEqual(t, string(raw), edited, "the fixture should be scoped to a group")
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
}
