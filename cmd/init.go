package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/providers/square" // imported for its init(), which registers the adapter
)

// Environment names Mise accepts.
const (
	envProduction = "production"
	envSandbox    = "sandbox"
)

// Environment variables init reads so that CI pipelines and scripted
// setups never have to answer a prompt.
const (
	envVarAppID     = "SQUARE_APPLICATION_ID"
	envVarAppSecret = "SQUARE_APPLICATION_SECRET"
)

// initOptions holds everything `mise init` needs. Every field can be
// supplied by a flag or an environment variable; anything still empty
// is prompted for interactively.
type initOptions struct {
	platform     string
	environment  string
	authMethod   string
	accessToken  string
	clientID     string
	clientSecret string
	callbackPort int
	noBrowser    bool
	force        bool
}

var initOpts initOptions

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize a new Mise workspace",
	Long: `Creates a new Mise workspace in the current directory.

Prompts for platform selection and authentication, then generates
a mise.yaml configuration file and .mise/ directory for state
and credentials.

Authentication happens one of two ways:

  oauth2         Opens a browser, you approve Mise on Square's consent
                 screen, and Square redirects back to a local listener.
                 Best for multi-location accounts and shared setups.

  access_token   You paste a personal access token from the Square
                 Developer Dashboard. Best for a single operator or a
                 sandbox account.

Every value can also be supplied non-interactively:

  mise init --platform square --environment sandbox \
            --auth-method access_token --token "$SQUARE_ACCESS_TOKEN"`,
	RunE: runInit,
}

func init() {
	initCmd.Flags().StringVar(&initOpts.platform, "platform", "", "POS platform (square)")
	initCmd.Flags().StringVar(&initOpts.environment, "environment", "", "API environment: production or sandbox")
	initCmd.Flags().StringVar(&initOpts.authMethod, "auth-method", "", "authentication method: oauth2 or access_token")
	initCmd.Flags().StringVar(&initOpts.accessToken, "token", "", "access token (access_token method; prefer SQUARE_ACCESS_TOKEN)")
	initCmd.Flags().StringVar(&initOpts.clientID, "client-id", "", "OAuth2 application ID (or set "+envVarAppID+")")
	initCmd.Flags().StringVar(&initOpts.clientSecret, "client-secret", "", "OAuth2 application secret (or set "+envVarAppSecret+")")
	initCmd.Flags().IntVar(&initOpts.callbackPort, "callback-port", square.DefaultCallbackPort, "local port for the OAuth2 redirect")
	initCmd.Flags().BoolVar(&initOpts.noBrowser, "no-browser", false, "print the authorization URL instead of opening a browser")
	initCmd.Flags().BoolVar(&initOpts.force, "force", false, "overwrite an existing mise.yaml and credentials")

	rootCmd.AddCommand(initCmd)
}

func runInit(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	out := cmd.OutOrStdout()
	configPath := configFile
	workDir := filepath.Dir(configPath)
	if workDir == "" {
		workDir = "."
	}

	prompt := newPrompter(out)
	opts := initOpts

	if err := checkExistingWorkspace(out, prompt, configPath, workDir, opts.force); err != nil {
		return err
	}

	if err := resolveInitOptions(&opts, prompt); err != nil {
		return err
	}

	// Authenticate first. Nothing is written to disk until Mise has a
	// token that Square accepts — a failed init leaves no half-built
	// workspace behind.
	creds, err := authenticate(ctx, out, prompt, opts)
	if err != nil {
		return err
	}

	locations, err := validateCredentials(ctx, out, opts, creds)
	if err != nil {
		return err
	}

	if err := writeWorkspace(out, configPath, workDir, opts, creds, locations); err != nil {
		return err
	}

	printNextSteps(out, opts, locations)
	return nil
}

// checkExistingWorkspace stops init from clobbering a configured
// workspace unless the operator asked for it.
func checkExistingWorkspace(out io.Writer, prompt *prompter, configPath, workDir string, force bool) error {
	configExists := fileExists(configPath)
	credsExist := credentials.Exists(workDir)

	if !configExists && !credsExist {
		return nil
	}

	if force {
		return nil
	}

	var found []string
	if configExists {
		found = append(found, configPath)
	}
	if credsExist {
		found = append(found, credentials.Path(workDir))
	}

	fmt.Fprintf(out, "This directory already has a Mise workspace (%s).\n", strings.Join(found, ", "))

	overwrite, err := prompt.confirm("Reinitialize and overwrite it?", false)
	if errors.Is(err, errNotInteractive) {
		return fmt.Errorf("workspace already initialized (%s) — pass --force to overwrite", strings.Join(found, ", "))
	}
	if err != nil {
		return err
	}
	if !overwrite {
		return fmt.Errorf("init cancelled — existing workspace left unchanged")
	}
	return nil
}

