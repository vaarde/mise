package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/version"
)

// rootCmd is the base command when called without any subcommands.
var rootCmd = &cobra.Command{
	Use:   "mise",
	Short: "Everything in its place, at every location",
	Long: `Mise is a configuration-as-code tool for restaurant POS platforms.

It lets multi-location restaurant operators define, version-control,
and deploy POS configuration (menus, tax rates, service charges,
discounts) as declarative YAML — with plan/apply workflows, drift
detection, and rollback via git.`,

	// Cobra renders this for --version and for `mise -v`-style checks.
	Version: version.Current().String(),

	// A failed API call or a bad token is not a usage mistake — dumping
	// the flag list on top of the error buries it. Cobra still prints
	// usage for genuine flag and argument errors.
	SilenceUsage: true,

	// Execute prints the error itself, once.
	SilenceErrors: true,
}

// Global flags
var (
	configFile string
	verbose    bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "mise.yaml", "path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	// -v is taken by --verbose, so the version flag is long-form only.
	rootCmd.Flags().Bool("version", false, "print the Mise version")
	rootCmd.SetVersionTemplate("{{.Version}}\n")
}

// Execute runs the root command. Called from main.go.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}
	return nil
}
