package engine

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

func TestApplyRefusesAResourceWithNoLocations(t *testing.T) {
	// A location group that matched nothing resolves to an empty list,
	// and Square reads an empty list as present_at_all_locations. The
	// plan would show the scope shrinking to nothing while the apply
	// widened it to the whole account.
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_tax", "sales_tax", nil, map[string]interface{}{"percentage": "8.5"}),
	}}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "no locations to apply at")
	assert.Contains(t, err.Error(), "every location")
	assert.Equal(t, 0, p.callCount, "nothing may be written")
}

func TestApplyRefusesADuplicatedResource(t *testing.T) {
	// A saved plan is a file, and a file can be edited. Two changes with
	// the same name would collapse into whichever came last.
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_tax", "sales_tax", []string{"LOC_A"}, map[string]interface{}{"percentage": "8.5"}),
		createChange("square_catalog_tax", "sales_tax", []string{"LOC_A"}, map[string]interface{}{"percentage": "4.0"}),
	}}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "square_catalog_tax.sales_tax")
	assert.Contains(t, err.Error(), "twice")
	assert.Equal(t, 0, p.callCount)
}

func TestOrderChangesRefusesADuplicatedResource(t *testing.T) {
	// The graph keys changes by name, so a duplicate would silently
	// overwrite its twin and one of the two would never be applied.
	changes := []ResourceChange{
		{Action: ActionCreate, ResourceType: "t", ResourceName: "a",
			Desired: map[string]interface{}{"x": 1}},
		{Action: ActionCreate, ResourceType: "t", ResourceName: "a",
			Desired: map[string]interface{}{"x": 2}},
	}

	_, err := OrderChanges(changes, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "t.a")
	assert.Contains(t, err.Error(), "twice")
}

func TestApplyCheckpointsAfterEveryWave(t *testing.T) {
	// Without a checkpoint, state reaches disk only when Apply returns,
	// so a crash after wave one of two leaves live POS objects that Mise
	// has no record of creating — and the next apply creates them again.
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_category", "beverages", []string{"LOC_A"},
			map[string]interface{}{"name": "Beverages"}),
		createChange("square_catalog_item", "tea", []string{"LOC_A"},
			map[string]interface{}{"category": "ref(square_catalog_category.beverages)"}),
	}}}

	var seen []int
	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{
		Checkpoint: func(s *state.State) error {
			seen = append(seen, len(s.Resources))
			return nil
		},
	})
	require.NoError(t, err)

	require.Len(t, seen, 2, "the item depends on the category, so there are two waves")
	assert.Equal(t, []int{1, 2}, seen,
		"each wave is on disk before the next one starts")
}

func TestApplyStopsWhenACheckpointFails(t *testing.T) {
	// Continuing past a failed checkpoint would widen exactly the gap
	// the checkpoint exists to close.
	p := newBatchProvider()
	st := state.New("stub")

	plan := &PlanResult{Plan: Plan{Changes: []ResourceChange{
		createChange("square_catalog_category", "beverages", []string{"LOC_A"},
			map[string]interface{}{"name": "Beverages"}),
		createChange("square_catalog_item", "tea", []string{"LOC_A"},
			map[string]interface{}{"category": "ref(square_catalog_category.beverages)"}),
	}}}

	_, err := Apply(context.Background(), p, plan, st, ApplyOptions{
		Checkpoint: func(*state.State) error { return errors.New("disk full") },
	})
	require.Error(t, err)

	assert.Contains(t, err.Error(), "disk full")
	assert.Contains(t, err.Error(), "mise drift")
	assert.Equal(t, 1, p.callCount, "the second wave never starts")
}

// blockingProvider writes individually and cancels the run partway
// through, standing in for a Ctrl-C landing mid-apply on an adapter
// without batch support.
type blockingProvider struct {
	stubProvider

	mu       sync.Mutex
	writes   int
	cancel   context.CancelFunc
	cancelAt int
}

func (b *blockingProvider) Create(_ context.Context, _ string, desired *provider.Resource) (string, error) {
	b.mu.Lock()
	b.writes++
	shouldCancel := b.writes == b.cancelAt
	b.mu.Unlock()

	if shouldCancel {
		b.cancel()
	}
	return "ID_" + desired.Name, nil
}

