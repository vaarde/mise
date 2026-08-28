package engine

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// planFixture wires up the pieces every plan test needs.
type planFixture struct {
	stub      *stubProvider
	root      *config.RootConfig
	declared  []config.ResourceDef
	stateFile *state.State
}

func newPlanFixture() *planFixture {
	return &planFixture{
		stub: &stubProvider{
			locations: []provider.Location{
				{ID: "LOC_ATL", Name: "Atlanta", State: "GA"},
				{ID: "LOC_NSH", Name: "Nashville", State: "TN"},
			},
			types:     []string{"square_catalog_tax", "square_catalog_category", "square_catalog_item"},
			resources: map[string][]*provider.Resource{},
		},
		root: &config.RootConfig{
			Version: "1",
			LocationGroups: map[string]config.LocationGroup{
				"all":     {Filter: "*"},
				"georgia": {Filter: map[string]interface{}{"state": "GA"}},
			},
		},
		stateFile: state.New("stub"),
	}
}

func (f *planFixture) run(t *testing.T, opts PlanOptions) *PlanResult {
	t.Helper()

	plan, err := ComputePlan(context.Background(), f.stub, f.root, f.declared, f.stateFile, opts)
	require.NoError(t, err)
	return plan
}

// liveTax registers a tax as it exists on the POS at the given locations.
func (f *planFixture) liveTax(id, name, percentage string, locationIDs ...string) {
	for _, locationID := range locationIDs {
		key := "square_catalog_tax@" + locationID
		f.stub.resources[key] = append(f.stub.resources[key], &provider.Resource{
			Type:       "square_catalog_tax",
			Name:       name,
			ProviderID: id,
			Version:    "1",
			Properties: map[string]interface{}{
				"name":       name,
				"percentage": percentage,
				"enabled":    true,
			},
		})
	}
}

// liveCategory registers a category as it exists on the POS.
func (f *planFixture) liveCategory(id, name string, locationIDs ...string) {
	for _, locationID := range locationIDs {
		key := "square_catalog_category@" + locationID
		f.stub.resources[key] = append(f.stub.resources[key], &provider.Resource{
			Type:       "square_catalog_category",
			Name:       name,
			ProviderID: id,
			Properties: map[string]interface{}{"name": name},
		})
	}
}

// tracked records a resource in state, as a previous fetch or apply would.
func (f *planFixture) tracked(fullName, providerID string, locationIDs ...string) {
	f.stateFile.Resources[fullName] = &state.ResourceState{
		ProviderID: providerID,
		Locations:  locationIDs,
		LastSynced: time.Now(),
	}
}

func TestPlanNoChanges(t *testing.T) {
	f := newPlanFixture()
	f.liveTax("TAX_1", "GA State Sales Tax", "4.5", "LOC_ATL", "LOC_NSH")
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_1", "LOC_ATL", "LOC_NSH")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{})

	assert.False(t, plan.HasChanges(), "fetch then plan with no edits must report no changes")
	assert.Empty(t, plan.Changes)
}

func TestPlanDetectsPropertyChange(t *testing.T) {
	f := newPlanFixture()
	f.liveTax("TAX_1", "GA State Sales Tax", "4.0", "LOC_ATL", "LOC_NSH")
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_1", "LOC_ATL", "LOC_NSH")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1)
	change := plan.Changes[0]
	assert.Equal(t, ActionUpdate, change.Action)
	assert.Equal(t, "TAX_1", change.ProviderID)
	assert.Equal(t, 1, plan.Summary.ToUpdate)

	require.Len(t, change.Diffs, 1, "only the changed property should be reported")
	assert.Equal(t, "percentage", change.Diffs[0].Path)
	assert.Equal(t, "4.0", change.Diffs[0].OldValue)
	assert.Equal(t, "4.5", change.Diffs[0].NewValue)
}

