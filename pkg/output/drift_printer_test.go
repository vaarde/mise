package output

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

func testDriftPrinter() (*DriftPrinter, *bytes.Buffer) {
	var buf bytes.Buffer
	printer := NewDriftPrinter(&buf, []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta"},
		{ID: "LOC_NSH", Name: "Nashville"},
	})
	return printer, &buf
}

func TestPrintNoDrift(t *testing.T) {
	printer, buf := testDriftPrinter()
	printer.Print(&engine.DriftResult{Checked: 12})

	out := buf.String()
	assert.Contains(t, out, "No drift.")
	assert.Contains(t, out, "12 resources match")
	assert.NotContains(t, out, "⚠")
}

func TestPrintNoDriftSingular(t *testing.T) {
	printer, buf := testDriftPrinter()
	printer.Print(&engine.DriftResult{Checked: 1})

	assert.Contains(t, buf.String(), "1 resource matches")
}

func TestPrintDriftMatchesThePRDShape(t *testing.T) {
	printer, buf := testDriftPrinter()

	printer.Print(&engine.DriftResult{
		Drifted: []engine.ResourceDrift{{
			FullName:    "square_catalog_tax.tn_sales_tax",
			Reason:      engine.DriftChanged,
			LocationIDs: []string{"LOC_NSH"},
			Diffs: []engine.PropertyDiff{
				{Path: "percentage", OldValue: "9.75", NewValue: "9.25"},
			},
		}},
		Checked: 1,
	})

	out := buf.String()
	assert.Contains(t, out, "Drift detected:")
	assert.Contains(t, out, "~ square_catalog_tax.tn_sales_tax (Nashville)")
	assert.Contains(t, out, `percentage: "9.75" (expected) → "9.25" (actual)`)
	assert.Contains(t, out, "⚠ Changed outside of Mise")
	assert.Contains(t, out, "1 resource drifted across 1 location.")
}

func TestPrintDriftAcrossSeveralLocations(t *testing.T) {
	printer, buf := testDriftPrinter()

	printer.Print(&engine.DriftResult{
		Drifted: []engine.ResourceDrift{
			{
				FullName: "square_catalog_tax.tn_sales_tax", Reason: engine.DriftChanged,
				LocationIDs: []string{"LOC_NSH"},
				Diffs:       []engine.PropertyDiff{{Path: "percentage", OldValue: "9.75", NewValue: "9.25"}},
			},
			{
				FullName: "square_catalog_discount.happy_hour", Reason: engine.DriftChanged,
				LocationIDs: []string{"LOC_ATL"},
				Diffs:       []engine.PropertyDiff{{Path: "percentage", OldValue: "15", NewValue: "20"}},
			},
		},
		Checked: 5,
	})

	assert.Contains(t, buf.String(), "2 resources drifted across 2 locations.")
}

func TestPrintDeletedResource(t *testing.T) {
	printer, buf := testDriftPrinter()

	printer.Print(&engine.DriftResult{
		Drifted: []engine.ResourceDrift{{
			FullName:    "square_catalog_tax.retired_tax",
			Reason:      engine.DriftDeleted,
			LocationIDs: []string{"LOC_ATL", "LOC_NSH"},
		}},
		Checked: 1,
	})

	out := buf.String()
	assert.Contains(t, out, "- square_catalog_tax.retired_tax (2 locations)")
	assert.Contains(t, out, "no longer exists on the POS")
	assert.Contains(t, out, "⚠ Deleted outside of Mise")
}

func TestPrintDriftShowsTheBaseline(t *testing.T) {
	printer, buf := testDriftPrinter()

	printer.Print(&engine.DriftResult{
		Checked:   3,
		LastFetch: "2026-09-15 14:23:00 UTC",
	})
	assert.Contains(t, buf.String(), "last fetched 2026-09-15 14:23:00 UTC")

	// An apply is more recent than a fetch, so it wins as the baseline.
	printer2, buf2 := testDriftPrinter()
	printer2.Print(&engine.DriftResult{
		Checked:   3,
		LastFetch: "2026-09-15 14:23:00 UTC",
		LastApply: "2026-09-16 09:00:00 UTC",
	})
	assert.Contains(t, buf2.String(), "last applied 2026-09-16")
	assert.NotContains(t, buf2.String(), "last fetched")
}

func TestPrintDriftSuggestsWhatToDoNext(t *testing.T) {
	printer, buf := testDriftPrinter()

	printer.Print(&engine.DriftResult{
		Drifted: []engine.ResourceDrift{{
			FullName: "t.x", Reason: engine.DriftChanged, LocationIDs: []string{"LOC_ATL"},
			Diffs: []engine.PropertyDiff{{Path: "a", OldValue: "1", NewValue: "2"}},
		}},
	})

	out := buf.String()
	assert.Contains(t, out, "mise plan")
	assert.Contains(t, out, "mise apply")
	assert.Contains(t, out, "mise fetch",
		"the operator should be told they can accept the live values instead")
}

func TestPrintDriftHandlesNil(t *testing.T) {
	printer, buf := testDriftPrinter()
	printer.Print(nil)
	assert.Empty(t, buf.String())
}

func TestPrintDriftJSON(t *testing.T) {
	var buf bytes.Buffer

	err := PrintDriftJSON(&buf, &engine.DriftResult{
		Checked: 4,
		Drifted: []engine.ResourceDrift{{
			FullName:     "square_catalog_tax.tn_sales_tax",
			ResourceType: "square_catalog_tax",
			ResourceName: "tn_sales_tax",
			ProviderID:   "TAX_1",
			Reason:       engine.DriftChanged,
			LocationIDs:  []string{"LOC_NSH"},
			Diffs:        []engine.PropertyDiff{{Path: "percentage", OldValue: "9.75", NewValue: "9.25"}},
		}},
	})
	require.NoError(t, err)

	var parsed struct {
		Checked int `json:"checked"`
		Drifted []struct {
			FullName   string `json:"full_name"`
			ProviderID string `json:"provider_id"`
			Reason     string `json:"reason"`
			Diffs      []struct {
				Path     string      `json:"path"`
				OldValue interface{} `json:"old_value"`
				NewValue interface{} `json:"new_value"`
			} `json:"diffs"`
		} `json:"drifted"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))

	assert.Equal(t, 4, parsed.Checked)
	require.Len(t, parsed.Drifted, 1)
	assert.Equal(t, "square_catalog_tax.tn_sales_tax", parsed.Drifted[0].FullName)
	assert.Equal(t, "TAX_1", parsed.Drifted[0].ProviderID)
	assert.Equal(t, "changed", parsed.Drifted[0].Reason)
	assert.Equal(t, "9.75", parsed.Drifted[0].Diffs[0].OldValue)
}

func TestPrintDriftJSONWhenClean(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, PrintDriftJSON(&buf, &engine.DriftResult{
		Checked: 7,
		Drifted: []engine.ResourceDrift{},
	}))

	// The JSON contract guarantees an array, not null, for a clean report.
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &parsed))
	assert.Equal(t, float64(7), parsed["checked"])
	assert.Equal(t, []interface{}{}, parsed["drifted"])
}
