package square

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/vaarde/mise/internal/provider"
)

func TestApplyBatchResponseMarksMissingObjectAsError(t *testing.T) {
	outcomes := []provider.WriteOutcome{{Name: "tax.nashville", ProviderID: "TAX_1"}}
	applyBatchResponse(outcomes, map[string]int{}, batchUpsertResponse{})
	require.Error(t, outcomes[0].Err)
}
