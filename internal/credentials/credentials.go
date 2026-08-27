// Package credentials stores and retrieves POS platform credentials
// for a Mise workspace.
//
// Credentials live in .mise/credentials — a JSON file with 0600
// permissions that is gitignored by default and must never be
// committed. mise.yaml records *how* to authenticate (the method);
// this file holds the actual secret.
//
// Think of mise.yaml as the note on the fridge saying "the spare key
// is under the mat" and .mise/credentials as the key itself.
package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// Dir is the workspace directory holding state and credentials.
	Dir = ".mise"

	// FileName is the credentials file name within Dir.
	FileName = "credentials"

	// MethodOAuth2 authenticates via the platform's OAuth2 flow.
	MethodOAuth2 = "oauth2"

	// MethodAccessToken authenticates with a personal access token
	// pasted in by the operator.
	MethodAccessToken = "access_token"

	// refreshWindow is how long before expiry a token is considered
	// stale enough to refresh proactively.
	refreshWindow = 7 * 24 * time.Hour
)

// Credentials holds the authentication material for one provider in
// one workspace.
type Credentials struct {
	Version     int    `json:"version"`
	Provider    string `json:"provider"`    // "square", "toast", ...
	Method      string `json:"method"`      // MethodOAuth2 or MethodAccessToken
	Environment string `json:"environment"` // "production" or "sandbox"

	AccessToken  string     `json:"access_token"`
	RefreshToken string     `json:"refresh_token,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	ObtainedAt   time.Time  `json:"obtained_at"`

	// MerchantID is the account the token belongs to. Square returns
	// this from the OAuth token exchange.
	MerchantID string `json:"merchant_id,omitempty"`

	// ClientID and ClientSecret are the OAuth application credentials.
	// They are stored so that Mise can refresh an expiring token
	// without re-prompting. Only set for MethodOAuth2.
	ClientID     string `json:"client_id,omitempty"`
	ClientSecret string `json:"client_secret,omitempty"`
}

// Path returns the credentials file path for a workspace.
func Path(workDir string) string {
	return filepath.Join(workDir, Dir, FileName)
}

// Exists reports whether a credentials file is present.
func Exists(workDir string) bool {
	_, err := os.Stat(Path(workDir))
	return err == nil
}

// Save writes credentials to .mise/credentials with 0600 permissions,
// creating the .mise/ directory (0700) if needed.
func (c *Credentials) Save(workDir string) error {
	if err := c.Validate(); err != nil {
		return err
	}

	dir := filepath.Join(workDir, Dir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s directory: %w", Dir, err)
	}

	if c.Version == 0 {
		c.Version = 1
	}
	if c.ObtainedAt.IsZero() {
		c.ObtainedAt = time.Now().UTC()
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("cannot serialize credentials: %w", err)
	}
	data = append(data, '\n')

	path := Path(workDir)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("cannot write credentials file: %w", err)
	}

	// WriteFile only applies the mode when creating the file. Re-apply
	// it so an existing, more permissive file gets locked down too.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("cannot set permissions on credentials file: %w", err)
	}

	return nil
}

// Load reads credentials from .mise/credentials.
func Load(workDir string) (*Credentials, error) {
	path := Path(workDir)

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, fmt.Errorf("no credentials found at %s — run 'mise init' first", path)
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read credentials file: %w", err)
	}

	var c Credentials
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("cannot parse credentials file %s: %w", path, err)
	}

	return &c, nil
}

// Resolve returns the credentials Mise should use for a provider.
//
// An access token in the environment wins over the stored file — this
// is how CI pipelines authenticate without running an interactive
// `mise init`. Mise checks MISE_<PROVIDER>_ACCESS_TOKEN first, then
// <PROVIDER>_ACCESS_TOKEN (e.g. SQUARE_ACCESS_TOKEN).
func Resolve(workDir, providerName, environment string) (*Credentials, error) {
	if token, _ := FromEnv(providerName); token != "" {
		return &Credentials{
			Version:     1,
			Provider:    providerName,
			Method:      MethodAccessToken,
			Environment: environment,
			AccessToken: token,
			ObtainedAt:  time.Now().UTC(),
		}, nil
	}
	return Load(workDir)
}

// FromEnv looks up an access token for a provider in the environment
// and reports which variable supplied it.
func FromEnv(providerName string) (token, envVar string) {
	upper := strings.ToUpper(providerName)
	for _, name := range []string{
		"MISE_" + upper + "_ACCESS_TOKEN",
		upper + "_ACCESS_TOKEN",
	} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, name
		}
	}
	return "", ""
}

// Validate checks that the credentials are internally consistent
// before they are written to disk or used to talk to an API.
func (c *Credentials) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("credentials are missing a provider name")
	}
	switch c.Method {
	case MethodOAuth2, MethodAccessToken:
	case "":
		return fmt.Errorf("credentials are missing an auth method")
	default:
		return fmt.Errorf("unknown auth method %q — use %q or %q", c.Method, MethodOAuth2, MethodAccessToken)
	}
	if c.AccessToken == "" {
		return fmt.Errorf("credentials are missing an access token")
	}
	return nil
}

// Expired reports whether the access token's expiry has passed.
// Tokens with no expiry (personal access tokens) never expire.
func (c *Credentials) Expired() bool {
	return c.ExpiresAt != nil && !time.Now().Before(*c.ExpiresAt)
}

// NeedsRefresh reports whether the token is expired or close enough
// to expiry that Mise should refresh it before a long operation.
// Only OAuth2 credentials can be refreshed.
func (c *Credentials) NeedsRefresh() bool {
	if c.Method != MethodOAuth2 || c.RefreshToken == "" || c.ExpiresAt == nil {
		return false
	}
	return time.Now().Add(refreshWindow).After(*c.ExpiresAt)
}
