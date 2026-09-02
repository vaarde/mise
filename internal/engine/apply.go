package engine

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// ApplyOptions tunes an apply run.
type ApplyOptions struct {
	// Parallelism caps concurrent API calls for providers without batch
	// support. Zero means DefaultParallelism.
	Parallelism int

	// Checkpoint, when set, is called with the state after each wave
	// completes. It is how a long apply survives a crash: without it,
	// state reaches disk only when Apply returns, so a power loss after
	// wave three of five leaves three waves of live POS objects that
	// Mise has no record of creating — and the next apply creates them
	// all over again.
	//
	// A checkpoint that fails stops the apply. Continuing would widen
	// exactly the gap the checkpoint exists to close.
	Checkpoint func(*state.State) error
}

// ApplyResult reports what an apply actually did.
//
// It is deliberately separate from the plan: a plan says what should
// happen, this says what did. On a partial failure they differ, and the
// operator needs to see both.
type ApplyResult struct {
	Created []string       `json:"created"`
	Updated []string       `json:"updated"`
	Failed  []ApplyFailure `json:"failed"`
}

// ApplyFailure is one resource that could not be written.
type ApplyFailure struct {
	FullName string `json:"full_name"`
	Action   Action `json:"action"`
	Err      error  `json:"-"`
	Message  string `json:"message"`
}

// HasFailures reports whether anything failed.
func (r *ApplyResult) HasFailures() bool { return len(r.Failed) > 0 }

// Total returns how many resources were written successfully.
func (r *ApplyResult) Total() int { return len(r.Created) + len(r.Updated) }

// Apply executes a plan against the POS and updates state as it goes.
//
// State is written by the caller after Apply returns, including when
// Apply returns an error: a partial apply must still record the
// resources that did land, or the next plan would try to create them a
// second time. Set ApplyOptions.Checkpoint to have state persisted
// after each wave as well.
func Apply(
	ctx context.Context,
	p provider.Provider,
	plan *PlanResult,
	st *state.State,
	opts ApplyOptions,
) (*ApplyResult, error) {
	result := &ApplyResult{}
	if len(plan.Changes) == 0 {
		return result, nil
	}

	if err := checkWritable(plan.Changes); err != nil {
		return result, err
	}

	waves, err := OrderChanges(plan.Changes, nil)
	if err != nil {
		return result, err
	}

	appliedAt := time.Now().UTC()

	for _, wave := range waves {
		// Cancelled between waves is the clean case: nothing in this one
		// has been sent, so stop without inventing failures for it.
		if ctxErr := ctx.Err(); ctxErr != nil {
			st.LastApply = &appliedAt
			return result, ctxErr
		}

		// References to resources created in an earlier wave can only be
		// resolved now that those resources have provider IDs.
		ops, skipped := buildOperations(wave, st)
		result.Failed = append(result.Failed, skipped...)

		if len(ops) == 0 {
			continue
		}

		outcomes, err := writeBatch(ctx, p, ops, opts.Parallelism)
		if err != nil {
			// Cancelled mid-write is the one case Mise cannot describe
			// honestly as a failure: the request may have reached the POS
			// and been applied even though the response never came back.
			// Recording those resources as failed would send the operator
			// to create things that already exist, so say what is actually
			// known — that the outcome is unknown — and point at drift.
			if ctxErr := ctx.Err(); ctxErr != nil {
				// Whatever did report back before the cancellation is
				// known, so record it rather than throwing it away.
				accounted := recordOutcomes(result, wave, ops, outcomes, st, appliedAt)
				st.LastApply = &appliedAt
				saveCheckpoint(opts, st)

				unknown := len(ops) - accounted
				return result, fmt.Errorf(
					"apply interrupted while writing %d %s: whether the change reached the POS is unknown — run 'mise drift' before retrying: %w",
					unknown, pluralizeWord(unknown, "resource", "resources"), ctxErr)
			}

			// The whole call failed, so nothing in this wave landed.
			for _, op := range ops {
				result.Failed = append(result.Failed, ApplyFailure{
					FullName: op.Name,
					Action:   actionFor(op.Resource.ProviderID),
					Err:      err,
					Message:  err.Error(),
				})
			}
			return result, err
		}

		recordOutcomes(result, wave, ops, outcomes, st, appliedAt)

		// Persist what this wave did before starting the next one, so a
		// crash costs at most one wave rather than the whole run.
		if err := saveCheckpointErr(opts, st); err != nil {
			st.LastApply = &appliedAt
			return result, fmt.Errorf("a wave was applied but state could not be saved, "+
				"so a retry would repeat it — run 'mise drift' to see what is live: %w", err)
		}

		// A resource that failed is a dependency nothing later can rely
		// on, so stop rather than cascading confusing errors.
		if result.HasFailures() {
			break
		}
	}

	st.LastApply = &appliedAt

	if result.HasFailures() {
		return result, fmt.Errorf("apply finished with %d failed %s",
			len(result.Failed), pluralizeWord(len(result.Failed), "resource", "resources"))
	}
	return result, nil
}

