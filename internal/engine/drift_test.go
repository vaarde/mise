package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// driftFixture wires up a provider and a state file to compare.
type driftFixture struct {
	stub      *stubProvider
	stateFile *state.State
}

func newDriftFixture() *driftFixture {
	return &driftFixture{
		stub: &stubProvider{
			locations: []provider.Location{
				{ID: "LOC_ATL", Name: "Atlanta", State: "GA"},
				{ID: "LOC_NSH", Name: "Nashville", State: "TN"},
			},
			types:     []string{"square_catalog_tax", "square_catalog_category", "square_catalog_item"},
			resources: map[string][]*provider.Resource{},
		},
		stateFile: state.New("stub"),
	}
}

func (f *driftFixture) run(t *testing.T, opts DriftOptions) *DriftResult {
	t.Helper()

	result, err := Drift(context.Background(), f.stub, f.stateFile, opts)
	require.NoError(t, err)
	return result
}

// recorded adds a resource to state, as fetch or apply would have.
func (f *driftFixture) recorded(
	resourceType, name, providerID string,
	properties map[string]interface{},
	locationIDs ...string,
) {
	f.stateFile.Resources[state.ResourceKey(resourceType, name)] = &state.ResourceState{
		ProviderID: providerID,
		Type:       resourceType,
		Name:       name,
		Locations:  locationIDs,
		Properties: properties,
		LastSynced: time.Now(),
	}
}

// onPOS puts a resource on the live provider at the given locations.
func (f *driftFixture) onPOS(
	resourceType, displayName, providerID string,
	properties map[string]interface{},
	locationIDs ...string,
) {
	for _, locationID := range locationIDs {
		key := resourceType + "@" + locationID
		f.stub.resources[key] = append(f.stub.resources[key], &provider.Resource{
			Type:       resourceType,
			Name:       displayName,
			ProviderID: providerID,
			Properties: properties,
		})
	}
}

func TestDriftReportsNothingWhenInSync(t *testing.T) {
	f := newDriftFixture()
	props := map[string]interface{}{"name": "GA Tax", "percentage": "4.5", "enabled": true}

	f.recorded("square_catalog_tax", "ga_tax", "TAX_1", props, "LOC_ATL", "LOC_NSH")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1", props, "LOC_ATL", "LOC_NSH")

	result := f.run(t, DriftOptions{})

	assert.False(t, result.HasDrift())
	assert.Equal(t, 1, result.Checked, "a clean report should say what it looked at")
}

func TestDriftDetectsAChangedValue(t *testing.T) {
	f := newDriftFixture()

	f.recorded("square_catalog_tax", "tn_sales_tax", "TAX_1",
		map[string]interface{}{"name": "TN Sales Tax", "percentage": "9.75"}, "LOC_NSH")
	// Someone edited the rate in the POS dashboard.
	f.onPOS("square_catalog_tax", "TN Sales Tax", "TAX_1",
		map[string]interface{}{"name": "TN Sales Tax", "percentage": "9.25"}, "LOC_NSH")

	result := f.run(t, DriftOptions{})

	require.True(t, result.HasDrift())
	require.Len(t, result.Drifted, 1)

	drift := result.Drifted[0]
	assert.Equal(t, "square_catalog_tax.tn_sales_tax", drift.FullName)
	assert.Equal(t, DriftChanged, drift.Reason)

	require.Len(t, drift.Diffs, 1, "only the changed property should be reported")
	assert.Equal(t, "percentage", drift.Diffs[0].Path)
	assert.Equal(t, "9.75", drift.Diffs[0].OldValue, "expected is what Mise recorded")
	assert.Equal(t, "9.25", drift.Diffs[0].NewValue, "actual is what the POS says now")
}

func TestDriftDetectsADeletedResource(t *testing.T) {
	f := newDriftFixture()
	f.recorded("square_catalog_tax", "ga_tax", "TAX_GONE",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")

	result := f.run(t, DriftOptions{})

	require.Len(t, result.Drifted, 1)
	assert.Equal(t, DriftDeleted, result.Drifted[0].Reason)
	assert.Empty(t, result.Drifted[0].Diffs, "a deleted resource has no property diffs")
}

func TestDriftDetectsALocationScopeChange(t *testing.T) {
	f := newDriftFixture()
	props := map[string]interface{}{"percentage": "4.5"}

	// Mise recorded the tax at both locations; it is now only at one.
	f.recorded("square_catalog_tax", "ga_tax", "TAX_1", props, "LOC_ATL", "LOC_NSH")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1", props, "LOC_ATL")

	result := f.run(t, DriftOptions{})

	require.Len(t, result.Drifted, 1)
	require.Len(t, result.Drifted[0].Diffs, 1)
	assert.Equal(t, LocationsProperty, result.Drifted[0].Diffs[0].Path)
	assert.Equal(t, []string{"LOC_ATL", "LOC_NSH"}, result.Drifted[0].Diffs[0].OldValue)
	assert.Equal(t, []string{"LOC_ATL"}, result.Drifted[0].Diffs[0].NewValue)
}

func TestDriftIgnoresUntrackedProperties(t *testing.T) {
	f := newDriftFixture()

	f.recorded("square_catalog_tax", "ga_tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	// The POS reports more fields than Mise ever recorded.
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1", map[string]interface{}{
		"percentage": "4.5", "calculation_phase": "TAX_SUBTOTAL_PHASE", "enabled": true,
	}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(), "a field Mise never tracked is not drift")
}