func TestPlanDetectsNewResource(t *testing.T) {
	f := newPlanFixture()

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "brand_new_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{"name": "Brand New Tax", "percentage": "2.0"},
	}}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1)
	change := plan.Changes[0]
	assert.Equal(t, ActionCreate, change.Action)
	assert.Empty(t, change.ProviderID, "a create has no provider ID yet")
	assert.Equal(t, 1, plan.Summary.ToCreate)
	assert.Equal(t, []string{"LOC_ATL", "LOC_NSH"}, change.LocationIDs)

	// Every property shows as an addition.
	paths := []string{change.Diffs[0].Path, change.Diffs[1].Path}
	assert.ElementsMatch(t, []string{"name", "percentage"}, paths)
	assert.Nil(t, change.Diffs[0].OldValue)
}

func TestPlanRecreatesResourceDeletedOutsideMise(t *testing.T) {
	f := newPlanFixture()
	// State remembers an ID, but the POS no longer has the object.
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_GONE", "LOC_ATL")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{"name": "GA State Sales Tax", "percentage": "4.5"},
	}}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1)
	assert.Equal(t, ActionCreate, plan.Changes[0].Action)
	assert.Empty(t, plan.Changes[0].ProviderID)
}

func TestPlanIgnoresResourcesNotInConfig(t *testing.T) {
	f := newPlanFixture()
	// Two taxes live on the POS; only one is declared.
	f.liveTax("TAX_1", "GA State Sales Tax", "4.5", "LOC_ATL")
	f.liveTax("TAX_2", "Some Other Tax", "9.0", "LOC_ATL")
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_1", "LOC_ATL")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax",
		Locations: []interface{}{"LOC_ATL"},
		Properties: map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{})

	assert.False(t, plan.HasChanges(),
		"Mise manages what the config declares and leaves everything else alone")
	assert.Equal(t, 0, plan.Summary.ToDelete, "plan never proposes a destroy")
}

func TestPlanIgnoresExtraLivePropertiesNotDeclared(t *testing.T) {
	f := newPlanFixture()
	f.stub.resources["square_catalog_tax@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_tax", Name: "GA Tax", ProviderID: "TAX_1",
		Properties: map[string]interface{}{
			"name":              "GA Tax",
			"percentage":        "4.5",
			"calculation_phase": "TAX_SUBTOTAL_PHASE",
			"inclusion_type":    "ADDITIVE",
		},
	}}
	f.tracked("square_catalog_tax.ga_tax", "TAX_1", "LOC_ATL")

	// The config only declares two of the four live properties.
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_tax", Locations: []interface{}{"LOC_ATL"},
		Properties: map[string]interface{}{"name": "GA Tax", "percentage": "4.5"},
	}}

	plan := f.run(t, PlanOptions{})
	assert.False(t, plan.HasChanges(), "undeclared live properties are not drift")
}

func TestPlanDetectsLocationScopeChange(t *testing.T) {
	f := newPlanFixture()
	f.liveTax("TAX_1", "GA State Sales Tax", "4.5", "LOC_ATL")
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_1", "LOC_ATL")

	// Config widens the tax from Georgia only to everywhere.
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1)
	change := plan.Changes[0]
	assert.Equal(t, ActionUpdate, change.Action)

	require.Len(t, change.Diffs, 1)
	assert.Equal(t, LocationsProperty, change.Diffs[0].Path)
	assert.Equal(t, []string{"LOC_ATL"}, change.Diffs[0].OldValue)
	assert.Equal(t, []string{"LOC_ATL", "LOC_NSH"}, change.Diffs[0].NewValue)
}

func TestPlanResolvesLocationGroups(t *testing.T) {
	f := newPlanFixture()

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "georgia_only", Locations: "${group.georgia}",
		Properties: map[string]interface{}{"name": "Georgia Only", "percentage": "1.0"},
	}}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1)
	assert.Equal(t, []string{"LOC_ATL"}, plan.Changes[0].LocationIDs,
		"the georgia group should resolve to the Georgia location only")
}

