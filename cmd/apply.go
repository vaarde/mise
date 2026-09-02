package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

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
	applyJSON        bool
)

func init() {
	applyCmd.Flags().BoolVar(&applyAutoApprove, "auto-approve", false, "skip confirmation prompt (for CI/CD)")
	applyCmd.Flags().StringVar(&applyPlanFile, "plan", "", "apply a previously saved plan file")
	applyCmd.Flags().StringVar(&applyTarget, "target", "", "apply only a specific resource")
	applyCmd.Flags().StringVar(&applyLocation, "location", "", "apply only for a specific location")
	applyCmd.Flags().IntVar(&applyParallelism, "parallelism", engine.DefaultParallelism,
		"max concurrent API calls")
	applyCmd.Flags().BoolVar(&applyJSON, "json", false, "write the apply result as machine-readable JSON")
	rootCmd.AddCommand(applyCmd)
}

func runApply(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()
	diagnostics := out
	if applyJSON {
		// Keep stdout valid JSON for callers such as the Strands runtime.
		// Human diagnostics may still go to stderr.
		diagnostics = cmd.ErrOrStderr()
	}

	ws, err := loadWorkspace(ctx, diagnostics, configFile)
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
			fmt.Fprintf(diagnostics, "Warning: %v\n", releaseErr)
		}
	}()

	plan, err := resolveApplyPlan(ctx, diagnostics)
	if err != nil {
		return err
	}

	if !applyJSON {
		printer := output.NewPlanPrinter(out, plan.Locations)
		printer.Print(plan)
	}

	if !plan.HasChanges() {
		if applyJSON {
			return printApplyJSON(out, &engine.ApplyResult{}, nil, "no_changes")
		}
		return nil
	}

	approved, err := confirmApply(diagnostics, applyAutoApprove)
	if err != nil {
		return err
	}
	if !approved {
		if applyJSON {
			return printApplyJSON(out, &engine.ApplyResult{}, nil, "cancelled")
		}
		fmt.Fprintln(out, "\nApply cancelled. Nothing was changed.")
		return nil
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return err
	}

	if !applyJSON {
		fmt.Fprintf(out, "\nApplying changes...\n")
	}

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

	if applyJSON {
		if err := printApplyJSON(out, result, applyErr, ""); err != nil {
			return err
		}
	} else {
		output.PrintApplyResult(out, result)
	}

	if applyErr != nil {
		return applyErr
	}
	return nil
}

type applyJSONFailure struct {
	Resource string `json:"resource"`
	Action   string `json:"action"`
	Message  string `json:"message"`
}

type applyJSONEnvelope struct {
	Status  string             `json:"status"`
	Created []string           `json:"created"`
	Updated []string           `json:"updated"`
	Failed  []applyJSONFailure `json:"failed"`
}

func printApplyJSON(out io.Writer, result *engine.ApplyResult, applyErr error, forcedStatus string) error {
	if result == nil {
		result = &engine.ApplyResult{}
	}

	status := forcedStatus
	if status == "" {
		switch {
		case applyErr != nil && strings.Contains(applyErr.Error(), "whether the change reached the POS is unknown"):
			status = "outcome_uncertain"
		case result.HasFailures():
			status = "partial"
		case applyErr != nil:
			status = "failed"
		default:
			status = "success"
		}
	}

	failures := make([]applyJSONFailure, 0, len(result.Failed))
	for _, failure := range result.Failed {
		failures = append(failures, applyJSONFailure{
			Resource: failure.FullName,
			Action:   failure.Action.String(),
			Message:  failure.Message,
		})
	}

	return json.NewEncoder(out).Encode(applyJSONEnvelope{
		Status:  status,
		Created: nonNilStrings(result.Created),
		Updated: nonNilStrings(result.Updated),
		Failed:  failures,
	})
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
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
