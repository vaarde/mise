package engine

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// batchProvider is a stub adapter that records what it was asked to
// write and can be told to fail specific resources.
type batchProvider struct {
	stubProvider

	batches   [][]provider.WriteOperation
	failFor   map[string]error
	newIDs    map[string]string
	batchErr  error
	callCount int
}

func newBatchProvider() *batchProvider {
	return &batchProvider{
		stubProvider: stubProvider{resources: map[string][]*provider.Resource{}},
		failFor:      map[string]error{},
		newIDs:       map[string]string{},
	}
}

func (b *batchProvider) ApplyBatch(_ context.Context, ops []provider.WriteOperation) ([]provider.WriteOutcome, error) {
	b.callCount++
	b.batches = append(b.batches, ops)

	if b.batchErr != nil {
		return nil, b.batchErr
	}

	outcomes := make([]provider.WriteOutcome, len(ops))
	for i, op := range ops {
		outcome := provider.WriteOutcome{Name: op.Name, ProviderID: op.Resource.ProviderID}

		if err, failing := b.failFor[op.Name]; failing {
			outcome.Err = err
			outcomes[i] = outcome
			continue
		}

		if op.Action == provider.WriteCreate {
			id, ok := b.newIDs[op.Name]
			if !ok {
				id = "NEW_" + op.Resource.Name
			}
			outcome.ProviderID = id
		}
		outcome.Version = "100"
		outcomes[i] = outcome
	}

	return outcomes, nil
}

// createChange builds a create for a resource with given properties.
func createChange(resourceType, name string, locationIDs []string, properties map[string]interface{}) ResourceChange {
	return ResourceChange{
		Action:       ActionCreate,
		ResourceType: resourceType,
		ResourceName: name,
		LocationIDs:  locationIDs,
		Desired:      properties,
	}
}

func TestApplyCreatesResources(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "ga_tax", []string{"LOC_A"},
				map[string]interface{}{"name": "GA Tax", "percentage": "4.5"}),
		},
		Summary: PlanSummary{ToCreate: 1},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"square_catalog_tax.ga_tax"}, result.Created)
	assert.Empty(t, result.Updated)
	assert.False(t, result.HasFailures())

	// State records the new provider ID, so the next plan sees the
	// resource as existing rather than proposing it again.
	entry, ok := st.Resources["square_catalog_tax.ga_tax"]
	require.True(t, ok)
	assert.Equal(t, "NEW_ga_tax", entry.ProviderID)
	assert.Equal(t, "100", entry.Version, "the version token enables the next update's concurrency check")
	assert.Equal(t, []string{"LOC_A"}, entry.Locations)
	require.NotNil(t, st.LastApply)
}

func TestApplyUpdatesResources(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")
	st.Resources["square_catalog_tax.ga_tax"] = &state.ResourceState{
		ProviderID: "TAX_1", Version: "42",
	}

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{{
			Action:       ActionUpdate,
			ResourceType: "square_catalog_tax",
			ResourceName: "ga_tax",
			ProviderID:   "TAX_1",
			LocationIDs:  []string{"LOC_A"},
			Desired:      map[string]interface{}{"percentage": "5.0"},
		}},
		Summary: PlanSummary{ToUpdate: 1},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, []string{"square_catalog_tax.ga_tax"}, result.Updated)
	assert.Empty(t, result.Created)

	// The stored version is passed back to the provider so a change made
	// between plan and apply is caught rather than silently overwritten.
	require.Len(t, p.batches, 1)
	assert.Equal(t, "42", p.batches[0][0].Resource.Version)
	assert.Equal(t, provider.WriteUpdate, p.batches[0][0].Action)
}

func TestApplyOrdersDependenciesAcrossWaves(t *testing.T) {
	p := newBatchProvider()
	p.newIDs["square_catalog_category.beverages"] = "CAT_NEW"
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_item", "lemonade", []string{"LOC_A"},
				map[string]interface{}{
					"name":     "Lemonade",
					"category": "ref(square_catalog_category.beverages)",
				}),
			createChange("square_catalog_category", "beverages", []string{"LOC_A"},
				map[string]interface{}{"name": "Beverages"}),
		},
		Summary: PlanSummary{ToCreate: 2},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)
	assert.Len(t, result.Created, 2)

	// Two waves: the category first, then the item that references it.
	require.Len(t, p.batches, 2)
	assert.Equal(t, "square_catalog_category.beverages", p.batches[0][0].Name)
	assert.Equal(t, "square_catalog_item.lemonade", p.batches[1][0].Name)

	// By the time the item is written, its reference has been resolved to
	// the ID the category was just assigned.
	assert.Equal(t, "CAT_NEW", p.batches[1][0].Resource.Properties["category"],
		"a reference must resolve to the ID created in the previous wave")
}