func TestPlanComparesReferencesByProviderID(t *testing.T) {
	f := newPlanFixture()
	f.stub.resources["square_catalog_item@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_item", Name: "Lemonade", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"name":     "Lemonade",
			"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_1"},
		},
	}}
	f.liveCategory("CAT_1", "Beverages", "LOC_ATL")
	f.tracked("square_catalog_item.lemonade", "ITEM_1", "LOC_ATL")
	f.tracked("square_catalog_category.beverages", "CAT_1", "LOC_ATL")

	f.declared = []config.ResourceDef{
		{Type: "square_catalog_category", Name: "beverages", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{"name": "Beverages"}},
		{Type: "square_catalog_item", Name: "lemonade", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{
				"name":     "Lemonade",
				"category": "ref(square_catalog_category.beverages)",
			}},
	}

	plan := f.run(t, PlanOptions{})
	assert.False(t, plan.HasChanges(),
		"a ref pointing at the same provider ID as the live value is not a change")
}

func TestPlanDetectsChangedReference(t *testing.T) {
	f := newPlanFixture()
	f.stub.resources["square_catalog_item@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_item", Name: "Lemonade", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_OLD"},
		},
	}}
	f.liveCategory("CAT_NEW", "Drinks", "LOC_ATL")
	f.tracked("square_catalog_item.lemonade", "ITEM_1", "LOC_ATL")
	f.tracked("square_catalog_category.drinks", "CAT_NEW", "LOC_ATL")

	f.declared = []config.ResourceDef{
		{Type: "square_catalog_category", Name: "drinks", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{"name": "Drinks"}},
		{Type: "square_catalog_item", Name: "lemonade", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{
				"category": "ref(square_catalog_category.drinks)",
			}},
	}

	plan := f.run(t, PlanOptions{})

	require.Len(t, plan.Changes, 1, "only the item changed; the category matches")
	require.Len(t, plan.Changes[0].Diffs, 1)
	assert.Equal(t, "category", plan.Changes[0].Diffs[0].Path)
	assert.Equal(t, "CAT_OLD", plan.Changes[0].Diffs[0].OldValue)
	assert.Equal(t, "CAT_NEW", plan.Changes[0].Diffs[0].NewValue)
}

func TestPlanRenamingAResourceIsNotAChangeToItsDependents(t *testing.T) {
	// The trap this guards: if plan compared resolved ref *names* rather
	// than provider IDs, renaming a category in YAML would show every
	// item that points at it as changed, even though nothing on the POS
	// differs.
	f := newPlanFixture()
	f.stub.resources["square_catalog_item@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_item", Name: "Lemonade", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_1"},
		},
	}}
	f.liveCategory("CAT_1", "Cold Drinks", "LOC_ATL")
	f.tracked("square_catalog_item.lemonade", "ITEM_1", "LOC_ATL")
	// The same provider ID, now recorded under a different config name.
	f.tracked("square_catalog_category.cold_drinks", "CAT_1", "LOC_ATL")

	f.declared = []config.ResourceDef{
		{Type: "square_catalog_category", Name: "cold_drinks", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{"name": "Cold Drinks"}},
		{Type: "square_catalog_item", Name: "lemonade", Locations: []interface{}{"LOC_ATL"},
			Properties: map[string]interface{}{
				"category": "ref(square_catalog_category.cold_drinks)",
			}},
	}

	plan := f.run(t, PlanOptions{})
	assert.False(t, plan.HasChanges(), "renaming a resource must not fabricate a diff")
}

func TestPlanTreatsNumbersFromYAMLAndAPIAsEqual(t *testing.T) {
	// YAML decodes 450 as int; the Square client decodes it as int64.
	// Without canonicalization every price would look changed.
	f := newPlanFixture()
	f.stub.resources["square_catalog_item@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_item", Name: "Lemonade", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"variations": []interface{}{
				map[string]interface{}{
					"name":        "Regular",
					"price_money": map[string]interface{}{"amount": int64(450), "currency": "USD"},
				},
			},
		},
	}}
	f.tracked("square_catalog_item.lemonade", "ITEM_1", "LOC_ATL")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_item", Name: "lemonade", Locations: []interface{}{"LOC_ATL"},
		Properties: map[string]interface{}{
			"variations": []interface{}{
				map[string]interface{}{
					"name":        "Regular",
					"price_money": map[string]interface{}{"amount": 450, "currency": "USD"},
				},
			},
		},
	}}

	plan := f.run(t, PlanOptions{})
	assert.False(t, plan.HasChanges(), "int(450) and int64(450) are the same price")
}