// checkWritable rejects a plan that cannot be executed as written.
//
// Both cases below are caught when a plan is computed in the same run,
// but "mise apply --plan" executes a file, and a file can be edited.
// Neither would fail loudly at the POS: an empty location list reads as
// "every location" to Square, and a repeated resource would quietly
// collapse into whichever copy came last.
func checkWritable(changes []ResourceChange) error {
	seen := make(map[string]bool, len(changes))

	for _, change := range changes {
		name := change.FullName()
		if seen[name] {
			return fmt.Errorf("the plan contains %s twice — Mise identifies a resource by its type and name, "+
				"so only one of the two would be applied", name)
		}
		seen[name] = true

		if len(change.LocationIDs) == 0 {
			return fmt.Errorf("%s has no locations to apply at — a POS that scopes by location list "+
				"reads an empty one as every location, which is the opposite of what this says", name)
		}
	}

	return nil
}

// saveCheckpoint saves state, ignoring the error. Used where the apply
// is already returning an error and there is nothing further to add.
func saveCheckpoint(opts ApplyOptions, st *state.State) {
	_ = saveCheckpointErr(opts, st)
}

// saveCheckpointErr saves state after a wave, if one is configured.
func saveCheckpointErr(opts ApplyOptions, st *state.State) error {
	if opts.Checkpoint == nil {
		return nil
	}
	return opts.Checkpoint(st)
}

// buildOperations turns a wave of changes into write operations,
// resolving any references that earlier waves have now satisfied.
func buildOperations(wave Wave, st *state.State) ([]provider.WriteOperation, []ApplyFailure) {
	lookup := stateLookup(st)

	var (
		ops     []provider.WriteOperation
		skipped []ApplyFailure
	)

	for _, change := range wave {
		resolved, unresolved := config.ResolveRefs(change.Desired, lookup)
		if len(unresolved) > 0 {
			sort.Strings(unresolved)
			err := fmt.Errorf("cannot resolve %s — the resource it points at was not created",
				config.FormatRef(unresolved[0]))
			skipped = append(skipped, ApplyFailure{
				FullName: change.FullName(),
				Action:   change.Action,
				Err:      err,
				Message:  err.Error(),
			})
			continue
		}

		properties, _ := resolved.(map[string]interface{})
		if properties == nil {
			properties = map[string]interface{}{}
		}

		action := provider.WriteCreate
		if change.Action == ActionUpdate {
			action = provider.WriteUpdate
		}

		version := ""
		if entry, ok := st.Resources[change.FullName()]; ok {
			version = entry.Version
		}

		ops = append(ops, provider.WriteOperation{
			Action: action,
			Name:   change.FullName(),
			Resource: &provider.Resource{
				Type:        change.ResourceType,
				Name:        change.ResourceName,
				ProviderID:  change.ProviderID,
				Properties:  properties,
				LocationIDs: change.LocationIDs,
				Version:     version,
			},
		})
	}

	return ops, skipped
}

// writeBatch sends a wave to the provider, preferring a batch call when
// the adapter supports one.
func writeBatch(
	ctx context.Context,
	p provider.Provider,
	ops []provider.WriteOperation,
	parallelism int,
) ([]provider.WriteOutcome, error) {
	if batcher, ok := p.(provider.BatchApplier); ok {
		return batcher.ApplyBatch(ctx, ops)
	}
	return writeIndividually(ctx, p, ops, parallelism)
}

