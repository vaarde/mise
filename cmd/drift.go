package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/state"
	"github.com/vaarde/mise/pkg/output"
)

var driftCmd = &cobra.Command{
	Use:   "drift",
	Short: "Detect POS configuration changes made outside Mise",
	Long: `Compares the live POS configuration against the last state Mise
recorded, and reports anything that changed outside of Mise — someone
editing a tax rate in the POS dashboard, another tool writing to the
same account, a resource deleted by hand.

Drift reads only. It never changes the POS and never rewrites state:
a drift report is evidence of a discrepancy, not permission to accept
it.

Exit code 2 means drift was found, so a scheduled check can alert on
it without parsing the output.`,
	RunE: runDrift,
}

// Flags for drift
var (
	driftLocation    string
	driftType        string
	driftJSON        bool
	driftParallelism int
)

// ExitCodeDrift is returned when drift is detected, so CI and cron jobs
// can distinguish "drift found" from "the command failed".
const ExitCodeDrift = 2

// driftDetectedError signals drift without being an operational failure.
type driftDetectedError struct{ count int }

func (e *driftDetectedError) Error() string {
	return fmt.Sprintf("drift detected in %d %s", e.count,
		map[bool]string{true: "resource", false: "resources"}[e.count == 1])
}

// ExitCode reports the process exit status for this error.
func (e *driftDetectedError) ExitCode() int { return ExitCodeDrift }

// Silent reports that the drift report already told the operator what
// happened, so Execute should not print an error line as well.
func (e *driftDetectedError) Silent() bool { return true }

func init() {
	driftCmd.Flags().StringVar(&driftLocation, "location", "", "check drift for a single location")
	driftCmd.Flags().StringVar(&driftType, "type", "", "check drift for a specific resource type")
	driftCmd.Flags().BoolVar(&driftJSON, "json", false, "output drift report as JSON")
	driftCmd.Flags().IntVar(&driftParallelism, "parallelism", engine.DefaultParallelism,
		"maximum locations to read concurrently")
	rootCmd.AddCommand(driftCmd)
}

func runDrift(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()

	ws, err := loadWorkspace(ctx, out, configFile)
	if err != nil {
		return err
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return err
	}

	// Drift compares recorded provider IDs against a live account. Run
	// against a different account, every ID would miss and the report
	// would claim the entire configuration had been deleted.
	if err := ws.CheckStateIdentity(ctx, st); err != nil {
		return err
	}

	result, err := engine.Drift(ctx, ws.Provider, st, engine.DriftOptions{
		TargetLocation: driftLocation,
		ResourceType:   driftType,
		Parallelism:    driftParallelism,
	})
	if err != nil {
		return err
	}

	if driftJSON {
		if err := output.PrintDriftJSON(out, result); err != nil {
			return err
		}
	} else {
		output.NewDriftPrinter(out, result.Locations).Print(result)
	}

	if result.HasDrift() {
		return &driftDetectedError{count: len(result.Drifted)}
	}
	return nil
}
