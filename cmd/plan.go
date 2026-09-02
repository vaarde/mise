package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/state"
	"github.com/vaarde/mise/internal/version"
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

	ws, err := loadWorkspace(ctx, out, configFile)
	if err != nil {
		return err
	}

	plan, inputs, err := computePlan(ctx, ws, planOptions{
		target:      planTarget,
		location:    planLocation,
		parallelism: planParallelism,
	})
	if err != nil {
		return err
	}

	output.NewPlanPrinter(out, plan.Locations).Print(plan)

	if planOutFile != "" {
		saved, err := newSavedPlan(ctx, ws, plan, inputs)
		if err != nil {
			return err
		}
		if err := savePlan(planOutFile, saved); err != nil {
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

// planInputs records what a plan was computed from, so a saved plan can
// say which account, which state, and which config produced it.
type planInputs struct {
	State  *state.State
	Digest string
}

// computePlan reads live state and diffs it against the declared config.
// apply reuses this so that what it executes is exactly what plan shows.
func computePlan(ctx context.Context, ws *workspace, opts planOptions) (*engine.PlanResult, *planInputs, error) {
	declared, err := config.LoadResources(ws.Dir, filepath.Base(configFile))
	if err != nil {
		return nil, nil, err
	}
	if len(declared) == 0 {
		return nil, nil, fmt.Errorf("no resources declared in %s — run 'mise fetch' to import your current configuration", ws.Dir)
	}

	st, err := state.Load(ws.Dir)
	if err != nil {
		return nil, nil, err
	}

	// Everything below reads provider IDs out of state and compares them
	// against a live account. If that is not the account the state came
	// from, every comparison is meaningless.
	if err := ws.CheckStateIdentity(ctx, st); err != nil {
		return nil, nil, err
	}

	digest, err := config.Digest(declared)
	if err != nil {
		return nil, nil, err
	}

	plan, err := engine.ComputePlan(ctx, ws.Provider, ws.Config, declared, st, engine.PlanOptions{
		TargetResource: opts.target,
		TargetLocation: opts.location,
		Parallelism:    opts.parallelism,
	})
	if err != nil {
		return nil, nil, err
	}

	return plan, &planInputs{State: st, Digest: digest}, nil
}

// savedPlanFormat is the version of the plan file layout. It changes
// when the file's shape changes, so an old file is refused with an
// explanation rather than half-understood.
const savedPlanFormat = 2

// SavedPlan is what 'mise plan --out' writes and 'mise apply --plan'
// reads.
//
// The plan itself is only half of it. A plan is a set of instructions
// naming provider IDs, and those IDs mean nothing outside the account
// that issued them — so the file also records which account, which
// state, and which config it came from. Applying a plan built against
// the sandbox to production would otherwise be an ordinary successful
// run that re-created the entire menu in the wrong place.
type SavedPlan struct {
	FormatVersion int    `json:"format_version"`
	MiseVersion   string `json:"mise_version"`

	// CreatedAt is when the plan was computed, so an operator can see
	// how old the thing they are about to apply is.
	CreatedAt time.Time `json:"created_at"`

	// Identity is the POS account the plan was computed against.
	Identity state.Identity `json:"identity"`

	// StateSerial is the state file's serial at plan time. State moving
	// on means something else applied in between, so the provider IDs
	// and version tokens in the plan may already be stale.
	StateSerial int `json:"state_serial"`

	// ConfigDigest fingerprints the declarations the plan came from, so
	// an edit to the YAML after the review can be pointed out.
	ConfigDigest string `json:"config_digest"`

	Plan *engine.PlanResult `json:"plan"`
}

// newSavedPlan wraps a plan with the provenance needed to apply it
// safely later.
func newSavedPlan(ctx context.Context, ws *workspace, plan *engine.PlanResult, inputs *planInputs) (*SavedPlan, error) {
	identity, err := ws.Identity(ctx)
	if err != nil {
		return nil, err
	}

	return &SavedPlan{
		FormatVersion: savedPlanFormat,
		MiseVersion:   version.Version,
		CreatedAt:     time.Now().UTC(),
		Identity:      identity,
		StateSerial:   inputs.State.Serial,
		ConfigDigest:  inputs.Digest,
		Plan:          plan,
	}, nil
}

// savePlan writes a plan to disk for a later apply.
func savePlan(path string, saved *SavedPlan) error {
	data, err := json.MarshalIndent(saved, "", "  ")
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
func LoadPlan(path string) (*SavedPlan, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("cannot read plan file %s: %w", path, err)
	}

	var saved SavedPlan
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, fmt.Errorf("cannot parse plan file %s: %w", path, err)
	}

	if saved.Plan == nil || saved.FormatVersion == 0 {
		return nil, fmt.Errorf("%s is not a plan file this version of Mise can apply — "+
			"plans written before Mise recorded which account they were built for cannot be verified.\n"+
			"Run 'mise plan --out %s' again", path, path)
	}
	if saved.FormatVersion > savedPlanFormat {
		return nil, fmt.Errorf("%s was written by a newer Mise (plan format %d, this build understands %d) — "+
			"upgrade Mise or regenerate the plan", path, saved.FormatVersion, savedPlanFormat)
	}

	return &saved, nil
}

// Verify checks a saved plan against the workspace about to execute it.
//
// The account must match — a plan is a list of provider IDs, and those
// exist only within the account that issued them. State moving on and
// config being edited are reported as errors too: both mean the thing
// the operator reviewed is no longer the thing about to happen.
func (s *SavedPlan) Verify(ctx context.Context, ws *workspace, st *state.State, digest string) error {
	current, err := ws.Identity(ctx)
	if err != nil {
		return err
	}

	if err := s.Identity.Check(current); err != nil {
		return fmt.Errorf("this plan was not built for the account you are applying it to.\n%w", err)
	}

	if s.StateSerial != st.Serial {
		return fmt.Errorf(
			"the workspace has changed since this plan was made (state was at serial %d, it is now %d).\n"+
				"Something else applied in between, so the plan's recorded IDs and versions may be stale — "+
				"run 'mise plan' again to see the current picture",
			s.StateSerial, st.Serial)
	}

	if digest != "" && s.ConfigDigest != "" && digest != s.ConfigDigest {
		return fmt.Errorf(
			"the config files have been edited since this plan was made.\n" +
				"The plan would apply what it previewed, which is no longer what the files say — " +
				"run 'mise plan' again to review the current configuration")
	}

	return nil
}
