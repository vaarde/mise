package output

import (
	"fmt"
	"io"

	"github.com/vaarde/mise/internal/engine"
)

// PrintApplyResult reports what an apply actually did.
//
// On a clean run this is one line. On a partial failure it names every
// resource that failed and why — an operator whose apply stopped at 38
// of 40 locations needs to know which two, not just that something went
// wrong.
func PrintApplyResult(out io.Writer, result *engine.ApplyResult) {
	if result == nil {
		return
	}

	fmt.Fprintln(out)

	if len(result.Failed) > 0 {
		fmt.Fprintln(out, boldColor.Sprint("Failed:"))
		for _, failure := range result.Failed {
			fmt.Fprintf(out, "  %s %s\n",
				deleteColor.Sprintf("%s %s", failure.Action.Symbol(), failure.FullName),
				dimColor.Sprint(failure.Message))
		}
		fmt.Fprintln(out)
	}

	for _, name := range result.Created {
		fmt.Fprintf(out, "  %s %s\n", createColor.Sprint("+"), name)
	}
	for _, name := range result.Updated {
		fmt.Fprintf(out, "  %s %s\n", updateColor.Sprint("~"), name)
	}
	if result.Total() > 0 {
		fmt.Fprintln(out)
	}

	summary := fmt.Sprintf("%s %s, %s, %s.",
		boldColor.Sprint("Apply complete."),
		createColor.Sprintf("%d added", len(result.Created)),
		updateColor.Sprintf("%d changed", len(result.Updated)),
		deleteColor.Sprint("0 destroyed"),
	)

	if len(result.Failed) > 0 {
		summary = fmt.Sprintf("%s %s, %s, %s.",
			boldColor.Sprint("Apply incomplete."),
			createColor.Sprintf("%d added", len(result.Created)),
			updateColor.Sprintf("%d changed", len(result.Updated)),
			deleteColor.Sprintf("%d failed", len(result.Failed)),
		)
	}

	fmt.Fprintln(out, summary)

	if len(result.Failed) > 0 {
		fmt.Fprintln(out,
			"\nFix the errors above and run 'mise apply' again — it will only attempt what is left.")
	}
}
