package cmd

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/version"
)

var versionJSON bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the Mise version",
	Long: `Prints the version, git commit, and platform of this build.

Include this output in bug reports — a configuration problem and a
version-specific bug look identical without it.`,
	RunE: runVersion,
}

func init() {
	versionCmd.Flags().BoolVar(&versionJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(versionCmd)
}

func runVersion(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	info := version.Current()

	if versionJSON {
		encoder := json.NewEncoder(out)
		encoder.SetIndent("", "  ")
		return encoder.Encode(info)
	}

	fmt.Fprint(out, info.Detailed())
	return nil
}
