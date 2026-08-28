// Package output formats Mise's command-line output — colored plan
// diffs, drift reports, and cross-location comparison tables.
package output

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/fatih/color"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

// Colors follow the convention every operator already knows from
// Terraform and from diff itself: green adds, yellow changes, red
// destroys.
var (
	createColor = color.New(color.FgGreen)
	updateColor = color.New(color.FgYellow)
	deleteColor = color.New(color.FgRed)
	dimColor    = color.New(color.Faint)
	boldColor   = color.New(color.Bold)
)

// PlanPrinter renders a plan to a writer.
type PlanPrinter struct {
	Out io.Writer

	// LocationNames maps a location ID to its human name, so a plan can
	// say "Atlanta" instead of "L7914T7JFEDXB".
	LocationNames map[string]string
}

// NewPlanPrinter builds a printer that can name the given locations.
func NewPlanPrinter(out io.Writer, locations []provider.Location) *PlanPrinter {
	names := make(map[string]string, len(locations))
	for _, l := range locations {
		names[l.ID] = l.Name
	}
	return &PlanPrinter{Out: out, LocationNames: names}
}

// Print renders the whole plan, including the summary line.
func (p *PlanPrinter) Print(plan *engine.PlanResult) {
	if !plan.HasChanges() {
		fmt.Fprintln(p.Out)
		fmt.Fprintln(p.Out, "No changes. Your configuration matches the live POS.")
		return
	}

	fmt.Fprintln(p.Out)
	fmt.Fprintln(p.Out, "Mise will perform the following actions:")
	fmt.Fprintln(p.Out)

	for _, change := range plan.Changes {
		p.printChange(change)
	}

	p.printSummary(plan.Summary)
}

// printChange renders one resource and its property diffs.
func (p *PlanPrinter) printChange(change engine.ResourceChange) {
	tint := colorFor(change.Action)

	header := fmt.Sprintf("  %s %s", change.Action.Symbol(), change.FullName())
	if scope := p.describeLocations(change.LocationIDs); scope != "" {
		header += " " + scope
	}
	fmt.Fprintln(p.Out, tint.Sprint(header))

	width := 0
	for _, diff := range change.Diffs {
		if len(diff.Path) > width {
			width = len(diff.Path)
		}
	}

	for _, diff := range change.Diffs {
		p.printDiff(change.Action, diff, width)
	}

	fmt.Fprintln(p.Out)
}

// printDiff renders one property line, aligned on the property name.
func (p *PlanPrinter) printDiff(action engine.Action, diff engine.PropertyDiff, width int) {
	label := fmt.Sprintf("%-*s", width, diff.Path)

	// On a create there is no previous value to show, so a single column
	// reads better than "<none> → value" on every line.
	if action == engine.ActionCreate || diff.OldValue == nil {
		fmt.Fprintf(p.Out, "      %s = %s\n", label, createColor.Sprint(formatValue(diff.NewValue)))
		return
	}

	fmt.Fprintf(p.Out, "      %s: %s %s %s\n",
		label,
		deleteColor.Sprint(formatValue(diff.OldValue)),
		dimColor.Sprint("→"),
		createColor.Sprint(formatValue(diff.NewValue)),
	)
}

// printSummary renders the closing count line.
func (p *PlanPrinter) printSummary(summary engine.PlanSummary) {
	fmt.Fprintf(p.Out, "%s %s, %s, %s.\n",
		boldColor.Sprint("Plan:"),
		createColor.Sprintf("%d to add", summary.ToCreate),
		updateColor.Sprintf("%d to change", summary.ToUpdate),
		deleteColor.Sprintf("%d to destroy", summary.ToDelete),
	)
}

// describeLocations renders the scope of a change, e.g. "(3 locations)"
// or "(Atlanta)" when it is a single named one.
func (p *PlanPrinter) describeLocations(locationIDs []string) string {
	switch len(locationIDs) {
	case 0:
		return ""
	case 1:
		if name := p.LocationNames[locationIDs[0]]; name != "" {
			return fmt.Sprintf("(%s)", name)
		}
		return fmt.Sprintf("(%s)", locationIDs[0])
	default:
		return fmt.Sprintf("(%d locations)", len(locationIDs))
	}
}

// colorFor maps an action to its color.
func colorFor(action engine.Action) *color.Color {
	switch action {
	case engine.ActionCreate:
		return createColor
	case engine.ActionUpdate:
		return updateColor
	case engine.ActionDelete:
		return deleteColor
	default:
		return dimColor
	}
}

// formatValue renders a property value on one line.
//
// Plan output is scanned, not parsed, so values are rendered compactly:
// strings quoted so trailing spaces and empty values are visible, and
// nested structures inlined rather than sprawling over the terminal.
func formatValue(value interface{}) string {
	switch typed := value.(type) {
	case nil:
		return "null"

	case string:
		return fmt.Sprintf("%q", typed)

	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprintf("%v", typed)

	case []string:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, fmt.Sprintf("%q", item))
		}
		return "[" + strings.Join(parts, ", ") + "]"

	case []interface{}:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			parts = append(parts, formatValue(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"

	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)

		parts := make([]string, 0, len(keys))
		for _, key := range keys {
			parts = append(parts, fmt.Sprintf("%s: %s", key, formatValue(typed[key])))
		}
		return "{" + strings.Join(parts, ", ") + "}"

	default:
		return fmt.Sprintf("%v", typed)
	}
}
