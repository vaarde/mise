//go:build integration

// Integration tests that run against a real Square sandbox account.
//
// They are behind the "integration" build tag and skip unless a sandbox
// token is present, so `go test ./...` never touches the network:
//
//	SQUARE_ACCESS_TOKEN=EAAAl... go test -tags integration ./cmd/ -run Integration -v
//
// These exercise the paths that unit tests cannot: Square's actual
// response shapes, the version tokens it hands back, whether a
// batch-upsert really is idempotent, and whether a second fetch really
// does produce byte-identical files.
package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/providers/square"
	"github.com/vaarde/mise/internal/state"
)

// requireSandbox returns a token for the Square sandbox, skipping the
// test when none is configured.
//
// It refuses a production token outright. These tests create, modify and
// delete catalog objects, and a mistyped environment variable must not
// be able to do that to a real restaurant's menu.
func requireSandbox(t *testing.T) string {
	t.Helper()

	token := os.Getenv("MISE_SQUARE_ACCESS_TOKEN")
	if token == "" {
		token = os.Getenv("SQUARE_ACCESS_TOKEN")
	}
	if token == "" {
		t.Skip("set SQUARE_ACCESS_TOKEN to a Square sandbox token to run integration tests")
	}

	// Square sandbox tokens are issued separately from production ones
	// and the sandbox host will reject a production token, but an
	// explicit opt-out is still required before anything is written.
	if os.Getenv("MISE_ALLOW_PRODUCTION") != "" {
		t.Fatal("integration tests write to the catalog and must only run against the sandbox")
	}

	return token
}

// sandboxClient builds a Square provider pointed at the sandbox, for the
// out-of-band reads and writes these tests use to simulate someone
// changing configuration outside of Mise.
func sandboxClient(t *testing.T, token string) provider.Provider {
	t.Helper()

	p, err := provider.Get(square.ProviderName)
	require.NoError(t, err)

	require.NoError(t, p.Configure(provider.ProviderConfig{
		Platform:    square.ProviderName,
		Environment: envSandbox,
		Credentials: provider.CredentialsConfig{
			Method:      credentials.MethodAccessToken,
			AccessToken: token,
		},
	}))

	return p
}

// testTaxName returns a name unique to this run, so repeated runs and
// concurrent runs never collide in a shared sandbox account.
func testTaxName() string {
	return fmt.Sprintf("Mise Integration Tax %d", time.Now().UnixNano())
}

// initSandboxWorkspace runs the real init against Square.
func initSandboxWorkspace(t *testing.T, dir, token string) {
	t.Helper()

	// The credentials file is the path under test; an inherited env var
	// would bypass it.
	t.Setenv("SQUARE_ACCESS_TOKEN", "")
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")

	out, err := runInitInWorkspace(t, dir, initOptions{
		platform:    square.ProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: token,
	})
	require.NoError(t, err, "init failed:\n%s", out)
	assert.Contains(t, out, "location")
}

func TestIntegrationInitVerifiesCredentialsAgainstSquare(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()

	initSandboxWorkspace(t, dir, token)

	// The workspace scaffolding, written for real rather than against a fake.
	assert.FileExists(t, filepath.Join(dir, "mise.yaml"))
	assert.FileExists(t, filepath.Join(dir, ".mise", "credentials"))

	raw, err := os.ReadFile(filepath.Join(dir, "mise.yaml"))
	require.NoError(t, err)
	assert.NotContains(t, string(raw), token,
		"mise.yaml records the auth method, never the token itself")
}

