package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/state"
	"github.com/vaarde/mise/pkg/output"
)

var applyCmd = &cobra.Command{
	Use:   "apply",
	Short: "Apply configuration changes to the POS",
	Long: `Executes the changes shown by 'mise plan' against your POS platform.

Shows the plan, asks for confirmation, then creates and updates
resources in dependency order — tax rates before the menu items that
charge them — and records the result in the state file.

Mise never deletes POS resources. A resource removed from your config
files is left alone on the platform.

If part of an apply fails, the resources that succeeded are still
recorded in state, so re-running only attempts what is left.`,
	RunE: runApply,
}

// Flags for apply
var (
	applyAutoApprove bool
	applyPlanFile    string
	applyTarget      string
	applyLocation    string
	applyParallelism int
)

func init() {
	applyCmd.Flags().BoolVar(&applyAutoApprove, "auto-approve", false, "skip confirmation prompt (for CI/CD)")
	applyCmd.Flags().StringVar(&applyPlanFile, "plan", "", "apply a previously saved plan file")
	applyCmd.Flags().StringVar(&applyTarget, "target", "", "apply only a specific resource")
	applyCmd.Flags().StringVar(&applyLocation, "location", "", "apply only for a specific location")
	applyCmd.Flags().IntVar(&applyParallelism, "parallelism", engine.DefaultParallelism,
		"max concurrent API calls")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()

	ws, err := loadWorkspace(ctx, out, configFile)
	if err != nil {
		return err
	}

	// One apply at a time: two concurrent runs would leave the state file
	// describing neither of them.
	lock, err := state.Acquire(ws.Dir, "apply")
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			fmt.Fprintf(out, "Warning: %v\n", releaseErr)
		}
	}()

	plan, err := resolveApplyPlan(ctx, out)
	if err != nil {
		return err
	}

	printer := output.NewPlanPrinter(out, plan.Locations)
	printer.Print(plan)

	if !plan.HasChanges() {
		return nil
	}

	approved, err := confirmApply(out, applyAutoApprove)
	if err != nil {
		return err
	}
	if !approved {
		fmt.Fprintln(out, "\nApply cancelled. Nothing was changed.")
		return nil
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "\nApplying changes...\n")

	result, applyErr := engine.Apply(ctx, ws.Provider, plan, st, engine.ApplyOptions{
		Parallelism: applyParallelism,
	})

	// State is saved even when the apply failed part-way: the resources
	// that did land must be recorded, or the next plan would create them
	// a second time.
	if saveErr := st.Save(ws.Dir); saveErr != nil {
		if applyErr != nil {
			return fmt.Errorf("%w (and the state file could not be saved: %v)", applyErr, saveErr)
		}
		return saveErr
	}

	output.PrintApplyResult(out, result)

	if applyErr != nil {
		return applyErr
	}
	return nil
}

// resolveApplyPlan either reads a saved plan or computes a fresh one.
func resolveApplyPlan(ctx context.Context, out io.Writer) (*engine.PlanResult, error) {
	if applyPlanFile != "" {
		fmt.Fprintf(out, "Applying saved plan %s\n", applyPlanFile)
		return LoadPlan(applyPlanFile)
	}

	return computePlan(ctx, out, planOptions{
		target:      applyTarget,
		location:    applyLocation,
		parallelism: applyParallelism,
	})
}

// confirmApply asks before making changes. The safe answer is the
// default, matching the PRD's "[y/N]".
func confirmApply(out io.Writer, autoApprove bool) (bool, error) {
	if autoApprove {
		return true, nil
	}

	prompt := newPrompter(out)
	fmt.Fprintln(out)

	approved, err := prompt.confirm("Do you want to apply these changes?", false)
	if errors.Is(err, errNotInteractive) {
		return false, fmt.Errorf("apply needs confirmation but there is no terminal — pass --auto-approve to run unattended")
	}
	if err != nil {
		return false, err
	}
	return approved, nil
}
