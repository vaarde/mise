package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/providers/square"
)

// workspace is a loaded Mise workspace: the root config, the credentials
// it points at, and a provider configured from both. Every command after
// init starts here.
type workspace struct {
	Dir      string
	Config   *config.RootConfig
	Creds    *credentials.Credentials
	Provider provider.Provider
}

// loadWorkspace reads mise.yaml, resolves credentials, and returns a
// configured provider ready to talk to the POS.
func loadWorkspace(ctx context.Context, out io.Writer, configPath string) (*workspace, error) {
	dir := filepath.Dir(configPath)
	if dir == "" {
		dir = "."
	}

	cfg, err := config.LoadRoot(configPath)
	if err != nil {
		return nil, fmt.Errorf("%w\nRun 'mise init' to create a workspace", err)
	}
	if cfg.Provider.Platform == "" {
		return nil, fmt.Errorf("%s does not name a provider platform", configPath)
	}

	creds, err := credentials.Resolve(dir, cfg.Provider.Platform, cfg.Provider.Environment)
	if err != nil {
		return nil, err
	}

	creds, err = refreshIfNeeded(ctx, out, dir, creds)
	if err != nil {
		return nil, err
	}

	p, err := provider.Get(cfg.Provider.Platform)
	if err != nil {
		return nil, err
	}

	if err := p.Configure(provider.ProviderConfig{
		Platform:    cfg.Provider.Platform,
		Environment: cfg.Provider.Environment,
		Credentials: provider.CredentialsConfig{
			Method:      creds.Method,
			AccessToken: creds.AccessToken,
		},
	}); err != nil {
		return nil, err
	}

	return &workspace{Dir: dir, Config: cfg, Creds: creds, Provider: p}, nil
}

// refreshIfNeeded renews an OAuth token that is expired or close to it,
// and persists the new one. Square's production tokens last 30 days, so
// a workspace left alone for a month would otherwise start failing with
// a 401 that looks like a configuration problem.
func refreshIfNeeded(ctx context.Context, out io.Writer, dir string, creds *credentials.Credentials) (*credentials.Credentials, error) {
	if creds.Provider != square.ProviderName || !creds.NeedsRefresh() {
		return creds, nil
	}

	fmt.Fprintln(out, "Access token is expiring — refreshing it with Square...")

	refreshed, err := square.Refresh(ctx, creds)
	if err != nil {
		if creds.Expired() {
			return nil, fmt.Errorf("access token has expired and could not be refreshed: %w\n"+
				"Run 'mise init --force' to re-authorize", err)
		}
		// Not expired yet, so a failed refresh is not fatal.
		fmt.Fprintf(out, "Could not refresh the token (%v) — continuing with the current one.\n", err)
		return creds, nil
	}

	if err := refreshed.Save(dir); err != nil {
		return nil, err
	}
	return refreshed, nil
}