func (b *blockingProvider) Update(context.Context, string, string, *provider.Resource) error {
	return nil
}

func (b *blockingProvider) written() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writes
}

func TestInterruptedIndividualWritesAreUnknownNotFailed(t *testing.T) {
	// The individual write path used to swallow cancellation and return
	// a nil error, so Apply recorded every remaining resource as failed.
	// That sends the operator to re-create things that may already
	// exist. The batch path has always reported these as unknown; both
	// paths must say the same thing.
	ctx, cancel := context.WithCancel(context.Background())

	p := &blockingProvider{
		stubProvider: stubProvider{resources: map[string][]*provider.Resource{}},
		cancel:       cancel,
		cancelAt:     1,
	}
	st := state.New("stub")

	changes := make([]ResourceChange, 0, 5)
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		changes = append(changes, createChange("square_catalog_tax", name, []string{"LOC_A"},
			map[string]interface{}{"percentage": "1.0"}))
	}
	plan := &PlanResult{Plan: Plan{Changes: changes}}

	result, err := Apply(ctx, p, plan, st, ApplyOptions{Parallelism: 1})
	require.Error(t, err)

	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "unknown")
	assert.Contains(t, err.Error(), "mise drift")

	assert.Empty(t, result.Failed,
		"a cancelled write is not a failed write — it may well have reached the POS")
	assert.Less(t, p.written(), len(changes), "cancellation stops new writes from starting")
}

func TestCompletedWritesSurviveACancellation(t *testing.T) {
	// Whatever did report back before the interruption is known, so it
	// belongs in state rather than being thrown away.
	ctx, cancel := context.WithCancel(context.Background())

	p := &blockingProvider{
		stubProvider: stubProvider{resources: map[string][]*provider.Resource{}},
		cancel:       cancel,
		cancelAt:     2,
	}
	st := state.New("stub")

	changes := make([]ResourceChange, 0, 6)
	for _, name := range []string{"a", "b", "c", "d", "e", "f"} {
		changes = append(changes, createChange("square_catalog_tax", name, []string{"LOC_A"},
			map[string]interface{}{"percentage": "1.0"}))
	}

	result, err := Apply(ctx, p, &PlanResult{Plan: Plan{Changes: changes}}, st,
		ApplyOptions{Parallelism: 1})
	require.Error(t, err)

	assert.NotEmpty(t, result.Created, "what landed is recorded")
	assert.Len(t, st.Resources, len(result.Created),
		"state records exactly what came back, so a retry does not duplicate it")
}

func TestIndividualWritesRunConcurrently(t *testing.T) {
	// ApplyOptions.Parallelism has always promised concurrent calls for
	// adapters without batch support, and until now did not deliver.
	p := &concurrencyProbe{
		stubProvider: stubProvider{resources: map[string][]*provider.Resource{}},
		release:      make(chan struct{}),
	}
	st := state.New("stub")

	changes := make([]ResourceChange, 0, 4)
	for _, name := range []string{"a", "b", "c", "d"} {
		changes = append(changes, createChange("square_catalog_tax", name, []string{"LOC_A"},
			map[string]interface{}{"percentage": "1.0"}))
	}

	go func() {
		p.waitForConcurrent(4)
		close(p.release)
	}()

	_, err := Apply(context.Background(), p, &PlanResult{Plan: Plan{Changes: changes}}, st,
		ApplyOptions{Parallelism: 4})
	require.NoError(t, err, "all four writes were in flight at once, so none could have been sequential")
}

// concurrencyProbe holds every Create open until enough of them are in
// flight at the same time.
type concurrencyProbe struct {
	stubProvider

	mu       sync.Mutex
	inFlight int
	release  chan struct{}
}

func (c *concurrencyProbe) Create(_ context.Context, _ string, desired *provider.Resource) (string, error) {
	c.mu.Lock()
	c.inFlight++
	c.mu.Unlock()

	<-c.release
	return "ID_" + desired.Name, nil
}

func (c *concurrencyProbe) Update(context.Context, string, string, *provider.Resource) error {
	return nil
}

// waitForConcurrent spins until n calls are in flight together.
func (c *concurrencyProbe) waitForConcurrent(n int) {
	for {
		c.mu.Lock()
		reached := c.inFlight >= n
		c.mu.Unlock()
		if reached {
			return
		}
	}
}