func TestPlanDetectsPriceChange(t *testing.T) {
	f := newPlanFixture()
	f.stub.resources["square_catalog_item@LOC_ATL"] = []*provider.Resource{{
		Type: "square_catalog_item", Name: "Lemonade", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"variations": []interface{}{
				map[string]interface{}{"name": "Regular",
					"price_money": map[string]interface{}{"amount": int64(450), "currency": "USD"}},
			},
		},
	}}
	f.tracked("square_catalog_item.lemonade", "ITEM_1", "LOC_ATL")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_item", Name: "lemonade", Locations: []interface{}{"LOC_ATL"},
		Properties: map[string]interface{}{
			"variations": []interface{}{
				map[string]interface{}{"name": "Regular",
					"price_money": map[string]interface{}{"amount": 500, "currency": "USD"}},
			},
		},
	}}

	plan := f.run(t, PlanOptions{})
	require.Len(t, plan.Changes, 1)
	assert.Equal(t, "variations", plan.Changes[0].Diffs[0].Path)
}

func TestPlanTargetFiltersToOneResource(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{
		{Type: "square_catalog_tax", Name: "one", Locations: config.GroupAll,
			Properties: map[string]interface{}{"percentage": "1.0"}},
		{Type: "square_catalog_tax", Name: "two", Locations: config.GroupAll,
			Properties: map[string]interface{}{"percentage": "2.0"}},
	}

	plan := f.run(t, PlanOptions{TargetResource: "two"})
	require.Len(t, plan.Changes, 1)
	assert.Equal(t, "two", plan.Changes[0].ResourceName)

	plan = f.run(t, PlanOptions{TargetResource: "square_catalog_tax.one"})
	require.Len(t, plan.Changes, 1)
	assert.Equal(t, "one", plan.Changes[0].ResourceName)
}

