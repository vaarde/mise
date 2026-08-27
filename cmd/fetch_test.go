package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// initWorkspace runs init against the fake provider so fetch has a
// workspace to operate on.
func initWorkspace(t *testing.T, dir string, locations []provider.Location) {
	t.Helper()

	clearTokenEnv(t)
	resetFakeState(t, locations)

	_, err := runInitInWorkspace(t, dir, initOptions{
		platform:    fakeProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok",
	})
	require.NoError(t, err)
}

// runFetchInWorkspace executes runFetch, restoring package globals after.
func runFetchInWorkspace(t *testing.T, dir string, force bool) (string, error) {
	t.Helper()

	previousConfig, previousForce, previousParallelism := configFile, fetchForce, fetchParallelism
	t.Cleanup(func() {
		configFile, fetchForce, fetchParallelism = previousConfig, previousForce, previousParallelism
	})

	configFile = filepath.Join(dir, "mise.yaml")
	fetchForce = force
	fetchParallelism = engine.DefaultParallelism

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runFetch(cmd, nil)
	return out.String(), err
}

func fakeTax(id, name, percentage string) *provider.Resource {
	return &provider.Resource{
		Type:       "fakepos_tax",
		Name:       name,
		ProviderID: id,
		Version:    "7",
		Properties: map[string]interface{}{"name": name, "percentage": percentage},
	}
}

func TestRunFetchWritesConfigAndState(t *testing.T) {
	dir := t.TempDir()
	locations := []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta", State: "GA", Timezone: "America/New_York"},
		{ID: "LOC_NSH", Name: "Nashville", State: "TN", Timezone: "America/Chicago"},
	}
	initWorkspace(t, dir, locations)

	fakeState.resourceTypes = []string{"fakepos_location", "fakepos_tax"}
	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5"), fakeTax("TAX_2", "Alcohol Tax", "6.0")},
		"fakepos_tax@LOC_NSH": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	out, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	// Summary matches the PRD's "Fetched N resources across M locations."
	assert.Contains(t, out, "Fetched 2 resources across 2 locations.")
	assert.Contains(t, out, "fakepos_tax")

	// Config file exists and reloads through the normal loader.
	assert.FileExists(t, filepath.Join(dir, "fakepos_tax.yaml"))
	assert.FileExists(t, filepath.Join(dir, config.LocationsFileName))

	loaded, err := config.LoadResources(dir, "mise.yaml")
	require.NoError(t, err)
	require.Len(t, loaded, 2)

	byName := map[string]config.ResourceDef{}
	for _, res := range loaded {
		byName[res.FullName()] = res
	}

	stateTax := byName["fakepos_tax.state_tax"]
	assert.Equal(t, config.GroupAll, stateTax.Locations, "a tax at every location uses the group shorthand")
	assert.Equal(t, "4.5", stateTax.Properties["percentage"])

	alcohol := byName["fakepos_tax.alcohol_tax"]
	assert.Equal(t, []interface{}{"LOC_ATL"}, alcohol.Locations, "a partial rollout lists its locations")
}

func TestRunFetchWritesStateFile(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta", State: "GA"}})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	loaded, err := state.Load(dir)
	require.NoError(t, err)

	assert.Equal(t, fakeProviderName, loaded.Provider)
	require.NotNil(t, loaded.LastFetch, "fetch records when it ran, so drift has a baseline")

	entry, ok := loaded.Resources["fakepos_tax.state_tax"]
	require.True(t, ok)
	assert.Equal(t, "TAX_1", entry.ProviderID, "state maps the config name to the provider ID")
	assert.Equal(t, "7", entry.Version)
	assert.Equal(t, []string{"LOC_ATL"}, entry.Locations)

	location, ok := loaded.Locations["LOC_ATL"]
	require.True(t, ok)
	assert.Equal(t, "Atlanta", location.Name)
}

