package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
)

// scriptedPrompter returns a prompter fed by a fixed input string, so
// tests can drive interactive paths without a terminal.
func scriptedPrompter(input string) (*prompter, *bytes.Buffer) {
	var out bytes.Buffer
	return &prompter{
		in:          bufio.NewReader(strings.NewReader(input)),
		out:         &out,
		interactive: input != "",
	}, &out
}

// clearTokenEnv removes any ambient Square token so tests are not
// influenced by the developer's own shell.
func clearTokenEnv(t *testing.T) {
	t.Helper()
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")
	t.Setenv("SQUARE_ACCESS_TOKEN", "")
	t.Setenv(envVarAppID, "")
	t.Setenv(envVarAppSecret, "")
}

func TestResolveInitOptionsFromFlags(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{
		platform:    "square",
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "sq0atp-token",
	}

	require.NoError(t, resolveInitOptions(&opts, p))
	assert.Equal(t, "square", opts.platform)
	assert.Equal(t, envSandbox, opts.environment)
	assert.Equal(t, credentials.MethodAccessToken, opts.authMethod)
	assert.Equal(t, 8666, opts.callbackPort, "an unset callback port should get the default")
}

func TestResolveInitOptionsPromptsWhenSeveralPlatformsAreRegistered(t *testing.T) {
	clearTokenEnv(t)
	// The test binary registers a fake adapter alongside Square, so the
	// operator has a choice to make.
	require.Greater(t, len(provider.AvailableProviders()), 1)

	p, out := scriptedPrompter("square\n")

	opts := initOptions{environment: envSandbox, accessToken: "tok"}
	require.NoError(t, resolveInitOptions(&opts, p))
	assert.Equal(t, "square", opts.platform)
	assert.Contains(t, out.String(), "Which POS platform?")
}

func TestResolveInitOptionsInfersAccessTokenMethodFromEnv(t *testing.T) {
	clearTokenEnv(t)
	t.Setenv("SQUARE_ACCESS_TOKEN", "sq0atp-from-env")
	p, out := scriptedPrompter("")

	opts := initOptions{platform: "square", environment: envSandbox}
	require.NoError(t, resolveInitOptions(&opts, p))

	assert.Equal(t, credentials.MethodAccessToken, opts.authMethod)
	assert.Equal(t, "sq0atp-from-env", opts.accessToken)
	assert.Contains(t, out.String(), "SQUARE_ACCESS_TOKEN")
}

func TestResolveInitOptionsInfersMethodFromTokenFlag(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{platform: "square", environment: envSandbox, accessToken: "sq0atp-flag"}
	require.NoError(t, resolveInitOptions(&opts, p))
	assert.Equal(t, credentials.MethodAccessToken, opts.authMethod)
}

func TestResolveInitOptionsRejectsUnknownPlatform(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{platform: "toast", environment: envSandbox, accessToken: "tok"}
	err := resolveInitOptions(&opts, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "toast")
}

func TestResolveInitOptionsRejectsUnknownEnvironment(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{platform: "square", environment: "staging", accessToken: "tok"}
	err := resolveInitOptions(&opts, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging")
}

func TestResolveInitOptionsRejectsUnknownAuthMethod(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{platform: "square", environment: envSandbox, authMethod: "magic-link"}
	err := resolveInitOptions(&opts, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic-link")
}

func TestResolveInitOptionsFailsLoudlyWithoutATerminal(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("") // not interactive

	opts := initOptions{platform: "square"} // no environment, nothing to infer
	err := resolveInitOptions(&opts, p)
	require.ErrorIs(t, err, errNotInteractive)
}

func TestResolveInitOptionsPromptsForEnvironment(t *testing.T) {
	clearTokenEnv(t)
	p, out := scriptedPrompter("2\n") // choose "production"

	opts := initOptions{platform: "square", authMethod: credentials.MethodAccessToken, accessToken: "tok"}
	require.NoError(t, resolveInitOptions(&opts, p))

	assert.Equal(t, envProduction, opts.environment)
	assert.Contains(t, out.String(), "Which environment?")
}

func TestAuthenticateWithTokenFromFlag(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	creds, err := authenticateWithToken(p, initOptions{
		platform:    "square",
		environment: envSandbox,
		accessToken: "  sq0atp-token  ",
	})
	require.NoError(t, err)

	assert.Equal(t, "sq0atp-token", creds.AccessToken, "surrounding whitespace should be trimmed")
	assert.Equal(t, credentials.MethodAccessToken, creds.Method)
	assert.Equal(t, "square", creds.Provider)
	assert.Equal(t, envSandbox, creds.Environment)
	assert.NoError(t, creds.Validate())
}

func TestAuthenticateWithTokenFromEnv(t *testing.T) {
	clearTokenEnv(t)
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "sq0atp-from-env")
	p, out := scriptedPrompter("")

	creds, err := authenticateWithToken(p, initOptions{platform: "square", environment: envSandbox})
	require.NoError(t, err)
	assert.Equal(t, "sq0atp-from-env", creds.AccessToken)
	assert.Contains(t, out.String(), "MISE_SQUARE_ACCESS_TOKEN")
}

func TestAuthenticateWithTokenGivesActionableErrorWithoutATerminal(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	_, err := authenticateWithToken(p, initOptions{platform: "square", environment: envSandbox})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--token")
	assert.Contains(t, err.Error(), "SQUARE_ACCESS_TOKEN")
}

func TestCheckExistingWorkspaceAllowsFreshDirectory(t *testing.T) {
	dir := t.TempDir()
	p, _ := scriptedPrompter("")

	require.NoError(t, checkExistingWorkspace(&bytes.Buffer{}, p, filepath.Join(dir, "mise.yaml"), dir, false))
}

func TestCheckExistingWorkspaceRefusesToClobber(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mise.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: \"1\"\n"), 0o644))

	p, _ := scriptedPrompter("") // not interactive
	err := checkExistingWorkspace(&bytes.Buffer{}, p, configPath, dir, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force")
}

