package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
)

// fakeProviderName is a stand-in POS platform registered only in
// tests, so runInit can be exercised end to end without a network.
const fakeProviderName = "fakepos"

// fakeState records what the fake adapter was handed and what it
// should return. Package-level because the registry hands out fresh
// instances built by a factory.
var fakeState struct {
	configuredToken       string
	configuredEnvironment string
	locations             []provider.Location
	listErr               error
	configureErr          error

	// resourceTypes and resources drive ReadAll, keyed "type@location".
	resourceTypes []string
	resources     map[string][]*provider.Resource
	readErr       error
}

type fakeProvider struct{}

func (f *fakeProvider) Name() string { return fakeProviderName }

func (f *fakeProvider) Configure(cfg provider.ProviderConfig) error {
	if fakeState.configureErr != nil {
		return fakeState.configureErr
	}
	fakeState.configuredToken = cfg.Credentials.AccessToken
	fakeState.configuredEnvironment = cfg.Environment
	return nil
}

func (f *fakeProvider) ListLocations(context.Context) ([]provider.Location, error) {
	return fakeState.locations, fakeState.listErr
}

func (f *fakeProvider) ResourceTypes() []string {
	if len(fakeState.resourceTypes) > 0 {
		return fakeState.resourceTypes
	}
	return []string{"fakepos_tax"}
}

func (f *fakeProvider) Read(context.Context, string, string, string) (*provider.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (f *fakeProvider) ReadAll(_ context.Context, resourceType, locationID string) ([]*provider.Resource, error) {
	if fakeState.readErr != nil {
		return nil, fakeState.readErr
	}
	return fakeState.resources[resourceType+"@"+locationID], nil
}

func (f *fakeProvider) Create(context.Context, string, *provider.Resource, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (f *fakeProvider) Update(context.Context, string, string, *provider.Resource, string) error {
	return fmt.Errorf("not implemented")
}

func (f *fakeProvider) Delete(context.Context, string, string, string) error {
	return fmt.Errorf("not implemented")
}

func init() {
	provider.Register(fakeProviderName, func() provider.Provider { return &fakeProvider{} })
}

// runInitInWorkspace executes runInit against a temp directory with
// the given options, restoring the command's package globals after.
func runInitInWorkspace(t *testing.T, dir string, opts initOptions) (string, error) {
	t.Helper()

	previousConfig, previousOpts := configFile, initOpts
	t.Cleanup(func() { configFile, initOpts = previousConfig, previousOpts })

	configFile = filepath.Join(dir, "mise.yaml")
	initOpts = opts

	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runInit(cmd, nil)
	return out.String(), err
}

// resetFakeState clears the fake adapter between tests.
func resetFakeState(t *testing.T, locations []provider.Location) {
	t.Helper()

	fakeState.configuredToken = ""
	fakeState.configuredEnvironment = ""
	fakeState.locations = locations
	fakeState.listErr = nil
	fakeState.configureErr = nil
	fakeState.resourceTypes = nil
	fakeState.resources = map[string][]*provider.Resource{}
	fakeState.readErr = nil

	t.Cleanup(func() {
		fakeState.locations = nil
		fakeState.listErr = nil
		fakeState.configureErr = nil
		fakeState.resourceTypes = nil
		fakeState.resources = nil
		fakeState.readErr = nil
	})
}

func TestRunInitBuildsCompleteWorkspace(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta - Peachtree St", State: "GA", Address: "1100 Peachtree St NE, Atlanta, GA"},
		{ID: "LOC_NSH", Name: "Nashville", State: "TN"},
	})

	out, err := runInitInWorkspace(t, dir, initOptions{
		platform:    fakeProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok-123",
	})
	require.NoError(t, err)

	// The token reached the adapter, and the adapter was pointed at the
	// environment the operator asked for.
	assert.Equal(t, "tok-123", fakeState.configuredToken)
	assert.Equal(t, envSandbox, fakeState.configuredEnvironment)

	// mise.yaml is present and parses.
	cfg, err := config.LoadRoot(filepath.Join(dir, "mise.yaml"))
	require.NoError(t, err)
	assert.Equal(t, fakeProviderName, cfg.Provider.Platform)
	assert.Equal(t, envSandbox, cfg.Provider.Environment)
	assert.Equal(t, credentials.MethodAccessToken, cfg.Provider.Credentials.Method)

	// Credentials are stored, and only in .mise/.
	stored, err := credentials.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "tok-123", stored.AccessToken)

	raw, err := os.ReadFile(filepath.Join(dir, "mise.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "tok-123", "the token must never land in mise.yaml")

	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gitignore), ".mise/credentials")

	// The operator sees what account they connected to and what to do next.
	assert.Contains(t, out, "Atlanta - Peachtree St")
	assert.Contains(t, out, "LOC_NSH")
	assert.Contains(t, out, "2 locations")
	assert.Contains(t, out, "mise fetch")
}

func TestRunInitLeavesNothingBehindWhenValidationFails(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, nil)
	fakeState.listErr = fmt.Errorf("401 unauthorized")

	_, err := runInitInWorkspace(t, dir, initOptions{
		platform:    fakeProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "bad-token",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "could not verify credentials")

	// A rejected token must not leave a half-built workspace: no config,
	// no credentials file on disk.
	assert.NoFileExists(t, filepath.Join(dir, "mise.yaml"))
	assert.False(t, credentials.Exists(dir))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "a failed init should not touch the directory")
}

func TestRunInitSucceedsOnAnAccountWithNoLocations(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, nil)

	out, err := runInitInWorkspace(t, dir, initOptions{
		platform:    fakeProviderName,
		environment: envProduction,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok",
	})
	require.NoError(t, err)

	assert.Contains(t, out, "no locations yet")
	assert.FileExists(t, filepath.Join(dir, "mise.yaml"))
}

func TestRunInitRefusesToReinitializeWithoutForce(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	opts := initOptions{
		platform:    fakeProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok-first",
	}
	_, err := runInitInWorkspace(t, dir, opts)
	require.NoError(t, err)

	// Second run, non-interactive, no --force.
	opts.accessToken = "tok-second"
	_, err = runInitInWorkspace(t, dir, opts)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")

	stored, err := credentials.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "tok-first", stored.AccessToken, "the existing credentials must survive a refused re-init")
}

func TestRunInitReinitializesWithForce(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}})

	opts := initOptions{
		platform:    fakeProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok-first",
	}
	_, err := runInitInWorkspace(t, dir, opts)
	require.NoError(t, err)

	opts.accessToken = "tok-second"
	opts.force = true
	_, err = runInitInWorkspace(t, dir, opts)
	require.NoError(t, err)

	stored, err := credentials.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "tok-second", stored.AccessToken)

	// Re-running init must not duplicate the gitignore entries.
	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, 1, bytes.Count(gitignore, []byte(".mise/credentials\n")))
}

func TestRunInitRejectsUnknownPlatformBeforeAskingForSecrets(t *testing.T) {
	clearTokenEnv(t)
	dir := t.TempDir()
	resetFakeState(t, nil)

	_, err := runInitInWorkspace(t, dir, initOptions{
		platform:    "clover",
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "tok",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "clover")
	assert.Empty(t, fakeState.configuredToken)
}
