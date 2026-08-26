package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var fetchCmd = &cobra.Command{
	Use:   "fetch",
	Short: "Pull current POS configuration into YAML files",
	Long: `Connects to your POS platform, reads the live configuration for
all locations, and generates YAML config files representing every
resource (menus, tax rates, discounts, modifiers).

This is the onboarding command — you don't have to write config
files from scratch. Run fetch, and your entire current setup
becomes a versioned, human-readable file set you can commit to git.`,
	RunE: runFetch,
}

// Flags for fetch
var fetchForce bool

func init() {
	fetchCmd.Flags().BoolVar(&fetchForce, "force", false, "overwrite existing config files without prompting")
	rootCmd.AddCommand(fetchCmd)
}

func runFetch(cmd *cobra.Command, args []string) error {
	fmt.Println("Fetching live configuration from POS...")

	// TODO: Milestone 2 implementation
	// 1. Load mise.yaml for provider config
	// 2. Initialize provider (Square adapter)
	// 3. Call provider.ListLocations() to discover all locations
	// 4. For each location (in parallel):
	//    a. Call provider.ReadAll() for each resource type
	// 5. Group resources by type and generate YAML files
	// 6. Write initial state file (.mise/state.json)
	// 7. Print summary

	return nil
}
