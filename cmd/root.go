package cmd

import (
	"context"
	"errors"
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

// ExitCodeInterrupted is the conventional status for a command stopped
// by SIGINT: 128 plus the signal number.
const ExitCodeInterrupted = 130

// ExitCoder is an error that carries its own process exit status.
// 'mise drift' uses it so a scheduled check can distinguish "drift was
// found" from "the command failed".
type ExitCoder interface {
	ExitCode() int
}

// silentError has already reported itself to the operator, so Execute
// sets the exit status without printing an "Error:" line on top of a
// report that already said the same thing.
type silentError interface {
	Silent() bool
}

// ExitCode returns the process exit status for an error.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}

	var coder ExitCoder
	if errors.As(err, &coder) {
		return coder.ExitCode()
	}
	if errors.Is(err, context.Canceled) {
		return ExitCodeInterrupted
	}
	return 1
}

// Execute runs the root command. Called from main.go.
func Execute(ctx context.Context) error {
	err := rootCmd.ExecuteContext(ctx)
	if err == nil {
		return nil
	}

	// The interrupt handler already said what happened, and a
	// "context canceled" line on top of it reads like a defect rather
	// than the operator's own Ctrl-C.
	if errors.Is(err, context.Canceled) {
		return err
	}

	var quiet silentError
	if !errors.As(err, &quiet) || !quiet.Silent() {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
	return err
}
