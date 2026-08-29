package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

// runDriftInWorkspace executes runDrift, restoring package globals after.
func runDriftInWorkspace(t *testing.T, dir, location, resourceType string, asJSON bool) (string, error) {
	t.Helper()

	prevConfig, prevLocation := configFile, driftLocation
	prevType, prevJSON, prevPar := driftType, driftJSON, driftParallelism
	t.Cleanup(func() {
		configFile, driftLocation = prevConfig, prevLocation
		driftType, driftJSON, driftParallelism = prevType, prevJSON, prevPar
	})

	configFile = dir + "/mise.yaml"
	driftLocation, driftType, driftJSON = location, resourceType, asJSON
	driftParallelism = engine.DefaultParallelism

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runDrift(cmd, nil)
	return out.String(), err
}

func TestDriftReportsNothingRightAfterFetch(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	out, err := runDriftInWorkspace(t, dir, "", "", false)
	require.NoError(t, err)
	assert.Contains(t, out, "No drift.")
}

func TestDriftDetectsAChangeMadeOutsideMise(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	// Someone edits the rate in the POS dashboard.
	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	out, err := runDriftInWorkspace(t, dir, "", "", false)

	require.Error(t, err, "drift should be signalled to the caller")
	assert.Contains(t, out, "Drift detected:")
	assert.Contains(t, out, "~ fakepos_tax.state_tax")
	assert.Contains(t, out, `"4.5" (expected)`)
	assert.Contains(t, out, `"9.25" (actual)`)
	assert.Contains(t, out, "⚠ Changed outside of Mise")
}

func TestDriftExitsWithACodeAScheduledCheckCanUse(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	_, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)

	// Exit 2 means "drift found", distinct from exit 1 "command failed".
	assert.Equal(t, ExitCodeDrift, ExitCode(err))

	var coder ExitCoder
	require.True(t, errors.As(err, &coder))
	assert.Equal(t, 2, coder.ExitCode())
}

func TestDriftErrorIsSilentBecauseTheReportAlreadySaidIt(t *testing.T) {
	// The report ends with "N resources drifted"; printing
	// "Error: drift detected" on top of that would be noise, and drift
	// being found is not an operational failure.
	err := &driftDetectedError{count: 2}
	assert.True(t, err.Silent())
	assert.Equal(t, "drift detected in 2 resources", err.Error())

	assert.Equal(t, "drift detected in 1 resource", (&driftDetectedError{count: 1}).Error())
}

func TestExitCodeDefaultsToOne(t *testing.T) {
	assert.Equal(t, 0, ExitCode(nil))
	assert.Equal(t, 1, ExitCode(fmt.Errorf("something broke")))
	assert.Equal(t, 2, ExitCode(&driftDetectedError{count: 1}))
}

func TestDriftDetectsADeletedResource(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	// The tax is gone from the POS entirely.
	fakeState.resources = map[string][]*provider.Resource{}

	out, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)

	assert.Contains(t, out, "- fakepos_tax.state_tax")
	assert.Contains(t, out, "⚠ Deleted outside of Mise")
}

func TestDriftJSONOutput(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	out, err := runDriftInWorkspace(t, dir, "", "", true)
	require.Error(t, err)

	var parsed struct {
		Checked int `json:"checked"`
		Drifted []struct {
			FullName string `json:"full_name"`
			Reason   string `json:"reason"`
		} `json:"drifted"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &parsed),
		"scheduled checks consume this, so it must be valid JSON")

	require.Len(t, parsed.Drifted, 1)
	assert.Equal(t, "fakepos_tax.state_tax", parsed.Drifted[0].FullName)
	assert.Equal(t, "changed", parsed.Drifted[0].Reason)
	assert.NotContains(t, out, "⚠", "JSON output should carry no decoration")
}

func TestDriftMakesNoChangesToDisk(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	before := readWorkspaceFiles(t, dir)

	_, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)

	after := readWorkspaceFiles(t, dir)
	assert.Equal(t, before, after,
		"a drift report is evidence, not permission to accept the change")
}

func TestDriftFiltersByType(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	_, err := runDriftInWorkspace(t, dir, "", "fakepos_tax", false)
	require.Error(t, err, "the tax did drift")

	_, err = runDriftInWorkspace(t, dir, "", "fakepos_nonexistent", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "fakepos_nonexistent")
}

func TestDriftFiltersByLocation(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "9.25")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "9.25")},
	}

	out, err := runDriftInWorkspace(t, dir, "Atlanta", "", false)
	require.Error(t, err)
	assert.Contains(t, out, "fakepos_tax.state_tax")

	_, err = runDriftInWorkspace(t, dir, "Miami", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Miami")
}

func TestDriftRequiresAWorkspace(t *testing.T) {
	dir := t.TempDir()
	clearTokenEnv(t)

	_, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestDriftRequiresStateFromAFetch(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	// Initialised but never fetched: there is no baseline to compare to.
	_, err := runDriftInWorkspace(t, dir, "", "", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise fetch")
}

// readWorkspaceFiles snapshots every file in a workspace, so a test can
// assert that a command changed nothing on disk.
func readWorkspaceFiles(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		rel, relErr := filepath.Rel(dir, path)
		if relErr != nil {
			return relErr
		}
		files[filepath.ToSlash(rel)] = string(data)
		return nil
	})
	require.NoError(t, err)
	return files
}
