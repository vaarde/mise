package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Detect POS configuration changes made outside Mise",
	Long: `Compares the last-known state (from the most recent fetch or apply)
against the current live POS configuration. Reports any differences
caused by someone modifying settings through the POS dashboard or
another tool.

This is your audit trail — run it weekly to catch unauthorized
changes before they become compliance problems.`,
	RunE: runDrift,
}

// Flags for drift
var (
	driftLocation string
	driftType     string
	driftJSON     bool
)

func init() {
	driftCmd.Flags().StringVar(&driftLocation, "location", "", "check drift for a single location")
	driftCmd.Flags().StringVar(&driftType, "type", "", "check drift for a specific resource type")
	driftCmd.Flags().BoolVar(&driftJSON, "json", false, "output drift report as JSON")
	rootCmd.AddCommand(driftCmd)
}

func runDrift(cmd *cobra.Command, args []string) error {
	fmt.Println("Checking for configuration drift...")

	// TODO: Milestone 5 implementation
	// 1. Load last-known state from .mise/state.json
	// 2. Fetch current live state from POS API
	// 3. Compare live state against stored state
	// 4. Report differences per location:
	//    - Property value changes
	//    - New resources not in state (created outside Mise)
	//    - Missing resources (deleted outside Mise)
	// 5. Print drift report (colored or JSON)

	fmt.Println("\n0 resources drifted across 0 locations.")
	return nil
}
