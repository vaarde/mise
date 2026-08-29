package output

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/vaarde/mise/internal/engine"
)

func TestPrintApplyResultSuccess(t *testing.T) {
	var buf bytes.Buffer

	PrintApplyResult(&buf, &engine.ApplyResult{
		Created: []string{"square_catalog_tax.summer_promo"},
		Updated: []string{"square_catalog_tax.ga_state_sales_tax"},
	})

	out := buf.String()
	assert.Contains(t, out, "+ square_catalog_tax.summer_promo")
	assert.Contains(t, out, "~ square_catalog_tax.ga_state_sales_tax")
	assert.Contains(t, out, "Apply complete.")
	assert.Contains(t, out, "1 added, 1 changed, 0 destroyed.")
}

func TestPrintApplyResultNamesEveryFailure(t *testing.T) {
	var buf bytes.Buffer

	PrintApplyResult(&buf, &engine.ApplyResult{
		Created: []string{"square_catalog_tax.worked"},
		Failed: []engine.ApplyFailure{
			{
				FullName: "square_catalog_tax.nashville_tax",
				Action:   engine.ActionUpdate,
				Err:      fmt.Errorf("INVALID_VALUE"),
				Message:  "INVALID_VALUE: percentage out of range",
			},
		},
	})

	out := buf.String()

	// An operator whose apply stopped part-way needs to know which
	// resource failed and why, not just that something went wrong.
	assert.Contains(t, out, "Failed:")
	assert.Contains(t, out, "square_catalog_tax.nashville_tax")
	assert.Contains(t, out, "percentage out of range")

	assert.Contains(t, out, "Apply incomplete.")
	assert.Contains(t, out, "1 added, 0 changed, 1 failed.")
	assert.Contains(t, out, "run 'mise apply' again",
		"the operator should be told the retry is safe")
	assert.NotContains(t, out, "Apply complete.")
}

func TestPrintApplyResultNothingApplied(t *testing.T) {
	var buf bytes.Buffer
	PrintApplyResult(&buf, &engine.ApplyResult{})

	assert.Contains(t, buf.String(), "0 added, 0 changed, 0 destroyed.")
}

func TestPrintApplyResultHandlesNil(t *testing.T) {
	var buf bytes.Buffer
	PrintApplyResult(&buf, nil)
	assert.Empty(t, buf.String())
}
