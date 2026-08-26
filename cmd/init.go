package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Mise workspace",
	Long: `Creates a new Mise workspace in the current directory.

Prompts for platform selection and authentication, then generates
a mise.yaml configuration file and .mise/ directory for state
and credentials.`,
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	fmt.Println("Initializing Mise workspace...")

	// TODO: Milestone 1 implementation
	// 1. Prompt for platform selection (square)
	// 2. Prompt for auth method (oauth2 or access_token)
	// 3. Run OAuth2 flow or validate access token
	// 4. Create mise.yaml with provider config
	// 5. Create .mise/ directory
	// 6. Create .gitignore entries

	fmt.Println("Workspace initialized. Run 'mise fetch' to import your current POS configuration.")
	return nil
}
