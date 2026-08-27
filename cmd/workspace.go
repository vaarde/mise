package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/vaarde/mise/internal/credentials"
)

// gitignoreEntries are the paths a Mise workspace must never commit.
// The state file holds provider IDs, the credentials file holds a live
// API token, and the lock file is per-machine.
var gitignoreEntries = []string{
	".mise/state.json",
	".mise/credentials",
	".mise/lock",
}

// rootConfigTemplate is the mise.yaml Mise writes on init.
//
// It is a commented template rather than a marshaled struct because
// this file is the operator's entry point — the comments explain
// location groups, which is the concept most new users need first.
const rootConfigTemplate = `# mise.yaml — Mise workspace configuration
#
# Secrets are never stored here. Credentials live in .mise/credentials
# (gitignored, permissions 0600). Commit this file; it is the shared
# definition of how your team connects to the POS.

version: "1"

provider:
  platform: %s
  environment: %s
  credentials:
    method: %s

# Location groups name a set of locations so resources can target them:
#
#   locations: ${group.georgia}
#
# The "all" group matches every location on the account. Add your own
# groups by filtering on location metadata (state, or explicit IDs).
location_groups:
  all:
    filter: "*"

  # georgia:
  #   filter:
  #     state: "GA"
  #
  # west_coast:
  #   filter:
  #     state: ["CA", "OR", "WA"]
  #
  # flagship:
  #   filter:
  #     ids: ["%s"]
`

// writeRootConfig renders mise.yaml for a new workspace.
func writeRootConfig(path, platform, environment, authMethod, exampleLocationID string) error {
	if exampleLocationID == "" {
		exampleLocationID = "loc_abc123"
	}

	content := fmt.Sprintf(rootConfigTemplate, platform, environment, authMethod, exampleLocationID)

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create workspace directory: %w", err)
		}
	}

	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	return nil
}

// ensureStateDir creates .mise/ with owner-only permissions. It holds
// the state file and the credentials file.
func ensureStateDir(workDir string) error {
	dir := filepath.Join(workDir, credentials.Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s directory: %w", credentials.Dir, err)
	}
	return nil
}

// ensureGitignore appends any missing Mise entries to the workspace
// .gitignore, creating the file if it does not exist. Entries already
// present are left alone, and the rest of the file is never rewritten.
//
// It returns the entries it added.
func ensureGitignore(workDir string) ([]string, error) {
	path := filepath.Join(workDir, ".gitignore")

	existing := map[string]bool{}
	endsWithNewline := true

	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		for _, line := range strings.Split(string(data), "\n") {
			existing[strings.TrimSpace(line)] = true
		}
		endsWithNewline = len(data) == 0 || strings.HasSuffix(string(data), "\n")
	case os.IsNotExist(err):
		// No .gitignore yet — it gets created below.
	default:
		return nil, fmt.Errorf("cannot read .gitignore: %w", err)
	}

	var missing []string
	for _, entry := range gitignoreEntries {
		if !existing[entry] {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return nil, nil
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot update .gitignore: %w", err)
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	if !endsWithNewline {
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "# Mise state and credentials (never commit these)")
	for _, entry := range missing {
		fmt.Fprintln(w, entry)
	}
	if err := w.Flush(); err != nil {
		return nil, fmt.Errorf("cannot update .gitignore: %w", err)
	}

	return missing, nil
}