func TestPlanTargetRejectsUnknownAndAmbiguous(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{
		{Type: "square_catalog_tax", Name: "dup", Properties: map[string]interface{}{}},
		{Type: "square_catalog_item", Name: "dup", Properties: map[string]interface{}{}},
	}

	_, err := ComputePlan(context.Background(), f.stub, f.root, f.declared, f.stateFile,
		PlanOptions{TargetResource: "nope"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no declared resource matches")

	_, err = ComputePlan(context.Background(), f.stub, f.root, f.declared, f.stateFile,
		PlanOptions{TargetResource: "dup"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ambiguous")
}

func TestPlanLocationFilter(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "t", Locations: config.GroupAll,
		Properties: map[string]interface{}{"percentage": "1.0"},
	}}

	plan := f.run(t, PlanOptions{TargetLocation: "Atlanta"})
	require.Len(t, plan.Locations, 1)
	assert.Equal(t, "LOC_ATL", plan.Locations[0].ID)

	plan = f.run(t, PlanOptions{TargetLocation: "LOC_NSH"})
	require.Len(t, plan.Locations, 1)
	assert.Equal(t, "LOC_NSH", plan.Locations[0].ID)
}

func TestPlanLocationFilterRejectsUnknown(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "t", Properties: map[string]interface{}{},
	}}

	_, err := ComputePlan(context.Background(), f.stub, f.root, f.declared, f.stateFile,
		PlanOptions{TargetLocation: "Miami"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Miami")
	assert.Contains(t, err.Error(), "Atlanta", "the error should list known locations")
}

func TestPlanRejectsDanglingReference(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_item", Name: "lemonade",
		Properties: map[string]interface{}{
			"category": "ref(square_catalog_category.does_not_exist)",
		},
	}}

	_, err := ComputePlan(context.Background(), f.stub, f.root, f.declared, f.stateFile, PlanOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does_not_exist")
}

func TestPlanRecordsPendingRefsForNewDependencies(t *testing.T) {
	// A brand-new category and an item pointing at it: the item's ref
	// cannot resolve yet, which is expected rather than an error.
	f := newPlanFixture()
	f.declared = []config.ResourceDef{
		{Type: "square_catalog_category", Name: "beverages", Locations: config.GroupAll,
			Properties: map[string]interface{}{"name": "Beverages"}},
		{Type: "square_catalog_item", Name: "lemonade", Locations: config.GroupAll,
			Properties: map[string]interface{}{
				"category": "ref(square_catalog_category.beverages)",
			}},
	}

	plan := f.run(t, PlanOptions{})
	assert.Equal(t, 2, plan.Summary.ToCreate)
}

func TestPlanSortsChangesDeterministically(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{
		{Type: "square_catalog_tax", Name: "zebra", Properties: map[string]interface{}{"a": 1}},
		{Type: "square_catalog_item", Name: "apple", Properties: map[string]interface{}{"a": 1}},
		{Type: "square_catalog_tax", Name: "alpha", Properties: map[string]interface{}{"a": 1}},
	}

	plan := f.run(t, PlanOptions{})
	require.Len(t, plan.Changes, 3)
	assert.Equal(t, "square_catalog_item.apple", plan.Changes[0].FullName())
	assert.Equal(t, "square_catalog_tax.alpha", plan.Changes[1].FullName())
	assert.Equal(t, "square_catalog_tax.zebra", plan.Changes[2].FullName())
}

func TestCanonicalNormalizesNumericTypes(t *testing.T) {
	assert.Equal(t, canonical(450), canonical(int64(450)))
	assert.Equal(t, canonical(450), canonical(450.0))
	assert.NotEqual(t, canonical(450), canonical(451))
	assert.NotEqual(t, canonical("450"), canonical(450), "a string is not a number")
}

func TestNormalizeLiveProperties(t *testing.T) {
	props := map[string]interface{}{
		"category": provider.Ref{ResourceType: "t", ProviderID: "ID_1"},
		"tax_ids": []interface{}{
			provider.Ref{ResourceType: "t", ProviderID: "ID_2"},
		},
		"name": "unchanged",
	}

	out := normalizeLiveProperties(props)
	assert.Equal(t, "ID_1", out["category"])
	assert.Equal(t, []interface{}{"ID_2"}, out["tax_ids"])
	assert.Equal(t, "unchanged", out["name"])
}

func TestPlanLocationFilterDoesNotFabricateALocationDiff(t *testing.T) {
	// Regression: narrowing a plan to one location used to compare the
	// full desired location set against a live set read only at that
	// location, reporting a phantom "location added" change.
	f := newPlanFixture()
	f.liveTax("TAX_1", "GA State Sales Tax", "4.5", "LOC_ATL", "LOC_NSH")
	f.tracked("square_catalog_tax.ga_state_sales_tax", "TAX_1", "LOC_ATL", "LOC_NSH")

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "ga_state_sales_tax", Locations: config.GroupAll,
		Properties: map[string]interface{}{
			"name": "GA State Sales Tax", "percentage": "4.5", "enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{TargetLocation: "Atlanta"})
	assert.False(t, plan.HasChanges(),
		"scoping a plan to one location must not invent a change")
}

func TestPlanLocationFilterSkipsResourcesOutOfScope(t *testing.T) {
	f := newPlanFixture()

	// A brand-new tax that only applies in Georgia.
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "georgia_only", Locations: "${group.georgia}",
		Properties: map[string]interface{}{"name": "Georgia Only", "percentage": "1.0"},
	}}

	atlanta := f.run(t, PlanOptions{TargetLocation: "Atlanta"})
	assert.Equal(t, 1, atlanta.Summary.ToCreate, "in scope at Atlanta")

	nashville := f.run(t, PlanOptions{TargetLocation: "Nashville"})
	assert.False(t, nashville.HasChanges(),
		"a Georgia-only tax is out of scope when planning Nashville")
}

func TestPlanLocationFilterNarrowsReportedScope(t *testing.T) {
	f := newPlanFixture()
	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax", Name: "everywhere", Locations: config.GroupAll,
		Properties: map[string]interface{}{"name": "Everywhere", "percentage": "1.0"},
	}}

	plan := f.run(t, PlanOptions{TargetLocation: "Atlanta"})
	require.Len(t, plan.Changes, 1)
	assert.Equal(t, []string{"LOC_ATL"}, plan.Changes[0].LocationIDs,
		"the change should report the locations actually in scope")
}