func TestRunFetchSkipsTheLocationResourceType(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.resourceTypes = []string{"fakepos_location", "fakepos_tax"}
	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
		"fakepos_location@LOC_ATL": {{
			Type: "fakepos_location", Name: "Atlanta", ProviderID: "LOC_ATL",
			Properties: map[string]interface{}{"name": "Atlanta"},
		}},
	}

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	// Locations belong in locations.yaml, read through ListLocations —
	// they must not also appear as a resource file.
	assert.NoFileExists(t, filepath.Join(dir, "fakepos_location.yaml"))
	assert.FileExists(t, filepath.Join(dir, config.LocationsFileName))
}

func TestRunFetchIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	_, err := runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(dir, "fakepos_tax.yaml"))
	require.NoError(t, err)

	_, err = runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)
	second, err := os.ReadFile(filepath.Join(dir, "fakepos_tax.yaml"))
	require.NoError(t, err)

	assert.Equal(t, string(first), string(second),
		"fetching twice with nothing changed must not produce a diff")
}

func TestRunFetchRefusesToOverwriteWithoutConfirmation(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	_, err := runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)

	// Simulate a hand edit, then fetch again without --force. Tests have
	// no terminal, so the prompt must fail loudly rather than block.
	edited := "resources:\n  - type: fakepos_tax\n    name: hand_edited\n    properties:\n      name: Mine\n"
	path := filepath.Join(dir, "fakepos_tax.yaml")
	require.NoError(t, os.WriteFile(path, []byte(edited), 0o644))

	_, err = runFetchInWorkspace(t, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, edited, string(raw), "a refused fetch must leave local edits untouched")
}

func TestRunFetchOverwritesWithForce(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	path := filepath.Join(dir, "fakepos_tax.yaml")
	require.NoError(t, os.WriteFile(path, []byte("resources: []\n"), 0o644))

	_, err := runFetchInWorkspace(t, dir, true)
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "state_tax")
}

func TestRunFetchReportsUnresolvedReferences(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	item := &provider.Resource{
		Type: "fakepos_tax", Name: "Orphan Tax", ProviderID: "TAX_1",
		Properties: map[string]interface{}{
			"name":     "Orphan Tax",
			"category": provider.Ref{ResourceType: "fakepos_category", ProviderID: "CAT_MISSING"},
		},
	}
	fakeState.resources = map[string][]*provider.Resource{"fakepos_tax@LOC_ATL": {item}}

	out, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err, "a dangling reference is a warning, not a failure")
	assert.Contains(t, out, "Warning:")
	assert.Contains(t, out, "CAT_MISSING")
}

func TestRunFetchPropagatesReadFailures(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.readErr = fmt.Errorf("503 service unavailable")

	_, err := runFetchInWorkspace(t, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service unavailable")

	// A failed fetch must not leave a state file claiming success.
	assert.NoFileExists(t, filepath.Join(dir, ".mise", "state.json"))
}

func TestRunFetchRequiresAWorkspace(t *testing.T) {
	dir := t.TempDir()
	clearTokenEnv(t)

	_, err := runFetchInWorkspace(t, dir, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestRunFetchHandlesEmptyCatalog(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	out, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	assert.Contains(t, out, "Fetched 0 resources across 1 location.")
	assert.FileExists(t, filepath.Join(dir, config.LocationsFileName))
}

func TestStateFileIsValidJSON(t *testing.T) {
	dir := t.TempDir()
	initWorkspace(t, dir, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	fakeState.resources = map[string][]*provider.Resource{
		"fakepos_tax@LOC_ATL": {fakeTax("TAX_1", "State Tax", "4.5")},
	}

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, ".mise", "state.json"))
	require.NoError(t, err)

	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(raw, &parsed), "state must stay git-diffable JSON")
	assert.Equal(t, float64(1), parsed["version"])
	assert.True(t, strings.Contains(string(raw), "  "), "state should be indented for readable diffs")
}