func TestIntegrationInitRejectsABadToken(t *testing.T) {
	requireSandbox(t)
	dir := t.TempDir()

	t.Setenv("SQUARE_ACCESS_TOKEN", "")
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")

	_, err := runInitInWorkspace(t, dir, initOptions{
		platform:    square.ProviderName,
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "EAAAnot-a-real-token",
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")

	// The hint is the whole point of catching this at init: a bare
	// "UNAUTHORIZED" leaves the operator guessing.
	assert.Contains(t, err.Error(), "mise init --force")

	assert.NoFileExists(t, filepath.Join(dir, ".mise", "credentials"),
		"a rejected token must not be written to disk")
}

func TestIntegrationFetchIsIdempotent(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	out, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err, "fetch failed:\n%s", out)

	first := snapshotWorkspace(t, dir)
	require.NotEmpty(t, first, "the sandbox account has no catalog to fetch — seed it first")

	out, err = runFetchInWorkspace(t, dir, true)
	require.NoError(t, err, "second fetch failed:\n%s", out)

	second := snapshotWorkspace(t, dir)

	// Byte-identical, not merely equivalent. Anything else shows up as
	// noise in every pull request the operator opens.
	assert.Equal(t, first, second,
		"fetching twice with nothing changed must produce identical files")
}

func TestIntegrationPlanIsCleanAfterFetch(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err, "plan failed:\n%s", out)

	// Whatever fetch wrote is by definition what is live. A plan that
	// finds changes here means fetch and plan disagree about the shape of
	// Square's data — the class of bug mocks cannot catch.
	assert.Contains(t, out, "No changes",
		"plan straight after fetch must be empty; got:\n%s", out)
}

func TestIntegrationApplyRoundTrip(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	displayName := testTaxName()
	resourceName := appendTax(t, dir, displayName, "7.25")

	// Plan sees exactly one create.
	out, err := runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err, "plan failed:\n%s", out)
	assert.Contains(t, out, resourceName)
	assert.Contains(t, out, "1 to create")

	// Apply it for real.
	out, err = runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err, "apply failed:\n%s", out)

	st, err := state.Load(dir)
	require.NoError(t, err)

	entry := st.Resources[state.ResourceKey(square.TypeTax, resourceName)]
	require.NotNil(t, entry, "apply must record what it created")
	require.NotEmpty(t, entry.ProviderID, "Square's assigned ID must land in state")

	// Whatever happens next, do not leave the object behind in a shared
	// sandbox account.
	t.Cleanup(func() {
		p := sandboxClient(t, token)
		if err := p.Delete(context.Background(), square.TypeTax, entry.ProviderID, ""); err != nil {
			t.Logf("could not clean up %s: %v", entry.ProviderID, err)
		}
	})

	// The tax really is on Square now, with the values that were declared.
	live, err := sandboxClient(t, token).Read(context.Background(), square.TypeTax, entry.ProviderID, "")
	require.NoError(t, err)
	assert.Equal(t, displayName, live.Name)
	assert.Equal(t, "7.25", live.Properties["percentage"])

	// And the loop closes: plan is empty again.
	out, err = runPlanInWorkspace(t, dir, "", "", "")
	require.NoError(t, err, "plan after apply failed:\n%s", out)
	assert.Contains(t, out, "No changes",
		"plan after apply must be empty; got:\n%s", out)
}

func TestIntegrationApplyIsIdempotentOnRerun(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	resourceName := appendTax(t, dir, testTaxName(), "3.5")

	_, err = runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	st, err := state.Load(dir)
	require.NoError(t, err)
	entry := st.Resources[state.ResourceKey(square.TypeTax, resourceName)]
	require.NotNil(t, entry)
	firstID := entry.ProviderID

	t.Cleanup(func() {
		p := sandboxClient(t, token)
		if err := p.Delete(context.Background(), square.TypeTax, firstID, ""); err != nil {
			t.Logf("could not clean up %s: %v", firstID, err)
		}
	})

	// Applying again must be a no-op rather than a second create. This is
	// what the deterministic idempotency key buys, and it can only be
	// checked against Square's real handling of it.
	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err, "second apply failed:\n%s", out)
	assert.Contains(t, out, "No changes")

	st, err = state.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, firstID, st.Resources[state.ResourceKey(square.TypeTax, resourceName)].ProviderID,
		"a re-run must not create a duplicate object")
}

