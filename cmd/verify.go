package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/state"
)

var verifyCmd = &cobra.Command{
	Use:   "verify",
	Short: "Verify live POS state against an approved saved plan",
	Long: `Re-reads the live POS configuration affected by a saved plan and
checks whether each targeted location actually matches the plan's desired
values. Verification is read-only and never rewrites Mise state.

Use --jsonl for a machine-readable event stream suitable for progress UIs.`,
	RunE: runVerify,
}

var (
	verifyPlanFile string
	verifyJSONL    bool
)

func init() {
	verifyCmd.Flags().StringVar(&verifyPlanFile, "plan", "", "saved plan file to verify (required)")
	verifyCmd.Flags().BoolVar(&verifyJSONL, "jsonl", false, "emit newline-delimited verification events")
	rootCmd.AddCommand(verifyCmd)
}

func runVerify(cmd *cobra.Command, args []string) error {
	if verifyPlanFile == "" {
		return fmt.Errorf("verify requires --plan <file>")
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()
	diagnostics := out
	if verifyJSONL {
		diagnostics = cmd.ErrOrStderr()
	}

	ws, err := loadWorkspace(ctx, diagnostics, configFile)
	if err != nil {
		return err
	}
	saved, err := LoadPlan(verifyPlanFile)
	if err != nil {
		return err
	}
	currentIdentity, err := ws.Identity(ctx)
	if err != nil {
		return err
	}
	if err := saved.Identity.Check(currentIdentity); err != nil {
		return fmt.Errorf("this plan was not built for the account you are verifying against.\n%w", err)
	}
	st, err := state.Load(ws.Dir)
	if err != nil {
		return err
	}
	if err := ws.CheckStateIdentity(ctx, st); err != nil {
		return err
	}

	var emit func(engine.VerifyEvent) error
	if verifyJSONL {
		encoder := json.NewEncoder(out)
		emit = func(event engine.VerifyEvent) error { return encoder.Encode(event) }
	}

	result, err := engine.Verify(ctx, ws.Provider, saved.Plan, st, emit)
	if err != nil {
		return err
	}
	if verifyJSONL {
		return nil
	}
	printVerifySummary(out, result)
	return nil
}

func printVerifySummary(out io.Writer, result *engine.VerifyResult) {
	fmt.Fprintf(out, "Verified %d/%d locations.\n", result.Verified, result.Total)
	fmt.Fprintf(out, "%d converged, %d need attention.\n", result.Converged, result.NonConverged)
	for _, location := range result.Locations {
		if location.Converged {
			continue
		}
		name := location.LocationName
		if name == "" {
			name = location.LocationID
		}
		fmt.Fprintf(out, "\n- %s\n", name)
		for _, issue := range location.Issues {
			fmt.Fprintf(out, "    %s: %s\n", issue.Resource, issue.Message)
		}
	}
}