func TestCheckExistingWorkspaceHonoursForce(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mise.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: \"1\"\n"), 0o644))

	p, _ := scriptedPrompter("")
	require.NoError(t, checkExistingWorkspace(&bytes.Buffer{}, p, configPath, dir, true))
}

func TestCheckExistingWorkspaceCancelsOnDecline(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mise.yaml")
	require.NoError(t, os.WriteFile(configPath, []byte("version: \"1\"\n"), 0o644))

	p, _ := scriptedPrompter("n\n")
	err := checkExistingWorkspace(&bytes.Buffer{}, p, configPath, dir, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestCheckExistingWorkspaceDetectsCredentialsWithoutConfig(t *testing.T) {
	dir := t.TempDir()
	creds := &credentials.Credentials{Provider: "square", Method: credentials.MethodAccessToken, AccessToken: "tok"}
	require.NoError(t, creds.Save(dir))

	p, _ := scriptedPrompter("")
	err := checkExistingWorkspace(&bytes.Buffer{}, p, filepath.Join(dir, "mise.yaml"), dir, false)

	require.Error(t, err)
	assert.Contains(t, err.Error(), credentials.FileName)
}

func TestWriteWorkspaceLaysDownEveryFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mise.yaml")
	var out bytes.Buffer

	opts := initOptions{platform: "square", environment: envSandbox, authMethod: credentials.MethodOAuth2}
	creds := &credentials.Credentials{
		Provider:     "square",
		Method:       credentials.MethodOAuth2,
		Environment:  envSandbox,
		AccessToken:  "sq0atp-token",
		RefreshToken: "sq0rtp-refresh",
	}
	locations := []provider.Location{{ID: "LOC_ATL", Name: "Atlanta", State: "GA"}}

	require.NoError(t, writeWorkspace(&out, configPath, dir, opts, creds, locations))

	// mise.yaml is written and loadable.
	cfg, err := config.LoadRoot(configPath)
	require.NoError(t, err)
	assert.Equal(t, credentials.MethodOAuth2, cfg.Provider.Credentials.Method)
	assert.Empty(t, cfg.Provider.Credentials.AccessToken, "the token must not round-trip through mise.yaml")

	// Credentials are stored separately and hold the real secret.
	stored, err := credentials.Load(dir)
	require.NoError(t, err)
	assert.Equal(t, "sq0atp-token", stored.AccessToken)
	assert.Equal(t, "sq0rtp-refresh", stored.RefreshToken)

	// The secret file is gitignored before the operator can commit it.
	gitignore, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Contains(t, string(gitignore), ".mise/credentials")
	assert.Contains(t, string(gitignore), ".mise/state.json")

	assert.Contains(t, out.String(), "never commit this")
}

func TestWriteWorkspaceUsesFirstLocationInTheExampleGroup(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "mise.yaml")

	creds := &credentials.Credentials{Provider: "square", Method: credentials.MethodAccessToken, AccessToken: "tok"}
	locations := []provider.Location{{ID: "LOC_REAL", Name: "Atlanta"}}
	opts := initOptions{platform: "square", environment: envSandbox, authMethod: credentials.MethodAccessToken}

	require.NoError(t, writeWorkspace(&bytes.Buffer{}, configPath, dir, opts, creds, locations))

	raw, err := os.ReadFile(configPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "LOC_REAL")
}

func TestPrintNextSteps(t *testing.T) {
	var out bytes.Buffer
	opts := initOptions{platform: "square", environment: envSandbox}

	printNextSteps(&out, opts, []provider.Location{{ID: "A"}, {ID: "B"}})
	assert.Contains(t, out.String(), "2 locations")
	assert.Contains(t, out.String(), "mise fetch")

	out.Reset()
	printNextSteps(&out, opts, nil)
	assert.Contains(t, out.String(), "mise fetch")
}

func TestPluralize(t *testing.T) {
	assert.Equal(t, "1 location", pluralize(1, "location", "locations"))
	assert.Equal(t, "0 locations", pluralize(0, "location", "locations"))
	assert.Equal(t, "3 locations", pluralize(3, "location", "locations"))
}

func TestFirstNonEmpty(t *testing.T) {
	assert.Equal(t, "b", firstNonEmpty("", "  ", " b ", "c"))
	assert.Equal(t, "", firstNonEmpty("", "   "))
}

func TestResolveInitOptionsRejectsTokenWithOAuth2(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{
		platform:    "square",
		environment: envSandbox,
		authMethod:  credentials.MethodOAuth2,
		accessToken: "sq0atp-token",
	}
	err := resolveInitOptions(&opts, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--token")
}

func TestResolveInitOptionsRejectsApplicationCredentialsWithAccessToken(t *testing.T) {
	clearTokenEnv(t)
	p, _ := scriptedPrompter("")

	opts := initOptions{
		platform:    "square",
		environment: envSandbox,
		authMethod:  credentials.MethodAccessToken,
		accessToken: "sq0atp-token",
		clientID:    "sq0idp-app",
	}
	err := resolveInitOptions(&opts, p)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--client-id")
}
