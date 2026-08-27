package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
)

func TestWriteRootConfigProducesLoadableYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mise.yaml")

	require.NoError(t, writeRootConfig(path, "square", "sandbox", "oauth2", "LOC_ATL"))

	// The generated file must parse with the same loader the rest of
	// Mise uses — a template that only looks right is not enough.
	cfg, err := config.LoadRoot(path)
	require.NoError(t, err)

	assert.Equal(t, "1", cfg.Version)
	assert.Equal(t, "square", cfg.Provider.Platform)
	assert.Equal(t, "sandbox", cfg.Provider.Environment)
	assert.Equal(t, "oauth2", cfg.Provider.Credentials.Method)
	require.Contains(t, cfg.LocationGroups, "all")
	assert.Equal(t, "*", cfg.LocationGroups["all"].Filter)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "LOC_ATL", "the commented example should use a real location ID")
	assert.NotContains(t, string(raw), "access_token:", "mise.yaml must never carry a secret")
}

func TestWriteRootConfigFallsBackToPlaceholderLocationID(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mise.yaml")

	require.NoError(t, writeRootConfig(path, "square", "production", "access_token", ""))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "loc_abc123")
}

func TestWriteRootConfigCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "workspace", "mise.yaml")

	require.NoError(t, writeRootConfig(path, "square", "sandbox", "oauth2", ""))
	assert.FileExists(t, path)
}

// unmarshalGeneratedConfig proves the commented examples in the
// template stay comments and do not become active configuration.
func TestGeneratedConfigHasOnlyTheAllGroup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mise.yaml")
	require.NoError(t, writeRootConfig(path, "square", "sandbox", "oauth2", ""))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)

	var parsed struct {
		LocationGroups map[string]interface{} `yaml:"location_groups"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &parsed))
	assert.Len(t, parsed.LocationGroups, 1)
}

func TestEnsureStateDir(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, ensureStateDir(dir))

	info, err := os.Stat(filepath.Join(dir, credentials.Dir))
	require.NoError(t, err)
	assert.True(t, info.IsDir())

	// Running init twice must not fail on an existing directory.
	require.NoError(t, ensureStateDir(dir))
}

func TestEnsureGitignoreCreatesFile(t *testing.T) {
	dir := t.TempDir()

	added, err := ensureGitignore(dir)
	require.NoError(t, err)
	assert.ElementsMatch(t, gitignoreEntries, added)

	raw, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	for _, entry := range gitignoreEntries {
		assert.Contains(t, string(raw), entry)
	}
}

func TestEnsureGitignoreIsIdempotent(t *testing.T) {
	dir := t.TempDir()

	_, err := ensureGitignore(dir)
	require.NoError(t, err)
	first, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)

	added, err := ensureGitignore(dir)
	require.NoError(t, err)
	assert.Empty(t, added, "a second run should have nothing to add")

	second, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	require.NoError(t, err)
	assert.Equal(t, string(first), string(second))
}

func TestEnsureGitignorePreservesExistingContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	existing := "node_modules/\n.mise/credentials\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	added, err := ensureGitignore(dir)
	require.NoError(t, err)
	assert.NotContains(t, added, ".mise/credentials", "an entry already present should not be duplicated")

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(raw)

	assert.True(t, strings.HasPrefix(content, existing), "existing entries must be left untouched")
	assert.Equal(t, 1, strings.Count(content, ".mise/credentials\n"))
	assert.Contains(t, content, ".mise/state.json")
	assert.Contains(t, content, ".mise/lock")
}

func TestEnsureGitignoreHandlesMissingTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gitignore")
	require.NoError(t, os.WriteFile(path, []byte("bin/"), 0o644))

	_, err := ensureGitignore(dir)
	require.NoError(t, err)

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	assert.Equal(t, "bin/", strings.TrimSpace(lines[0]),
		"an unterminated last line must not be joined to a Mise entry")
	assert.Contains(t, string(raw), "\n.mise/state.json")
}
