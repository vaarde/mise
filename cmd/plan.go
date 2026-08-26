package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview changes between config files and live POS state",
	Long: `Compares your declared YAML configuration against the live POS
state and shows what would change — without making any modifications.

Think of it like a dry run: you see exactly what Mise would create,
update, or flag before committing to the change.`,
	RunE: runPlan,
}

// Flags for plan
var (
	planTarget   string
	planLocation string
	planOutFile  string
)

func init() {
	planCmd.Flags().StringVar(&planTarget, "target", "", "plan only for a specific resource name")
	planCmd.Flags().StringVar(&planLocation, "location", "", "plan only for a specific location")
	planCmd.Flags().StringVarP(&planOutFile, "out", "o", "", "save plan to file for later apply")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	fmt.Println("Computing plan...")

	// TODO: Milestone 3 implementation
	// 1. Load declared state from YAML config files
	// 2. Load live state via provider.ReadAll()
	// 3. Compute diff (declared vs live)
	//    - In config but not live → create
	//    - In both but different properties → update
	//    - In live but not config → no action (safety default)
	// 4. Resolve ref() dependencies and build DAG
	// 5. Print colored plan output
	// 6. Optionally write plan to file

	fmt.Println("\nPlan: 0 to add, 0 to change, 0 to destroy.")
	return nil
}
