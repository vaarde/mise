package cmd

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// runApplyInWorkspace executes runApply, restoring package globals after.
func runApplyInWorkspace(t *testing.T, dir string, autoApprove bool, planFile, target string) (string, error) {
	t.Helper()

	prev := struct {
		config, plan, target, location string
		auto                           bool
		parallelism                    int
	}{configFile, applyPlanFile, applyTarget, applyLocation, applyAutoApprove, applyParallelism}

	t.Cleanup(func() {
		configFile, applyPlanFile, applyTarget = prev.config, prev.plan, prev.target
		applyLocation, applyAutoApprove, applyParallelism = prev.location, prev.auto, prev.parallelism
	})

	configFile = filepath.Join(dir, "mise.yaml")
	applyPlanFile, applyTarget, applyLocation = planFile, target, ""
	applyAutoApprove = autoApprove
	applyParallelism = engine.DefaultParallelism

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runApply(cmd, nil)
	return out.String(), err
}

// editTaxRate rewrites the percentage in the generated tax file.
func editTaxRate(t *testing.T, dir, from, to string) {
	t.Helper()

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	edited := strings.Replace(string(raw), `percentage: "`+from+`"`, `percentage: "`+to+`"`, 1)
	require.NotEqual(t, string(raw), edited, "the fixture should contain the rate being edited")
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))
}

func TestApplyWritesChangesAndUpdatesState(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "~ fakepos_tax.state_tax")
	assert.Contains(t, out, "Apply complete.")
	assert.Contains(t, out, "0 added")
	assert.Contains(t, out, "1 changed")

	// State reflects what is now live, so the next plan is clean.
	st, err := state.Load(dir)
	require.NoError(t, err)
	require.NotNil(t, st.LastApply)
	assert.Equal(t, "5.0", st.Resources["fakepos_tax.state_tax"].Properties["percentage"])
}

func TestApplyThenPlanShowsNoChanges(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	_, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	// The fake provider now reports the new rate, as a real POS would.
	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "5.0")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "5.0")},
	}

	planOut, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)
	assert.Contains(t, planOut, "No changes.",
		"applying a change then planning again must be clean")
}

func TestApplyRequiresConfirmation(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	// Tests have no terminal, so an unconfirmed apply must refuse rather
	// than block or assume yes.
	_, err := runApplyInWorkspace(t, dir, false, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--auto-approve")

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Nil(t, st.LastApply, "a refused apply must not have written anything")
}

func TestApplyWithNoChangesDoesNothing(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "No changes.")
	assert.NotContains(t, out, "Apply complete.")
}

func TestApplyCreatesANewResource(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(string(raw)+`
  - type: fakepos_tax
    name: alcohol_tax
    locations: ${group.all}
    properties:
      name: Alcohol Tax
      percentage: "6.0"
`), 0o644))

	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "+ fakepos_tax.alcohol_tax")
	assert.Contains(t, out, "1 added")

	st, err := state.Load(dir)
	require.NoError(t, err)
	entry, ok := st.Resources["fakepos_tax.alcohol_tax"]
	require.True(t, ok, "a created resource must be recorded with its new ID")
	assert.NotEmpty(t, entry.ProviderID)
}

func TestApplyFromASavedPlan(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "7.25")

	planFile := filepath.Join(dir, "change.plan")
	_, err := runPlanInWorkspace(t, dir, "", "", planFile)
	require.NoError(t, err)

	out, err := runApplyInWorkspace(t, dir, true, planFile, "")
	require.NoError(t, err)

	assert.Contains(t, out, "Applying saved plan")
	assert.Contains(t, out, "1 changed")

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "7.25", st.Resources["fakepos_tax.state_tax"].Properties["percentage"],
		"a saved plan applies exactly what was previewed")
}

func TestApplyTargetLimitsScope(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, []byte(string(raw)+`
  - type: fakepos_tax
    name: alcohol_tax
    locations: ${group.all}
    properties:
      name: Alcohol Tax
      percentage: "6.0"
`), 0o644))
	editTaxRate(t, dir, "4.5", "5.0")

	_, err = runApplyInWorkspace(t, dir, true, "", "alcohol_tax")
	require.NoError(t, err)

	st, err := state.Load(dir)
	require.NoError(t, err)
	assert.Contains(t, st.Resources, "fakepos_tax.alcohol_tax")
	assert.Equal(t, "4.5", st.Resources["fakepos_tax.state_tax"].Properties["percentage"],
		"the untargeted resource must be left untouched")
}

func TestApplyHoldsTheWorkspaceLock(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	// Simulate another apply already running.
	held, err := state.Acquire(dir, "apply")
	require.NoError(t, err)

	_, err = runApplyInWorkspace(t, dir, true, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "another mise process")

	require.NoError(t, held.Release())

	// With the lock free, the same apply works.
	_, err = runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)
}

func TestApplyReleasesTheLockWhenDone(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	_, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	assert.NoFileExists(t, state.LockPath(dir),
		"a finished apply must not leave the workspace locked")
}

func TestApplyRequiresAWorkspace(t *testing.T) {
	dir := t.TempDir()
	clearTokenEnv(t)

	_, err := runApplyInWorkspace(t, dir, true, "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestApplyRecordsPartialSuccessAndKeepsState(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	before, err := state.Load(dir)
	require.NoError(t, err)
	beforeSerial := before.Serial

	fakeState.writeErr = errWriteRejected

	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.Error(t, err, "an apply that failed must not report success")
	assert.Contains(t, out, "Apply incomplete.")
	assert.Contains(t, out, "rejected by the POS")

	// Nothing landed, so state must not claim the new rate is live or
	// move the serial. Advancing the serial here would make the exact
	// saved plan look stale even though the provider rejected every write.
	st, err2 := state.Load(dir)
	require.NoError(t, err2)
	assert.Equal(t, "4.5", st.Resources["fakepos_tax.state_tax"].Properties["percentage"],
		"a failed write must not be recorded as applied")
	assert.Equal(t, beforeSerial, st.Serial,
		"a zero-write failure must remain retryable from the same saved plan")
}

var errWriteRejected = errors.New("rejected by the POS")