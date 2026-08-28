package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

func init() { color.NoColor = true }

// runPlanInWorkspace executes runPlan, restoring package globals after.
func runPlanInWorkspace(t *testing.T, dir string, target, location, outFile string) (string, error) {
	t.Helper()

	prevConfig, prevTarget := configFile, planTarget
	prevLocation, prevOut, prevPar := planLocation, planOutFile, planParallelism
	t.Cleanup(func() {
		configFile, planTarget = prevConfig, prevTarget
		planLocation, planOutFile, planParallelism = prevLocation, prevOut, prevPar
	})

	configFile = filepath.Join(dir, "mise.yaml")
	planTarget, planLocation, planOutFile = target, location, outFile
	planParallelism = engine.DefaultParallelism

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runPlan(cmd, nil)
	return out.String(), err
}

// fetchedWorkspace builds a workspace that has been init'd and fetched,
// which is the state a real operator plans from.
func fetchedWorkspace(t *testing.T, dir string) {
	t.Helper()

	initWorkspace(t, dir, []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta", State: "GA"},
		{ID: "LOC_NSH", Name: "Nashville", State: "TN"},
	})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	_, err := runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)
}

func TestPlanAfterFetchShowsNoChanges(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	// The PRD's round-trip fidelity criterion: fetch, change nothing,
	// and plan must report no changes.
	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "No changes.")
	assert.NotContains(t, out, "to add")
}

func TestPlanDetectsAnEditedTaxRate(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	// Edit the rate the way an operator would: in the YAML file.
	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.Replace(string(raw), `percentage: "4.5"`, `percentage: "5.0"`, 1)
	require.NotEqual(t, string(raw), edited, "the fixture should contain the rate we are editing")
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "~ fakepos_tax.state_tax")
	assert.Contains(t, out, `percentage: "4.5" → "5.0"`)
	assert.Contains(t, out, "Plan: 0 to add, 1 to change, 0 to destroy.")
}

func TestPlanDetectsANewResource(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	addition := string(raw) + `
  - type: fakepos_tax
    name: alcohol_tax
    locations: ${group.all}
    properties:
      name: Alcohol Tax
      percentage: "6.0"
`
	require.NoError(t, os.WriteFile(path, []byte(addition), 0o644))

	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "+ fakepos_tax.alcohol_tax")
	assert.Contains(t, out, `name       = "Alcohol Tax"`)
	assert.Contains(t, out, "Plan: 1 to add, 0 to change, 0 to destroy.")
}

func TestPlanMakesNoChangesToDisk(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	before, err := os.ReadFile(path)
	require.NoError(t, err)
	stateBefore, err := os.ReadFile(filepath.Join(dir, ".mise", "state.json"))
	require.NoError(t, err)

	_, err = runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)

	after, err := os.ReadFile(path)
	require.NoError(t, err)
	stateAfter, err := os.ReadFile(filepath.Join(dir, ".mise", "state.json"))
	require.NoError(t, err)

	assert.Equal(t, string(before), string(after), "plan is a preview and must not write config")
	assert.Equal(t, string(stateBefore), string(stateAfter), "plan must not write state")
}

func TestPlanTargetFlag(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	addition := string(raw) + `
  - type: fakepos_tax
    name: alcohol_tax
    locations: ${group.all}
    properties:
      name: Alcohol Tax
      percentage: "6.0"
`
	require.NoError(t, os.WriteFile(path, []byte(addition), 0o644))

	out, err := runPlanInWorkspace(t, dir, "alcohol_tax", "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "alcohol_tax")
	assert.Contains(t, out, "Plan: 1 to add, 0 to change, 0 to destroy.")
}

func TestPlanLocationFlag(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	out, err := runPlanInWorkspace(t, dir, "", "Atlanta", "")
	require.NoError(t, err)
	assert.Contains(t, out, "No changes.")

	_, err = runPlanInWorkspace(t, dir, "", "Miami", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Miami")
}

func TestPlanSavesAndReloadsAPlanFile(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	edited := strings.Replace(string(raw), `percentage: "4.5"`, `percentage: "7.25"`, 1)
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	planFile := filepath.Join(dir, "tax-change.plan")
	out, err := runPlanInWorkspace(t, dir, "", "", planFile)
	require.NoError(t, err)

	assert.Contains(t, out, "Plan saved to")
	assert.Contains(t, out, "mise apply --plan")
	assert.FileExists(t, planFile)

	// A saved plan must carry enough to apply exactly what was previewed.
	loaded, err := LoadPlan(planFile)
	require.NoError(t, err)

	require.Len(t, loaded.Changes, 1)
	change := loaded.Changes[0]
	assert.Equal(t, engine.ActionUpdate, change.Action)
	assert.Equal(t, "fakepos_tax.state_tax", change.FullName())
	assert.Equal(t, "TAX_1", change.ProviderID)
	assert.Equal(t, "7.25", change.Desired["percentage"],
		"the saved plan carries the full desired state, not just the diff")
	assert.Len(t, loaded.Locations, 2, "a saved plan remembers the locations it covered")
}

func TestPlanRequiresAWorkspace(t *testing.T) {
	dir := t.TempDir()
	clearTokenEnv(t)

	_, err := runPlanInWorkspace(t, dir, "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestPlanRequiresDeclaredResources(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	// A workspace that has been init'd but never fetched has no configs.
	_, err := runPlanInWorkspace(t, dir, "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise fetch")
}

func TestPlanReportsBrokenReferences(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	broken := string(raw) + `
  - type: fakepos_tax
    name: broken
    locations: ${group.all}
    properties:
      category: ref(fakepos_category.nonexistent)
`
	require.NoError(t, os.WriteFile(path, []byte(broken), 0o644))

	_, err = runPlanInWorkspace(t, dir, "", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nonexistent")
	assert.Contains(t, err.Error(), "not declared")
}

func TestPlanDetectsLocationScopeChangeFromGroups(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	// Narrow the tax from every location to Georgia only by editing the
	// group reference, which is the whole point of location groups.
	path := filepath.Join(dir, "fakepos_tax.yaml")
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	narrowed := strings.Replace(string(raw), "locations: ${group.all}", "locations: [LOC_ATL]", 1)
	require.NotEqual(t, string(raw), narrowed)
	require.NoError(t, os.WriteFile(path, []byte(narrowed), 0o644))

	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err)

	assert.Contains(t, out, "~ fakepos_tax.state_tax")
	assert.Contains(t, out, "locations:")
	assert.Contains(t, out, "LOC_NSH")
}
