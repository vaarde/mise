package cmd

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyJSONEmitsMachineResult(t *testing.T) {
	dir := t.TempDir()
	fetchedWorkspace(t, dir)
	editTaxRate(t, dir, "4.5", "5.0")

	previous := applyJSON
	applyJSON = true
	t.Cleanup(func() { applyJSON = previous })

	out, err := runApplyInWorkspace(t, dir, true, "", "")
	require.NoError(t, err)

	var result applyJSONEnvelope
	require.NoError(t, json.Unmarshal([]byte(out), &result), out)
	assert.Equal(t, "success", result.Status)
	assert.Empty(t, result.Created)
	assert.Equal(t, []string{"fakepos_tax.state_tax"}, result.Updated)
	assert.Empty(t, result.Failed)
}
