package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fatih/color"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

// Printer tests assert on text, so colors are disabled for them.
func init() { color.NoColor = true }

func testPrinter() (*PlanPrinter, *bytes.Buffer) {
	var buf bytes.Buffer
	printer := NewPlanPrinter(&buf, []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta"},
		{ID: "LOC_NSH", Name: "Nashville"},
		{ID: "LOC_SAV", Name: "Savannah"},
	})
	return printer, &buf
}

func TestPrintNoChanges(t *testing.T) {
	printer, buf := testPrinter()
	printer.Print(&engine.PlanResult{})

	assert.Contains(t, buf.String(), "No changes.")
	assert.NotContains(t, buf.String(), "Plan:")
}

func TestPrintUpdateMatchesThePRDShape(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action:       engine.ActionUpdate,
				ResourceType: "square_catalog_tax",
				ResourceName: "ga_state_sales_tax",
				LocationIDs:  []string{"LOC_ATL", "LOC_NSH", "LOC_SAV"},
				Diffs: []engine.PropertyDiff{
					{Path: "percentage", OldValue: "4.0", NewValue: "4.5"},
				},
			}},
			Summary: engine.PlanSummary{ToUpdate: 1},
		},
	})

	out := buf.String()
	assert.Contains(t, out, "Mise will perform the following actions:")
	assert.Contains(t, out, "~ square_catalog_tax.ga_state_sales_tax (3 locations)")
	assert.Contains(t, out, `percentage: "4.0" → "4.5"`)
	assert.Contains(t, out, "Plan: 0 to add, 1 to change, 0 to destroy.",
		"the summary line counts adds, changes and destroys")
}

func TestPrintSummaryCounts(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{
				{Action: engine.ActionCreate, ResourceType: "t", ResourceName: "a"},
				{Action: engine.ActionUpdate, ResourceType: "t", ResourceName: "b"},
			},
			Summary: engine.PlanSummary{ToCreate: 3, ToUpdate: 2, ToDelete: 1},
		},
	})

	assert.Contains(t, buf.String(), "Plan: 3 to add, 2 to change, 1 to destroy.")
}

func TestPrintCreateOmitsTheArrow(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action:       engine.ActionCreate,
				ResourceType: "square_catalog_item",
				ResourceName: "summer_lemonade",
				LocationIDs:  []string{"LOC_ATL", "LOC_NSH"},
				Diffs: []engine.PropertyDiff{
					{Path: "name", NewValue: "Summer Lemonade"},
					{Path: "price", NewValue: 450},
				},
			}},
			Summary: engine.PlanSummary{ToCreate: 1},
		},
	})

	out := buf.String()
	assert.Contains(t, out, "+ square_catalog_item.summer_lemonade (2 locations)")
	assert.Contains(t, out, `name  = "Summer Lemonade"`)
	assert.Contains(t, out, "price = 450")
	assert.NotContains(t, out, "→", "a create has no previous value to point away from")
}

func TestPrintNamesASingleLocation(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action:       engine.ActionUpdate,
				ResourceType: "t", ResourceName: "x",
				LocationIDs: []string{"LOC_ATL"},
				Diffs:       []engine.PropertyDiff{{Path: "a", OldValue: "1", NewValue: "2"}},
			}},
			Summary: engine.PlanSummary{ToUpdate: 1},
		},
	})

	assert.Contains(t, buf.String(), "(Atlanta)",
		"one location should be named, not counted")
}

func TestPrintFallsBackToLocationID(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action: engine.ActionUpdate, ResourceType: "t", ResourceName: "x",
				LocationIDs: []string{"LOC_UNKNOWN"},
				Diffs:       []engine.PropertyDiff{{Path: "a", OldValue: "1", NewValue: "2"}},
			}},
			Summary: engine.PlanSummary{ToUpdate: 1},
		},
	})

	assert.Contains(t, buf.String(), "(LOC_UNKNOWN)")
}

func TestPrintAlignsPropertyNames(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action: engine.ActionUpdate, ResourceType: "t", ResourceName: "x",
				Diffs: []engine.PropertyDiff{
					{Path: "a", OldValue: "1", NewValue: "2"},
					{Path: "much_longer_name", OldValue: "3", NewValue: "4"},
				},
			}},
			Summary: engine.PlanSummary{ToUpdate: 1},
		},
	})

	lines := strings.Split(buf.String(), "\n")
	var colA, colB int
	for _, line := range lines {
		if strings.Contains(line, "a ") && strings.Contains(line, "→") {
			colA = strings.Index(line, ":")
		}
		if strings.Contains(line, "much_longer_name") {
			colB = strings.Index(line, ":")
		}
	}
	require.NotZero(t, colA)
	require.NotZero(t, colB)
	assert.Equal(t, colA, colB, "property values should line up in one column")
}

func TestFormatValue(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"string is quoted", "4.5", `"4.5"`},
		{"empty string stays visible", "", `""`},
		{"number", 450, "450"},
		{"float", 4.5, "4.5"},
		{"bool", true, "true"},
		{"nil", nil, "null"},
		{"string slice", []string{"LOC_A", "LOC_B"}, `["LOC_A", "LOC_B"]`},
		{"interface slice", []interface{}{"a", 1}, `["a", 1]`},
		{
			"map is inlined with sorted keys",
			map[string]interface{}{"currency": "USD", "amount": 450},
			`{amount: 450, currency: "USD"}`,
		},
		{
			"nested structures",
			[]interface{}{map[string]interface{}{"name": "Regular"}},
			`[{name: "Regular"}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, formatValue(tc.value))
		})
	}
}

func TestFormatValueQuotingMakesWhitespaceVisible(t *testing.T) {
	// A trailing space is exactly the kind of change that is invisible
	// without quoting, and exactly the kind that breaks a tax lookup.
	assert.Equal(t, `"GA Tax "`, formatValue("GA Tax "))
}

func TestPrintLocationScopeChange(t *testing.T) {
	printer, buf := testPrinter()

	printer.Print(&engine.PlanResult{
		Plan: engine.Plan{
			Changes: []engine.ResourceChange{{
				Action: engine.ActionUpdate, ResourceType: "square_catalog_tax", ResourceName: "ga_tax",
				LocationIDs: []string{"LOC_ATL", "LOC_NSH"},
				Diffs: []engine.PropertyDiff{{
					Path:     engine.LocationsProperty,
					OldValue: []string{"LOC_ATL"},
					NewValue: []string{"LOC_ATL", "LOC_NSH"},
				}},
			}},
			Summary: engine.PlanSummary{ToUpdate: 1},
		},
	})

	assert.Contains(t, buf.String(), `locations: ["LOC_ATL"] → ["LOC_ATL", "LOC_NSH"]`)
}
