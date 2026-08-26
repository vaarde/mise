package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
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
}

// Global flags
var (
	configFile string
	verbose    bool
)

func init() {
	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "mise.yaml", "path to configuration file")
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")
}

// Execute runs the root command. Called from main.go.
func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
