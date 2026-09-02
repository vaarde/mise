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

func TestVerifyReportsConvergedLocations(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{
			{ID: "LOC_ATL", Name: "Atlanta"},
			{ID: "LOC_NSH", Name: "Nashville"},
		},
		types: []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_ATL": {{
				Type: "square_catalog_tax", ProviderID: "TAX_1",
				Properties: map[string]interface{}{"name": "State Tax", "percentage": "5.0"},
			}},
			"square_catalog_tax@LOC_NSH": {{
				Type: "square_catalog_tax", ProviderID: "TAX_1",
				Properties: map[string]interface{}{"name": "State Tax", "percentage": "5.0"},
			}},
		},
	}
	st := state.New("stub")
	st.Resources["square_catalog_tax.state_tax"] = &state.ResourceState{
		ProviderID: "TAX_1", LastSynced: time.Now(),
	}
	plan := &PlanResult{
		Plan: Plan{Changes: []ResourceChange{{
			Action: ActionUpdate, ResourceType: "square_catalog_tax", ResourceName: "state_tax",
			ProviderID: "TAX_1", LocationIDs: []string{"LOC_ATL", "LOC_NSH"},
			Desired: map[string]interface{}{"name": "State Tax", "percentage": "5.0"},
		}}},
		Locations: stub.locations,
	}

	var events []VerifyEvent
	result, err := Verify(context.Background(), stub, plan, st, func(event VerifyEvent) error {
		events = append(events, event)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, result.Verified)
	assert.Equal(t, 2, result.Converged)
	assert.Zero(t, result.NonConverged)
	require.Len(t, events, 3)
	assert.Equal(t, "verify_progress", events[0].Type)
	assert.Equal(t, 1, events[0].Verified)
	assert.Equal(t, "verify_complete", events[2].Type)
}

func TestVerifyReportsPropertyMismatch(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_ATL", Name: "Atlanta"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_ATL": {{
				Type: "square_catalog_tax", ProviderID: "TAX_1",
				Properties: map[string]interface{}{"name": "State Tax", "percentage": "4.0"},
			}},
		},
	}
	st := state.New("stub")
	st.Resources["square_catalog_tax.state_tax"] = &state.ResourceState{ProviderID: "TAX_1"}
	plan := &PlanResult{
		Plan: Plan{Changes: []ResourceChange{{
			Action: ActionUpdate, ResourceType: "square_catalog_tax", ResourceName: "state_tax",
			ProviderID: "TAX_1", LocationIDs: []string{"LOC_ATL"},
			Desired: map[string]interface{}{"name": "State Tax", "percentage": "5.0"},
		}}},
		Locations: stub.locations,
	}

	result, err := Verify(context.Background(), stub, plan, st, nil)
	require.NoError(t, err)
	assert.Equal(t, 1, result.NonConverged)
	require.Len(t, result.Locations, 1)
	require.Len(t, result.Locations[0].Issues, 1)
	assert.Equal(t, "percentage", result.Locations[0].Issues[0].Diffs[0].Path)
}