func TestDriftIgnoresResourcesNotInState(t *testing.T) {
	f := newDriftFixture()
	f.recorded("square_catalog_tax", "ga_tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")

	// A tax someone created in the dashboard that Mise does not manage.
	f.onPOS("square_catalog_tax", "Someone Elses Tax", "TAX_OTHER",
		map[string]interface{}{"percentage": "1.0"}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(), "Mise reports on what it manages, not everything on the POS")
}

func TestDriftMatchesByProviderIDNotName(t *testing.T) {
	// The trap: a resource renamed in YAML keeps its provider ID, and
	// must not read as deleted-and-recreated.
	f := newDriftFixture()
	props := map[string]interface{}{"percentage": "4.5"}

	f.recorded("square_catalog_tax", "renamed_in_yaml", "TAX_1", props, "LOC_ATL")
	f.onPOS("square_catalog_tax", "Original Display Name", "TAX_1", props, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(), "matching is by provider ID, so a rename is not drift")
}

func TestDriftNormalizesStateWrittenByFetch(t *testing.T) {
	// fetch records references as "ref(type.name)" strings.
	f := newDriftFixture()

	f.recorded("square_catalog_category", "beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	f.recorded("square_catalog_item", "lemonade", "ITEM_1", map[string]interface{}{
		"name":     "Lemonade",
		"category": "ref(square_catalog_category.beverages)",
	}, "LOC_ATL")

	f.onPOS("square_catalog_category", "Beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	f.onPOS("square_catalog_item", "Lemonade", "ITEM_1", map[string]interface{}{
		"name":     "Lemonade",
		"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_1"},
	}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(),
		"a ref string in state and a provider.Ref on the POS are the same reference")
}

func TestDriftNormalizesStateWrittenByApply(t *testing.T) {
	// apply records references as resolved provider IDs, so state's
	// shape depends on which command last wrote it. Both must compare
	// equal against the same live value.
	f := newDriftFixture()

	f.recorded("square_catalog_category", "beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	f.recorded("square_catalog_item", "lemonade", "ITEM_1", map[string]interface{}{
		"name":     "Lemonade",
		"category": "CAT_1",
	}, "LOC_ATL")

	f.onPOS("square_catalog_category", "Beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	f.onPOS("square_catalog_item", "Lemonade", "ITEM_1", map[string]interface{}{
		"name":     "Lemonade",
		"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_1"},
	}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(),
		"a raw provider ID in state and a provider.Ref on the POS are the same reference")
}

func TestDriftDetectsAChangedReference(t *testing.T) {
	f := newDriftFixture()

	f.recorded("square_catalog_category", "beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	f.recorded("square_catalog_item", "lemonade", "ITEM_1", map[string]interface{}{
		"category": "ref(square_catalog_category.beverages)",
	}, "LOC_ATL")

	f.onPOS("square_catalog_category", "Beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")
	// Someone moved the item to a different category.
	f.onPOS("square_catalog_item", "Lemonade", "ITEM_1", map[string]interface{}{
		"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_OTHER"},
	}, "LOC_ATL")

	result := f.run(t, DriftOptions{})

	require.Len(t, result.Drifted, 1)
	assert.Equal(t, "square_catalog_item.lemonade", result.Drifted[0].FullName)
	assert.Equal(t, "category", result.Drifted[0].Diffs[0].Path)
}

func TestDriftTreatsNumericTypesAsEqual(t *testing.T) {
	f := newDriftFixture()

	// State came back through JSON (float64); the API gives int64.
	f.recorded("square_catalog_item", "lemonade", "ITEM_1", map[string]interface{}{
		"price_money": map[string]interface{}{"amount": float64(450), "currency": "USD"},
	}, "LOC_ATL")
	f.onPOS("square_catalog_item", "Lemonade", "ITEM_1", map[string]interface{}{
		"price_money": map[string]interface{}{"amount": int64(450), "currency": "USD"},
	}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(), "the same price decoded two ways is not drift")
}

