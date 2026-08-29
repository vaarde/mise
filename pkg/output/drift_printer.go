package output

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/vaarde/mise/internal/engine"
	"github.com/vaarde/mise/internal/provider"
)

// DriftPrinter renders a drift report.
type DriftPrinter struct {
	Out           io.Writer
	LocationNames map[string]string
}

// NewDriftPrinter builds a printer that can name the given locations.
func NewDriftPrinter(out io.Writer, locations []provider.Location) *DriftPrinter {
	names := make(map[string]string, len(locations))
	for _, l := range locations {
		names[l.ID] = l.Name
	}
	return &DriftPrinter{Out: out, LocationNames: names}
}

// Print renders the report.
func (p *DriftPrinter) Print(result *engine.DriftResult) {
	if result == nil {
		return
	}

	fmt.Fprintln(p.Out)

	if !result.HasDrift() {
		fmt.Fprintf(p.Out, "No drift. %s %s the last known configuration.\n",
			pluralizeCount(result.Checked, "resource", "resources"),
			matchVerb(result.Checked))
		p.printBaseline(result)
		return
	}

	fmt.Fprintln(p.Out, boldColor.Sprint("Drift detected:"))
	fmt.Fprintln(p.Out)

	for _, drift := range result.Drifted {
		p.printDrift(drift)
	}

	fmt.Fprintf(p.Out, "%s drifted across %s.\n",
		pluralizeCount(len(result.Drifted), "resource", "resources"),
		pluralizeCount(len(result.AffectedLocations()), "location", "locations"))

	p.printBaseline(result)

	fmt.Fprintln(p.Out,
		"\nRun 'mise plan' to see how your config files differ, then 'mise apply' to\nrestore them, or 'mise fetch' to accept the live values as the new baseline.")
}

// printDrift renders one drifted resource.
func (p *DriftPrinter) printDrift(drift engine.ResourceDrift) {
	scope := p.describeLocations(drift.LocationIDs)

	if drift.Reason == engine.DriftDeleted {
		fmt.Fprintln(p.Out, deleteColor.Sprintf("  - %s %s", drift.FullName, scope))
		fmt.Fprintf(p.Out, "      %s\n", dimColor.Sprint("no longer exists on the POS"))
		fmt.Fprintf(p.Out, "      %s\n\n", updateColor.Sprint("⚠ Deleted outside of Mise"))
		return
	}

	fmt.Fprintln(p.Out, updateColor.Sprintf("  ~ %s %s", drift.FullName, scope))

	width := 0
	for _, diff := range drift.Diffs {
		if len(diff.Path) > width {
			width = len(diff.Path)
		}
	}

	for _, diff := range drift.Diffs {
		// "expected" is what Mise recorded; "actual" is what the POS
		// says now. Labelling them beats a bare arrow, because the
		// direction of a drift is the whole point.
		fmt.Fprintf(p.Out, "      %-*s: %s %s %s\n",
			width, diff.Path,
			createColor.Sprintf("%s (expected)", formatValue(diff.OldValue)),
			dimColor.Sprint("→"),
			deleteColor.Sprintf("%s (actual)", formatValue(diff.NewValue)),
		)
	}

	fmt.Fprintf(p.Out, "      %s\n\n", updateColor.Sprint("⚠ Changed outside of Mise"))
}

// printBaseline says when state was last known good, so the operator
// knows what window the drift happened in.
func (p *DriftPrinter) printBaseline(result *engine.DriftResult) {
	switch {
	case result.LastApply != "":
		fmt.Fprintf(p.Out, "%s\n", dimColor.Sprintf("Baseline: last applied %s", result.LastApply))
	case result.LastFetch != "":
		fmt.Fprintf(p.Out, "%s\n", dimColor.Sprintf("Baseline: last fetched %s", result.LastFetch))
	}
}

// describeLocations renders the scope of a drift.
func (p *DriftPrinter) describeLocations(locationIDs []string) string {
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

// PrintDriftJSON writes the report as JSON, for scheduled checks and
// alerting that consume it programmatically.
func PrintDriftJSON(out io.Writer, result *engine.DriftResult) error {
	encoder := json.NewEncoder(out)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

// pluralizeCount renders "1 resource" / "3 resources".
func pluralizeCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

// matchVerb agrees with the count.
func matchVerb(n int) string {
	if n == 1 {
		return "matches"
	}
	return "match"
}
