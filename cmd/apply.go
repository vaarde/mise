package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/config"
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

	plan, err := resolveApplyPlan(ctx, out, ws)
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

	// The identity is checked before the plan is computed too, but a
	// saved plan skips that path — and this is the last point before
	// anything is written.
	if err := ws.CheckStateIdentity(ctx, st); err != nil {
		return err
	}

	// State records which account it describes. Stamping it here means a
	// workspace built by an older Mise gains the protection on its first
	// apply rather than waiting for the next fetch.
	identity, err := ws.Identity(ctx)
	if err != nil {
		return err
	}
	st.SetIdentity(identity)

	fmt.Fprintf(out, "\nApplying changes...\n")

	result, applyErr := engine.Apply(ctx, ws.Provider, plan, st, engine.ApplyOptions{
		Parallelism: applyParallelism,

		// Each wave is written to disk as it completes. A crash then
		// costs the record of one wave rather than the whole run.
		Checkpoint: func(s *state.State) error { return s.Save(ws.Dir) },
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
func resolveApplyPlan(ctx context.Context, out io.Writer, ws *workspace) (*engine.PlanResult, error) {
	if applyPlanFile != "" {
		fmt.Fprintf(out, "Applying saved plan %s\n", applyPlanFile)
		return loadVerifiedPlan(ctx, ws, applyPlanFile)
	}

	plan, _, err := computePlan(ctx, ws, planOptions{
		target:      applyTarget,
		location:    applyLocation,
		parallelism: applyParallelism,
	})
	return plan, err
}

// loadVerifiedPlan reads a saved plan and refuses it unless it was built
// for this workspace, against this state, from this configuration.
//
// A plan file is a list of provider IDs and version tokens. Executed
// against the account it was made for, it does what it previewed;
// executed anywhere else, it is a set of instructions about objects that
// do not exist there, and apply would dutifully re-create all of them.
func loadVerifiedPlan(ctx context.Context, ws *workspace, path string) (*engine.PlanResult, error) {
	saved, err := LoadPlan(path)
	if err != nil {
		return nil, err
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return nil, err
	}

	// A config that no longer parses is reported as a config error
	// rather than silently skipping the digest comparison.
	declared, err := config.LoadResources(ws.Dir, filepath.Base(configFile))
	if err != nil {
		return nil, err
	}
	digest, err := config.Digest(declared)
	if err != nil {
		return nil, err
	}

	if err := saved.Verify(ctx, ws, st, digest); err != nil {
		return nil, err
	}

	return saved.Plan, nil
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
