package engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// cancellingProvider cancels the run at a chosen point, standing in for
// an operator pressing Ctrl-C.
type cancellingProvider struct {
	batchProvider

	cancel      context.CancelFunc
	cancelAfter int // cancel once this many batches have been sent
}

func (c *cancellingProvider) ApplyBatch(ctx context.Context, ops []provider.WriteOperation) ([]provider.WriteOutcome, error) {
	if c.callCount >= c.cancelAfter {
		c.cancel()
		// The request was already in flight when the signal arrived, so
		// the transport reports a cancellation rather than a result.
		c.callCount++
		c.batches = append(c.batches, ops)
		return nil, context.Canceled
	}
	return c.batchProvider.ApplyBatch(ctx, ops)
}

func newCancellingProvider(cancel context.CancelFunc, after int) *cancellingProvider {
	return &cancellingProvider{
		batchProvider: *newBatchProvider(),
		cancel:        cancel,
		cancelAfter:   after,
	}
}

func TestApplyInterruptedMidWriteDoesNotClaimTheChangeFailed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := newCancellingProvider(cancel, 0)
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "ga_sales_tax", []string{"LOC_A"},
				map[string]interface{}{"name": "GA Sales Tax", "percentage": "4.5"}),
		},
		Summary: PlanSummary{ToCreate: 1},
	}}

	result, err := Apply(ctx, p, plan, st, ApplyOptions{})

	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// The batch may well have landed on the POS with the response lost in
	// transit. Recording it as failed would send the operator to create a
	// resource that already exists.
	assert.Empty(t, result.Failed,
		"an interrupted write has an unknown outcome, not a known failure")
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "mise drift")
}

func TestApplyCancelledBetweenWavesKeepsWhatLanded(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := newCancellingProvider(cancel, 1)
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

	result, err := Apply(ctx, p, plan, st, ApplyOptions{})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)

	// The first wave completed before the signal, so it must still be in
	// state — otherwise the next plan would create the category twice.
	assert.Equal(t, []string{"square_catalog_category.beverages"}, result.Created)
	require.Contains(t, st.Resources, "square_catalog_category.beverages")
	assert.Equal(t, "CAT_NEW", st.Resources["square_catalog_category.beverages"].ProviderID)

	// LastApply is stamped even on an interrupted run: something did
	// happen, and drift needs the baseline to say when.
	assert.NotNil(t, st.LastApply)
}

func TestApplyCancelledBeforeStartWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{
		Changes: []ResourceChange{
			createChange("square_catalog_tax", "ga_sales_tax", []string{"LOC_A"},
				map[string]interface{}{"name": "GA Sales Tax"}),
		},
		Summary: PlanSummary{ToCreate: 1},
	}}

	result, err := Apply(ctx, p, plan, st, ApplyOptions{})

	assert.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, p.batches, "nothing should reach the POS after cancellation")
	assert.Empty(t, result.Created)
	assert.Empty(t, result.Failed)
}

func TestFetchReportsCancellationRatherThanAReadFailure(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	p := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A", Name: "Atlanta"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{},
		beforeRead: func() {
			cancel()
		},
	}

	_, err := Fetch(ctx, p, FetchOptions{})

	// Ctrl-C surfaces first as a read error at whichever location noticed
	// it, which reads like a fault at that location rather than the
	// operator's own doing.
	assert.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "cannot read")
}