func TestIntegrationDriftDetectsAnOutOfBandChange(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	displayName := testTaxName()
	resourceName := appendTax(t, dir, displayName, "5.0")

	_, err = runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	st, err := state.Load(dir)
	require.NoError(t, err)
	entry := st.Resources[state.ResourceKey(square.TypeTax, resourceName)]
	require.NotNil(t, entry)

	t.Cleanup(func() {
		p := sandboxClient(t, token)
		if err := p.Delete(context.Background(), square.TypeTax, entry.ProviderID, ""); err != nil {
			t.Logf("could not clean up %s: %v", entry.ProviderID, err)
		}
	})

	// Clean immediately after an apply.
	out, err := runDriftInWorkspace(t, dir, "", "", false)
	require.NoError(t, err, "drift right after apply should be clean; got:\n%s", out)
	assert.Contains(t, out, "No drift")

	// Now change it behind Mise's back, the way someone editing the
	// Square dashboard would.
	p := sandboxClient(t, token)
	live, err := p.Read(context.Background(), square.TypeTax, entry.ProviderID, "")
	require.NoError(t, err)

	live.Properties["percentage"] = "9.99"
	require.NoError(t, p.Update(context.Background(), square.TypeTax, entry.ProviderID, live, ""))

	out, err = runDriftInWorkspace(t, dir, "", "", false)

	require.Error(t, err, "drift must report a non-zero status when it finds drift")
	assert.Equal(t, ExitCodeDrift, ExitCode(err),
		"a scheduled check distinguishes drift from failure by the exit code alone")

	assert.Contains(t, out, "Drift detected")
	assert.Contains(t, out, resourceName)
	assert.Contains(t, out, "9.99")
	assert.Contains(t, out, "Changed outside of Mise")
}

func TestIntegrationDriftIsReadOnly(t *testing.T) {
	token := requireSandbox(t)
	dir := t.TempDir()
	initSandboxWorkspace(t, dir, token)

	_, err := runFetchInWorkspace(t, dir, false)
	require.NoError(t, err)

	before, err := os.ReadFile(filepath.Join(dir, ".mise", "state.json"))
	require.NoError(t, err)

	_, _ = runDriftInWorkspace(t, dir, "", "", false)

	after, err := os.ReadFile(filepath.Join(dir, ".mise", "state.json"))
	require.NoError(t, err)

	// A drift report is evidence of a discrepancy, not permission to
	// accept it. Accepting means running fetch.
	assert.Equal(t, before, after, "drift must never rewrite state")
}

func TestIntegrationRateLimiterSurvivesABurst(t *testing.T) {
	token := requireSandbox(t)
	p := sandboxClient(t, token)

	// Square's ceiling is around 40 req/s and the client is set to 30.
	// This is the only way to find out whether that holds against the
	// real endpoint, including its 429s and Retry-After headers.
	for i := 0; i < 60; i++ {
		_, err := p.ListLocations(context.Background())
		require.NoError(t, err, "request %d of a sustained burst failed", i)
	}
}

// appendTax adds a tax to taxes.yaml and returns the config name it was
// given. It writes the file from scratch when the account had no taxes
// for fetch to write.
func appendTax(t *testing.T, dir, displayName, percentage string) string {
	t.Helper()

	resourceName := slugForTest(displayName)
	path := filepath.Join(dir, "taxes.yaml")

	block := fmt.Sprintf(`  - type: %s
    name: %s
    locations: ${group.all}
    properties:
      name: %s
      percentage: "%s"
      calculation_phase: TAX_SUBTOTAL_PHASE
      inclusion_type: ADDITIVE
      enabled: true
`, square.TypeTax, resourceName, displayName, percentage)

	existing, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		require.NoError(t, os.WriteFile(path, []byte("resources:\n"+block), 0o644))
		return resourceName
	}
	require.NoError(t, err)

	content := string(existing)
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(content+block), 0o644))

	return resourceName
}

// slugForTest mirrors the engine's naming so the test can predict the
// config name it is about to declare.
func slugForTest(displayName string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(displayName) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// snapshotWorkspace reads every generated YAML file, so two fetches can
// be compared byte for byte.
func snapshotWorkspace(t *testing.T, dir string) map[string]string {
	t.Helper()

	files := map[string]string{}

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		// mise.yaml is written by init, not fetch.
		if filepath.Base(path) == "mise.yaml" {
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