// resolveInitOptions fills in any option not supplied by a flag,
// falling back to the environment and then to an interactive prompt.
func resolveInitOptions(opts *initOptions, prompt *prompter) error {
	platforms := provider.AvailableProviders()
	if len(platforms) == 0 {
		return fmt.Errorf("no POS adapters are registered in this build")
	}

	if opts.platform == "" {
		if len(platforms) == 1 {
			opts.platform = platforms[0]
		} else {
			choice, err := prompt.choose("Which POS platform?", platforms, platforms[0])
			if err != nil {
				return err
			}
			opts.platform = choice
		}
	}
	if _, err := provider.Get(opts.platform); err != nil {
		return err
	}

	if opts.environment == "" {
		choice, err := prompt.choose("Which environment?", []string{envSandbox, envProduction}, envSandbox)
		if err != nil {
			return err
		}
		opts.environment = choice
	}
	if opts.environment != envSandbox && opts.environment != envProduction {
		return fmt.Errorf("unknown environment %q — use %q or %q", opts.environment, envProduction, envSandbox)
	}

	// An access token already in the environment is an unambiguous
	// signal about which method the operator wants.
	if opts.authMethod == "" && opts.accessToken == "" {
		if token, envVar := credentials.FromEnv(opts.platform); token != "" {
			opts.authMethod = credentials.MethodAccessToken
			opts.accessToken = token
			fmt.Fprintf(prompt.out, "Using the access token from %s.\n", envVar)
		}
	}
	if opts.authMethod == "" && opts.accessToken != "" {
		opts.authMethod = credentials.MethodAccessToken
	}
	if opts.authMethod == "" {
		choice, err := prompt.choose(
			"How should Mise authenticate?",
			[]string{credentials.MethodOAuth2, credentials.MethodAccessToken},
			credentials.MethodOAuth2,
		)
		if err != nil {
			return err
		}
		opts.authMethod = choice
	}
	switch opts.authMethod {
	case credentials.MethodOAuth2:
		// Silently ignoring a token the operator explicitly passed is
		// worse than making them pick one method.
		if opts.accessToken != "" {
			return fmt.Errorf("--token is only used with --auth-method %s", credentials.MethodAccessToken)
		}
	case credentials.MethodAccessToken:
		if opts.clientID != "" || opts.clientSecret != "" {
			return fmt.Errorf("--client-id and --client-secret are only used with --auth-method %s",
				credentials.MethodOAuth2)
		}
	default:
		return fmt.Errorf("unknown auth method %q — use %q or %q",
			opts.authMethod, credentials.MethodOAuth2, credentials.MethodAccessToken)
	}

	if opts.callbackPort == 0 {
		opts.callbackPort = square.DefaultCallbackPort
	}

	return nil
}

// authenticate obtains credentials by the chosen method.
func authenticate(ctx context.Context, out io.Writer, prompt *prompter, opts initOptions) (*credentials.Credentials, error) {
	switch opts.authMethod {
	case credentials.MethodAccessToken:
		return authenticateWithToken(prompt, opts)
	case credentials.MethodOAuth2:
		return authenticateWithOAuth2(ctx, out, prompt, opts)
	default:
		return nil, fmt.Errorf("unknown auth method %q", opts.authMethod)
	}
}

// authenticateWithToken builds credentials from a personal access
// token supplied by flag, environment, or a masked prompt.
func authenticateWithToken(prompt *prompter, opts initOptions) (*credentials.Credentials, error) {
	token := strings.TrimSpace(opts.accessToken)

	if token == "" {
		if envToken, envVar := credentials.FromEnv(opts.platform); envToken != "" {
			fmt.Fprintf(prompt.out, "Using the access token from %s.\n", envVar)
			token = envToken
		}
	}

	if token == "" {
		fmt.Fprintf(prompt.out, "\nFind your access token in the Square Developer Dashboard under\n"+
			"your application → Credentials → %s Access Token.\n", strings.Title(opts.environment)) //nolint:staticcheck // ASCII environment names only
		answer, err := prompt.askSecret("Square access token")
		if err != nil {
			if errors.Is(err, errNotInteractive) {
				return nil, fmt.Errorf("no access token supplied — pass --token or set SQUARE_ACCESS_TOKEN")
			}
			return nil, err
		}
		token = answer
	}

	if token == "" {
		return nil, fmt.Errorf("no access token supplied")
	}

	return &credentials.Credentials{
		Version:     1,
		Provider:    opts.platform,
		Method:      credentials.MethodAccessToken,
		Environment: opts.environment,
		AccessToken: token,
	}, nil
}

