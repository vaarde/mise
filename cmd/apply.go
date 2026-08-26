package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration changes to the POS",
	Long: `Executes the changes described by a plan — creating, updating,
or modifying resources on the POS platform.

Mise computes a plan, shows you exactly what will change, and
asks for confirmation before making any API calls. Changes are
applied in dependency order and parallelized across locations.`,
	RunE: runApply,
}

// Flags for apply
var (
	applyAutoApprove bool
	applyPlanFile    string
	applyTarget      string
	applyParallelism int
)

func init() {
	applyCmd.Flags().BoolVar(&applyAutoApprove, "auto-approve", false, "skip confirmation prompt (for CI/CD)")
	applyCmd.Flags().StringVar(&applyPlanFile, "plan", "", "apply a previously saved plan file")
	applyCmd.Flags().StringVar(&applyTarget, "target", "", "apply only a specific resource")
	applyCmd.Flags().IntVar(&applyParallelism, "parallelism", 10, "max concurrent API calls")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	fmt.Println("Computing plan...")

	// TODO: Milestone 4 implementation
	// 1. Compute plan (or load from --plan file)
	// 2. Print plan summary
	// 3. Prompt for confirmation (unless --auto-approve)
	// 4. Acquire state lock (.mise/lock)
	// 5. Execute changes via provider in dependency order:
	//    a. Resolve DAG for creation order
	//    b. Parallelize across locations (bounded by --parallelism)
	//    c. Use Square batch API where possible
	//    d. Generate idempotency keys per resource
	// 6. Update state file with new provider IDs
	// 7. Release state lock
	// 8. Print results (successes + any failures)

	fmt.Println("\nApply complete. 0 added, 0 changed, 0 destroyed.")
	return nil
}
