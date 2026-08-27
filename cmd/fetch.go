package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/engine"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Pull current POS configuration into YAML files",
	Long: `Connects to your POS platform, reads the live configuration for
all locations, and generates YAML config files representing every
resource (menus, tax rates, discounts, modifiers).

This is the onboarding command — you don't have to write config
files from scratch. Run fetch, and your entire current setup
becomes a versioned, human-readable file set you can commit to git.

Fetch overwrites the config files it generates, so it prompts before
replacing existing ones. It never touches mise.yaml.`,
	RunE: runFetch,
}

// Flags for fetch
var (
	fetchForce       bool
	fetchParallelism int
)

func init() {
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "overwrite existing config files without prompting")
	fetchCmd.Flags().IntVar(&fetchParallelism, "parallelism", engine.DefaultParallelism,
		"maximum locations to read concurrently")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()

	ws, err := loadWorkspace(ctx, out, configFile)
	if err != nil {
		return err
	}

	if err := confirmOverwrite(out, ws, fetchForce); err != nil {
		return err
	}

	fmt.Fprintf(out, "Fetching live configuration from %s (%s)...\n",
		ws.Config.Provider.Platform, environmentLabel(ws.Config.Provider.Environment))

	result, err := engine.Fetch(ctx, ws.Provider, engine.FetchOptions{
		Parallelism: fetchParallelism,
		// Locations arrive through ListLocations, which every provider
		// implements; reading them again as a resource type would be
		// duplicate work and a duplicate file.
		SkipTypes: []string{ws.Config.Provider.Platform + "_location"},
	})
	if err != nil {
		return err
	}

	written, err := writeFetchedFiles(ws, result)
	if err != nil {
		return err
	}

	printFetchSummary(out, result, written)
	return nil
}

// confirmOverwrite warns before replacing generated config files. Fetch
// regenerates from the live POS, so any local edits to those files are
// lost — that is worth an explicit yes.
func confirmOverwrite(out io.Writer, ws *workspace, force bool) error {
	existing := config.ExistingGeneratedFiles(ws.Dir, ws.Provider.ResourceTypes())
	if len(existing) == 0 || force {
		return nil
	}

	fmt.Fprintf(out, "This will overwrite existing config files:\n")
	for _, path := range existing {
		fmt.Fprintf(out, "  %s\n", path)
	}

	prompt := newPrompter(out)
	proceed, err := prompt.confirm("Proceed?", false)
	if errors.Is(err, errNotInteractive) {
		return fmt.Errorf("fetch would overwrite %s — pass --force to confirm",
			strings.Join(existing, ", "))
	}
	if err != nil {
		return err
	}
	if !proceed {
		return fmt.Errorf("fetch cancelled — no files were changed")
	}
	return nil
}

// writeFetchedFiles writes the config files and the state file.
func writeFetchedFiles(ws *workspace, result *engine.FetchResult) ([]string, error) {
	locationsFile, err := config.GenerateLocationsFile(ws.Dir, result.Locations)
	if err != nil {
		return nil, err
	}

	resourceFiles, err := config.GenerateResourceFiles(ws.Dir, result.ToResourceDefs())
	if err != nil {
		return nil, err
	}

	// State is written last: it records what Mise believes is on disk,
	// so it should only claim a fetch happened once the files exist.
	if err := result.ToState(ws.Provider.Name(), time.Now().UTC()).Save(ws.Dir); err != nil {
		return nil, err
	}

	return append([]string{locationsFile}, resourceFiles...), nil
}

// printFetchSummary reports what was fetched, per the PRD's
// "Fetched 847 resources across 15 locations."
func printFetchSummary(out io.Writer, result *engine.FetchResult, written []string) {
	for _, warning := range result.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning)
	}

	fmt.Fprintf(out, "\nFetched %s across %s.\n",
		pluralize(result.Total(), "resource", "resources"),
		pluralize(len(result.Locations), "location", "locations"))

	counts := result.CountsByType()
	for _, resourceType := range sortedKeys(counts) {
		fmt.Fprintf(out, "  %-32s %d\n", resourceType, counts[resourceType])
	}

	fmt.Fprintf(out, "\nWrote:\n")
	for _, path := range written {
		fmt.Fprintf(out, "  %s\n", path)
	}
	fmt.Fprintf(out, "  .mise/state.json\n")

	fmt.Fprintf(out, "\nCommit these files to git, then run 'mise plan' after editing them.\n")
}

// environmentLabel renders an unset environment as its effective value.
func environmentLabel(environment string) string {
	if environment == "" {
		return envProduction
	}
	return environment
}

// sortedKeys returns a map's keys in sorted order.
func sortedKeys(counts map[string]int) []string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
