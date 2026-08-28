package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/state"
	"github.com/vaarde/mise/pkg/output"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Preview changes between config files and live POS state",
	Long: `Compares your declared YAML configuration against the live POS
state and shows what would change — without making any modifications.

Think of it like a dry run: you see exactly what Mise would create,
update, or flag before committing to the change.

Resources that exist on the POS but not in your config files are left
alone. Mise only manages what you declare.`,
	RunE: runPlan,
}

// Flags for plan
var (
	planTarget      string
	planLocation    string
	planOutFile     string
	planParallelism int
)

func init() {
	planCmd.Flags().StringVar(&planTarget, "target", "", "plan only for a specific resource name")
	planCmd.Flags().StringVar(&planLocation, "location", "", "plan only for a specific location")
	planCmd.Flags().StringVarP(&planOutFile, "out", "o", "", "save plan to file for later apply")
	planCmd.Flags().IntVar(&planParallelism, "parallelism", engine.DefaultParallelism,
		"maximum locations to read concurrently")
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	out := cmd.OutOrStdout()

	plan, err := computePlan(ctx, out, planOptions{
		target:      planTarget,
		location:    planLocation,
		parallelism: planParallelism,
	})
	if err != nil {
		return err
	}

	output.NewPlanPrinter(out, plan.Locations).Print(plan)

	if planOutFile != "" {
		if err := savePlan(planOutFile, plan); err != nil {
			return err
		}
		fmt.Fprintf(out, "\nPlan saved to %s. Apply it with:\n    mise apply --plan %s\n",
			planOutFile, planOutFile)
	}

	return nil
}

// planOptions carries the shared inputs of plan and apply.
type planOptions struct {
	target      string
	location    string
	parallelism int
}

// computePlan loads the workspace, reads live state, and diffs the two.
// apply reuses this so that what it executes is exactly what plan shows.
func computePlan(ctx context.Context, out io.Writer, opts planOptions) (*engine.PlanResult, error) {
	ws, err := loadWorkspace(ctx, out, configFile)
	if err != nil {
		return nil, err
	}

	declared, err := config.LoadResources(ws.Dir, filepath.Base(configFile))
	if err != nil {
		return nil, err
	}
	if len(declared) == 0 {
		return nil, fmt.Errorf("no resources declared in %s — run 'mise fetch' to import your current configuration", ws.Dir)
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return nil, err
	}

	return engine.ComputePlan(ctx, ws.Provider, ws.Config, declared, st, engine.PlanOptions{
		TargetResource: opts.target,
		TargetLocation: opts.location,
		Parallelism:    opts.parallelism,
	})
}

// savePlan writes a plan to disk for a later apply.
func savePlan(path string, plan *engine.PlanResult) error {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize plan: %w", err)
	}
	data = append(data, '\n')

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("cannot create directory for %s: %w", path, err)
		}
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("cannot write plan file %s: %w", path, err)
	}
	return nil
}

// LoadPlan reads a plan saved by 'mise plan --out'. Apply uses this.
func LoadPlan(path string) (*engine.PlanResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read plan file %s: %w", path, err)
	}

	var plan engine.PlanResult
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, fmt.Errorf("cannot parse plan file %s: %w", path, err)
	}
	return &plan, nil
}