func TestApplyBatchesIndependentResourcesTogether(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "a", []string{"LOC_A"}, map[string]interface{}{"x": 1}),
			createChange("square_catalog_tax", "b", []string{"LOC_A"}, map[string]interface{}{"x": 2}),
			createChange("square_catalog_tax", "c", []string{"LOC_A"}, map[string]interface{}{"x": 3}),
		},
		Summary: PlanSummary{ToCreate: 3},
	}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, 1, p.callCount, "independent resources should go up in one API call")
	assert.Len(t, p.batches[0], 3)
}

func TestApplyRecordsPartialSuccess(t *testing.T) {
	p := newBatchProvider()
	p.failFor["square_catalog_tax.b"] = fmt.Errorf("INVALID_VALUE: percentage out of range")
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "a", []string{"LOC_A"}, map[string]interface{}{"x": 1}),
			createChange("square_catalog_tax", "b", []string{"LOC_A"}, map[string]interface{}{"x": 2}),
		},
		Summary: PlanSummary{ToCreate: 2},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})

	require.Error(t, err, "an apply with failures must not report success")
	assert.Contains(t, err.Error(), "1 failed resource")

	// The one that worked is recorded; re-running will not create it twice.
	assert.Equal(t, []string{"square_catalog_tax.a"}, result.Created)
	assert.Contains(t, st.Resources, "square_catalog_tax.a")
	assert.NotContains(t, st.Resources, "square_catalog_tax.b")

	require.Len(t, result.Failed, 1)
	assert.Equal(t, "square_catalog_tax.b", result.Failed[0].FullName)
	assert.Contains(t, result.Failed[0].Message, "percentage out of range")
}

func TestApplyStopsAfterAFailedWave(t *testing.T) {
	p := newBatchProvider()
	p.failFor["square_catalog_category.beverages"] = fmt.Errorf("rejected")
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_category", "beverages", []string{"LOC_A"},
				map[string]interface{}{"name": "Beverages"}),
			createChange("square_catalog_item", "lemonade", []string{"LOC_A"},
				map[string]interface{}{"category": "ref(square_catalog_category.beverages)"}),
		},
		Summary: PlanSummary{ToCreate: 2},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.Error(t, err)

	assert.Equal(t, 1, p.callCount,
		"the item depends on the failed category, so the second wave must not run")
	assert.Empty(t, result.Created)
}

func TestApplyReportsAWholeBatchFailure(t *testing.T) {
	p := newBatchProvider()
	p.batchErr = fmt.Errorf("503 service unavailable")
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "a", []string{"LOC_A"}, map[string]interface{}{"x": 1}),
			createChange("square_catalog_tax", "b", []string{"LOC_A"}, map[string]interface{}{"x": 2}),
		},
	}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "service unavailable")

	assert.Len(t, result.Failed, 2, "nothing in a failed call landed")
	assert.Empty(t, st.Resources, "state must not claim anything was written")
}

func TestApplyEmptyPlanDoesNothing(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")

	result, err := Apply(context.Background(), p, &PlanResult{}, st, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, 0, result.Total())
	assert.Equal(t, 0, p.callCount)
	assert.Nil(t, st.LastApply, "an empty apply is not an apply")
}

func TestApplyRejectsCircularReferences(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("t", "a", nil, map[string]interface{}{"needs": "ref(t.b)"}),
		createChange("t", "b", nil, map[string]interface{}{"needs": "ref(t.a)"}),
	}}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "circular reference")
	assert.Equal(t, 0, p.callCount, "nothing should be written when the order is impossible")
}

func TestApplyPassesLocationSetToTheProvider(t *testing.T) {
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_tax", "ga_tax", []string{"LOC_A", "LOC_B", "LOC_C"},
			map[string]interface{}{"percentage": "4.5"}),
	}}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)

	// Square creates one object carrying its location list, not one
	// object per location, so the whole set must reach the adapter.
	require.Len(t, p.batches, 1)
	assert.Equal(t, []string{"LOC_A", "LOC_B", "LOC_C"}, p.batches[0][0].Resource.LocationIDs)
}

func TestApplyFallsBackToIndividualWrites(t *testing.T) {
	// stubProvider does not implement BatchApplier, so apply must use
	// Create and Update instead of failing.
	p := &writeCountingProvider{stubProvider: stubProvider{resources: map[string][]*provider.Resource{}}}
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_tax", "a", []string{"LOC_A"}, map[string]interface{}{"x": 1}),
		createChange("square_catalog_tax", "b", []string{"LOC_A"}, map[string]interface{}{"x": 2}),
	}}}

	result, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.NoError(t, err)

	assert.Equal(t, 2, p.creates, "an adapter without batch support still applies")
	assert.Len(t, result.Created, 2)
}

// writeCountingProvider implements Create/Update but not BatchApplier.
type writeCountingProvider struct {
	stubProvider
	creates int
	updates int
}

func (w *writeCountingProvider) Create(_ context.Context, _ string, desired *provider.Resource, _ string) (string, error) {
	w.creates++
	return "ID_" + desired.Name, nil
}

func (w *writeCountingProvider) Update(_ context.Context, _ string, _ string, _ *provider.Resource, _ string) error {
	w.updates++
	return nil
}
