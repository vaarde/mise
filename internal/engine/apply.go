package engine

import (
	"context"
	"fmt"
	"sort"
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
// second time.
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
				st.LastApply = &appliedAt
				return result, fmt.Errorf(
					"apply interrupted while writing %d %s: whether the change reached the POS is unknown — run 'mise drift' before retrying: %w",
					len(ops), pluralizeWord(len(ops), "resource", "resources"), ctxErr)
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

// writeIndividually applies operations one at a time, for adapters
// without batch support.
func writeIndividually(
	ctx context.Context,
	p provider.Provider,
	ops []provider.WriteOperation,
	parallelism int,
) ([]provider.WriteOutcome, error) {
	if parallelism <= 0 {
		parallelism = DefaultParallelism
	}

	outcomes := make([]provider.WriteOutcome, len(ops))

	for i, op := range ops {
		outcome := provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}

		locationID := ""
		if len(op.Resource.LocationIDs) > 0 {
			locationID = op.Resource.LocationIDs[0]
		}

		switch op.Action {
		case provider.WriteCreate:
			id, err := p.Create(ctx, op.Resource.Type, op.Resource, locationID)
			outcome.ProviderID, outcome.Err = id, err
		case provider.WriteUpdate:
			outcome.Err = p.Update(ctx, op.Resource.Type, op.Resource.ProviderID, op.Resource, locationID)
		}

		outcomes[i] = outcome
	}

	return outcomes, nil
}

// recordOutcomes folds a wave's results into the apply result and state.
func recordOutcomes(
	result *ApplyResult,
	wave Wave,
	ops []provider.WriteOperation,
	outcomes []provider.WriteOutcome,
	st *state.State,
	appliedAt time.Time,
) {
	changesByName := make(map[string]ResourceChange, len(wave))
	for _, change := range wave {
		changesByName[change.FullName()] = change
	}

	for i, outcome := range outcomes {
		if i >= len(ops) {
			break
		}
		op := ops[i]
		change := changesByName[op.Name]

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
