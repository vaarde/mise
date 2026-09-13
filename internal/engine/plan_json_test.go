package engine

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/config"
)

func TestPlanNoChangesSerializesEmptyChangesArray(t *testing.T) {
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
	encoded, err := json.Marshal(plan)
	require.NoError(t, err)

	assert.Contains(t, string(encoded), `"changes":[]`)
	assert.NotContains(t, string(encoded), `"changes":null`)
}
