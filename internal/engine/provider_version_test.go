package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
)

func TestPlanAndApplyUseObservedProviderVersion(t *testing.T) {
	f := newPlanFixture()
	f.liveTax("TAX_1", "Nashville City Tax", "3.25", "LOC_NSH")
	f.tracked("square_catalog_tax.nashville_city_tax", "TAX_1", "LOC_NSH")
	f.stateFile.Resources["square_catalog_tax.nashville_city_tax"].Version = "stale-state-version"

	f.declared = []config.ResourceDef{{
		Type: "square_catalog_tax",
		Name: "nashville_city_tax",
		Locations: []string{"LOC_NSH"},
		Properties: map[string]interface{}{
			"name": "Nashville City Tax",
			"percentage": "2.75",
			"enabled": true,
		},
	}}

	plan := f.run(t, PlanOptions{})
	require.Len(t, plan.Changes, 1)
	change := plan.Changes[0]
	assert.Equal(t, "1", change.ProviderVersion, "plan must capture the live provider version, not state")

	ops, failures := buildOperations(Wave{change}, f.stateFile)
	require.Empty(t, failures)
	require.Len(t, ops, 1)
	assert.Equal(t, "1", ops[0].Resource.Version, "apply must use the exact version approved in the plan")
}
