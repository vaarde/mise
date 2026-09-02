package cmd

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/credentials"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/providers/square"
	"github.com/vaarde/mise/internal/state"
)

// workspace is a loaded Mise workspace: the root config, the credentials
// it points at, and a provider configured from both. Every command after
// init starts here.
type workspace struct {
	Dir      string
	Config   *config.RootConfig
	Creds    *credentials.Credentials
	Provider provider.Provider

	// identity caches the account the credentials reach. Resolving it
	// costs an API call, and plan, apply and drift each want it.
	identity     *state.Identity
	identityOnce sync.Once
	identityErr  error
}

// Identity reports which POS account this workspace's credentials
// actually reach.
//
// The platform and environment come from mise.yaml. The account itself
// comes from the adapter, when it can name one — an adapter that cannot
// leaves it empty rather than guessing, and the check degrades to
// platform and environment.
func (ws *workspace) Identity(ctx context.Context) (state.Identity, error) {
	ws.identityOnce.Do(func() {
		id := state.Identity{
			Provider:    ws.Config.Provider.Platform,
			Environment: ws.Config.Provider.Environment,
		}

		if namer, ok := ws.Provider.(provider.AccountIdentifier); ok {
			accountID, err := namer.AccountID(ctx)
			if err != nil {
				ws.identityErr = fmt.Errorf("cannot confirm which %s account these credentials reach: %w",
					ws.Config.Provider.Platform, err)
				return
			}
			id.AccountID = accountID
		}

		ws.identity = &id
	})

	if ws.identityErr != nil {
		return state.Identity{}, ws.identityErr
	}
	return *ws.identity, nil
}

// CheckStateIdentity refuses to operate on state that belongs to a
// different POS account.
//
// Provider IDs are only meaningful within the account that issued them.
// Without this check, pointing a workspace at a second merchant makes
// every recorded ID miss, so plan reads each resource as deleted and
// proposes to create it — duplicating the whole configuration into the
// wrong account, quietly and successfully.
//
// State written before Mise recorded an identity has nothing to compare,
// and is allowed through; the next fetch or apply stamps it.
func (ws *workspace) CheckStateIdentity(ctx context.Context, st *state.State) error {
	recorded := st.Identity()
	if recorded.IsZero() {
		return nil
	}

	current, err := ws.Identity(ctx)
	if err != nil {
		return err
	}
	return recorded.Check(current)
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
