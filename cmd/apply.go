package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"

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
	applyCmd.Flags().IntVar(&applyParallelism, "parallelism", engine.DefaultParallelism, "max concurrent API calls")
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
		diagnostics = cmd.ErrOrStderr()
	}

	ws, err := loadWorkspace(ctx, diagnostics, configFile)
	if err != nil {
		return err
	}

	lock, err := state.Acquire(ws.Dir, "apply")
	if err != nil {
		return err
	}
	defer func() {
		if releaseErr := lock.Release(); releaseErr != nil {
			fmt.Fprintf(diagnostics, "Warning: %v\n", releaseErr)
		}
	}()

	plan, err := resolveApplyPlan(ctx, diagnostics, ws)
	if err != nil {
		return err
	}

	if !applyJSON {
		output.NewPlanPrinter(out, plan.Locations).Print(plan)
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
	if err := ws.CheckStateIdentity(ctx, st); err != nil {
		return err
	}
	identity, err := ws.Identity(ctx)
	if err != nil {
		return err
	}
	st.SetIdentity(identity)

	if !applyJSON {
		fmt.Fprintln(out, "\nApplying changes...")
	}

	result, applyErr := engine.Apply(ctx, ws.Provider, plan, st, engine.ApplyOptions{
		Parallelism: applyParallelism,
		Checkpoint: func(s *state.State) error { return s.Save(ws.Dir) },
	})

	// A saved plan's state serial is part of its safety contract. If the
	// provider rejected the whole write before anything landed, advancing
	// that serial would make an otherwise safe retry look stale even though
	// the live POS and Mise state are unchanged. Persist state on success or
	// after a genuine partial success; leave it untouched on a zero-write
	// failure so the exact approved plan remains retryable.
	if applyErr == nil || result.Total() > 0 {
		if saveErr := st.Save(ws.Dir); saveErr != nil {
			if applyErr != nil {
				return fmt.Errorf("%w (and the state file could not be saved: %v)", applyErr, saveErr)
			}
			return saveErr
		}
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
		failures = append(failures, applyJSONFailure{Resource: failure.FullName, Action: failure.Action.String(), Message: failure.Message})
	}
	return json.NewEncoder(out).Encode(applyJSONEnvelope{
		Status: status, Created: nonNilStrings(result.Created), Updated: nonNilStrings(result.Updated), Failed: failures,
	})
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func resolveApplyPlan(ctx context.Context, out io.Writer, ws *workspace) (*engine.PlanResult, error) {
	if applyPlanFile != "" {
		fmt.Fprintf(out, "Applying saved plan %s\n", applyPlanFile)
		return loadVerifiedPlan(ctx, ws, applyPlanFile)
	}
	plan, _, err := computePlan(ctx, ws, planOptions{target: applyTarget, location: applyLocation, parallelism: applyParallelism})
	return plan, err
}

func loadVerifiedPlan(ctx context.Context, ws *workspace, path string) (*engine.PlanResult, error) {
	saved, err := LoadPlan(path)
	if err != nil {
		return nil, err
	}
	st, err := state.Load(ws.Dir)
	if err != nil {
		return nil, err
	}
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