// authenticateWithOAuth2 runs Square's authorization-code flow.
func authenticateWithOAuth2(ctx context.Context, out io.Writer, prompt *prompter, opts initOptions) (*credentials.Credentials, error) {
	clientID := firstNonEmpty(opts.clientID, os.Getenv(envVarAppID))
	clientSecret := firstNonEmpty(opts.clientSecret, os.Getenv(envVarAppSecret))

	redirectURL := square.RedirectURL(opts.callbackPort)

	if clientID == "" || clientSecret == "" {
		fmt.Fprintf(out, "\nOAuth2 needs your Square application's credentials.\n"+
			"Create an application at https://developer.squareup.com/apps, then add\n\n  %s\n\n"+
			"as a Redirect URL under OAuth in that application's settings.\n\n", redirectURL)
	}

	if clientID == "" {
		answer, err := prompt.ask("Square application ID", "")
		if err != nil {
			if errors.Is(err, errNotInteractive) {
				return nil, fmt.Errorf("no application ID supplied — pass --client-id or set %s", envVarAppID)
			}
			return nil, err
		}
		clientID = answer
	}
	if clientSecret == "" {
		answer, err := prompt.askSecret("Square application secret")
		if err != nil {
			if errors.Is(err, errNotInteractive) {
				return nil, fmt.Errorf("no application secret supplied — pass --client-secret or set %s", envVarAppSecret)
			}
			return nil, err
		}
		clientSecret = answer
	}

	if opts.environment == envProduction {
		fmt.Fprintf(out, "\nNote: Square requires HTTPS redirect URLs for production applications.\n"+
			"If %s is rejected, use --auth-method access_token instead.\n", redirectURL)
	}

	oauthCfg := square.OAuthConfig{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		Environment:  opts.environment,
		CallbackPort: opts.callbackPort,
		OpenBrowser:  !opts.noBrowser,
		Out:          out,
	}

	fmt.Fprintln(out)
	creds, err := oauthCfg.Authorize(ctx)
	if err != nil {
		return nil, fmt.Errorf("OAuth2 authorization failed: %w", err)
	}

	fmt.Fprintln(out, "Authorization granted.")
	return creds, nil
}

// validateCredentials proves the token works before Mise writes
// anything, by listing the account's locations. This doubles as the
// operator's first confirmation that they connected the right account.
func validateCredentials(ctx context.Context, out io.Writer, opts initOptions, creds *credentials.Credentials) ([]provider.Location, error) {
	p, err := provider.Get(opts.platform)
	if err != nil {
		return nil, err
	}

	cfg := provider.ProviderConfig{
		Platform:    opts.platform,
		Environment: opts.environment,
		Credentials: provider.CredentialsConfig{
			Method:      creds.Method,
			AccessToken: creds.AccessToken,
		},
	}
	if err := p.Configure(cfg); err != nil {
		return nil, err
	}

	fmt.Fprintf(out, "\nVerifying credentials against the %s %s API...\n", opts.platform, opts.environment)

	locations, err := p.ListLocations(ctx)
	if err != nil {
		// The provider's own error carries the advice for the status it
		// got back; naming the environment here is what that advice
		// cannot know, and it is the most common thing to have wrong.
		return nil, fmt.Errorf("could not verify credentials against the %s %s API: %w",
			opts.platform, opts.environment, err)
	}

	if len(locations) == 0 {
		fmt.Fprintf(out, "Credentials are valid, but this account has no locations yet.\n")
		return locations, nil
	}

	fmt.Fprintf(out, "Connected. Found %s:\n", pluralize(len(locations), "location", "locations"))
	for _, l := range locations {
		if l.Address != "" {
			fmt.Fprintf(out, "  - %s (%s) — %s\n", l.Name, l.ID, l.Address)
		} else {
			fmt.Fprintf(out, "  - %s (%s)\n", l.Name, l.ID)
		}
	}

	return locations, nil
}

// writeWorkspace lays down .mise/, the credentials file, mise.yaml,
// and the .gitignore entries that keep secrets out of git.
func writeWorkspace(out io.Writer, configPath, workDir string, opts initOptions, creds *credentials.Credentials, locations []provider.Location) error {
	if err := ensureStateDir(workDir); err != nil {
		return err
	}

	if err := creds.Save(workDir); err != nil {
		return err
	}

	// Add the gitignore entries before mentioning the credentials file,
	// so a workspace is never momentarily committable.
	added, err := ensureGitignore(workDir)
	if err != nil {
		return err
	}

	exampleLocationID := ""
	if len(locations) > 0 {
		exampleLocationID = locations[0].ID
	}
	if err := writeRootConfig(configPath, opts.platform, opts.environment, opts.authMethod, exampleLocationID); err != nil {
		return err
	}

	fmt.Fprintf(out, "\nCreated:\n")
	fmt.Fprintf(out, "  %s\n", configPath)
	fmt.Fprintf(out, "  %s (permissions 0600, never commit this)\n", credentials.Path(workDir))
	if len(added) > 0 {
		fmt.Fprintf(out, "  %s (added %s)\n", filepath.Join(workDir, ".gitignore"), strings.Join(added, ", "))
	}

	return nil
}

// printNextSteps closes out a successful init.
func printNextSteps(out io.Writer, opts initOptions, locations []provider.Location) {
	fmt.Fprintf(out, "\nWorkspace initialized for %s (%s).\n", opts.platform, opts.environment)
	if len(locations) > 0 {
		fmt.Fprintf(out, "Run 'mise fetch' to import the configuration for %s.\n",
			pluralize(len(locations), "location", "locations"))
	} else {
		fmt.Fprintln(out, "Run 'mise fetch' to import your current POS configuration.")
	}
}

// fileExists reports whether a path exists and is not a directory.
func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// firstNonEmpty returns the first non-empty string, trimmed.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// capitalize upper-cases the first letter of an ASCII word.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// pluralize renders "1 location" / "3 locations".
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}