func TestDriftFiltersByType(t *testing.T) {
	f := newDriftFixture()

	f.recorded("square_catalog_tax", "ga_tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	f.recorded("square_catalog_category", "beverages", "CAT_1",
		map[string]interface{}{"name": "Beverages"}, "LOC_ATL")

	// Both drifted, but only taxes are being checked.
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1",
		map[string]interface{}{"percentage": "9.9"}, "LOC_ATL")
	f.onPOS("square_catalog_category", "Renamed", "CAT_1",
		map[string]interface{}{"name": "Renamed"}, "LOC_ATL")

	result := f.run(t, DriftOptions{ResourceType: "square_catalog_tax"})

	require.Len(t, result.Drifted, 1)
	assert.Equal(t, "square_catalog_tax.ga_tax", result.Drifted[0].FullName)
}

func TestDriftRejectsUnknownType(t *testing.T) {
	f := newDriftFixture()
	f.recorded("square_catalog_tax", "ga_tax", "TAX_1", map[string]interface{}{}, "LOC_ATL")

	_, err := Drift(context.Background(), f.stub, f.stateFile,
		DriftOptions{ResourceType: "square_catalog_pizza"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "square_catalog_pizza")
	assert.Contains(t, err.Error(), "square_catalog_tax", "the error should list the known types")
}

func TestDriftFiltersByLocation(t *testing.T) {
	f := newDriftFixture()
	props := map[string]interface{}{"percentage": "4.5"}

	f.recorded("square_catalog_tax", "atlanta_only", "TAX_ATL", props, "LOC_ATL")
	f.recorded("square_catalog_tax", "nashville_only", "TAX_NSH", props, "LOC_NSH")

	// Neither exists on the POS any more, so both would drift.
	result := f.run(t, DriftOptions{TargetLocation: "Nashville"})

	require.Len(t, result.Drifted, 1)
	assert.Equal(t, "square_catalog_tax.nashville_only", result.Drifted[0].FullName,
		"a resource outside the checked location is out of scope, not drifted")
}

func TestDriftLocationFilterDoesNotFabricateAScopeChange(t *testing.T) {
	f := newDriftFixture()
	props := map[string]interface{}{"percentage": "4.5"}

	f.recorded("square_catalog_tax", "ga_tax", "TAX_1", props, "LOC_ATL", "LOC_NSH")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1", props, "LOC_ATL", "LOC_NSH")

	result := f.run(t, DriftOptions{TargetLocation: "Atlanta"})
	assert.False(t, result.HasDrift(),
		"narrowing the check to one location must not look like a scope change")
}

func TestDriftRequiresState(t *testing.T) {
	f := newDriftFixture()

	_, err := Drift(context.Background(), f.stub, state.New("stub"), DriftOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise fetch")

	_, err = Drift(context.Background(), f.stub, nil, DriftOptions{})
	require.Error(t, err)
}

func TestDriftReportsTheBaseline(t *testing.T) {
	f := newDriftFixture()
	fetched := time.Date(2026, 9, 15, 14, 23, 0, 0, time.UTC)
	f.stateFile.LastFetch = &fetched
	f.recorded("square_catalog_tax", "ga_tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.Contains(t, result.LastFetch, "2026-09-15",
		"the report should say what window the drift happened in")
}

func TestDriftResultsAreSortedAndCounted(t *testing.T) {
	f := newDriftFixture()

	for _, name := range []string{"zebra", "apple", "mango"} {
		f.recorded("square_catalog_tax", name, "TAX_"+name,
			map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	}

	result := f.run(t, DriftOptions{})
	require.Len(t, result.Drifted, 3)

	assert.Equal(t, "square_catalog_tax.apple", result.Drifted[0].FullName)
	assert.Equal(t, "square_catalog_tax.mango", result.Drifted[1].FullName)
	assert.Equal(t, "square_catalog_tax.zebra", result.Drifted[2].FullName)

	assert.Equal(t, []string{"LOC_ATL"}, result.AffectedLocations())
}

func TestDriftSkipsEntriesWithoutAProviderID(t *testing.T) {
	f := newDriftFixture()
	f.stateFile.Resources["square_catalog_tax.never_applied"] = &state.ResourceState{
		Type: "square_catalog_tax", Name: "never_applied",
	}
	f.recorded("square_catalog_tax", "ga_tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")
	f.onPOS("square_catalog_tax", "GA Tax", "TAX_1",
		map[string]interface{}{"percentage": "4.5"}, "LOC_ATL")

	result := f.run(t, DriftOptions{})
	assert.False(t, result.HasDrift(), "a resource never applied cannot have drifted")
	assert.Equal(t, 1, result.Checked)
}