// writeIndividually writes one resource per call, for adapters without
// batch support.
//
// Everything in a wave is independent by construction, so the writes run
// concurrently up to parallelism — which is what ApplyOptions.Parallelism
// has always promised and, until now, quietly did not do.
//
// Cancellation stops new writes from starting and returns the context
// error alongside the outcomes that did come back. Apply then reports
// the rest as unknown rather than failed, because a request cut off
// mid-flight may still have reached the POS. Returning a nil error here,
// as an earlier version did, made Apply treat a Ctrl-C as an ordinary
// failure and send the operator to re-create resources that may already
// exist — the exact outcome the batch path takes care to avoid.
func writeIndividually(
	ctx context.Context,
	p provider.Provider,
	ops []provider.WriteOperation,
	parallelism int,
) ([]provider.WriteOutcome, error) {
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}
	if parallelism > len(ops) {
		parallelism = len(ops)
	}

	var (
		mu       sync.Mutex
		outcomes = make([]provider.WriteOutcome, 0, len(ops))
		wg       sync.WaitGroup
		slots    = make(chan struct{}, parallelism)
	)

	for _, op := range ops {
		// A slot has to be free before the next write starts, and a
		// cancelled run must not sit waiting for one.
		select {
		case slots <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}

		wg.Add(1)
		go func(op provider.WriteOperation) {
			defer wg.Done()
			defer func() { <-slots }()

			outcome := writeOne(ctx, p, op)

			mu.Lock()
			outcomes = append(outcomes, outcome)
			mu.Unlock()
		}(op)
	}

	wg.Wait()

	if ctxErr := ctx.Err(); ctxErr != nil {
		return outcomes, ctxErr
	}
	return outcomes, nil
}

// writeOne performs a single create or update.
//
// The scope comes from op.Resource.LocationIDs. There is no separate
// location argument: the interface used to pass one *and* the full set,
// and handing an adapter the first ID of forty was not a contract
// anything could implement correctly.
func writeOne(ctx context.Context, p provider.Provider, op provider.WriteOperation) provider.WriteOutcome {
	outcome := provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}

	switch op.Action {
	case provider.WriteCreate:
		id, err := p.Create(ctx, op.Resource.Type, op.Resource)
		if err == nil {
			outcome.ProviderID = id
		}
		outcome.Err = err
	case provider.WriteUpdate:
		outcome.Err = p.Update(ctx, op.Resource.Type, op.Resource.ProviderID, op.Resource)
	}

	return outcome
}

// recordOutcomes folds a wave's results into the apply result and state,
// and reports how many operations the outcomes accounted for.
//
// Outcomes are matched to operations by name rather than by position. A
// batch adapter returns them in order, but the individual path completes
// concurrently and returns fewer than it was given when a run is
// cancelled — position would then attribute one resource's result to
// another.
func recordOutcomes(
	result *ApplyResult,
	wave Wave,
	ops []provider.WriteOperation,
	outcomes []provider.WriteOutcome,
	st *state.State,
	appliedAt time.Time,
) int {
	changesByName := make(map[string]ResourceChange, len(wave))
	for _, change := range wave {
		changesByName[change.FullName()] = change
	}

	opsByName := make(map[string]provider.WriteOperation, len(ops))
	for _, op := range ops {
		opsByName[op.Name] = op
	}

	accounted := 0
	for _, outcome := range outcomes {
		op, ok := opsByName[outcome.Name]
		if !ok {
			// A provider naming an outcome Mise never asked for is
			// misbehaving; there is nothing sound to record for it.
			continue
		}
		change := changesByName[op.Name]
		accounted++

		if outcome.Err != nil {
			result.Failed = append(result.Failed, ApplyFailure{
				FullName: op.Name,
				Action:   change.Action,
				Err:      outcome.Err,
				Message:  outcome.Err.Error(),
			})
			continue
		}

		providerID := outcome.ProviderID
		if providerID == "" {
			providerID = op.Resource.ProviderID
		}

		// State records what is now live, so the next plan compares
		// against reality rather than re-proposing the same change.
		st.Resources[op.Name] = &state.ResourceState{
			ProviderID: providerID,
			Type:       change.ResourceType,
			Name:       change.ResourceName,
			Locations:  change.LocationIDs,
			Properties: op.Resource.Properties,
			Version:    outcome.Version,
			LastSynced: appliedAt,
		}

		if change.Action == ActionCreate {
			result.Created = append(result.Created, op.Name)
		} else {
			result.Updated = append(result.Updated, op.Name)
		}
	}

	sort.Strings(result.Created)
	sort.Strings(result.Updated)
	return accounted
}

// actionFor infers the action from whether an ID already exists.
func actionFor(providerID string) Action {
	if providerID == "" {
		return ActionCreate
	}
	return ActionUpdate
}

// pluralizeWord picks the singular or plural form.
func pluralizeWord(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
