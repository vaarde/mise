package square

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/vaarde/mise/internal/credentials"
)

// OAuth endpoints. Square uses a separate host for sandbox, and the
// OAuth endpoints live at the host root rather than under /v2.
const (
	ProductionHost = "https://connect.squareup.com"
	SandboxHost    = "https://connect.squareupsandbox.com"

	// DefaultCallbackPort is where Mise listens for Square's OAuth
	// redirect. It must match the redirect URL registered on the
	// application in the Square Developer Dashboard.
	DefaultCallbackPort = 8666

	// CallbackPath is the path Square redirects back to.
	CallbackPath = "/mise/callback"

	// oauthTimeout is how long Mise waits for the operator to finish
	// authorizing in the browser before giving up.
	oauthTimeout = 5 * time.Minute
)

// RequiredScopes are the Square permissions Mise needs for Phase 0:
// read and write the catalog (items, categories, taxes, discounts,
// modifiers) and read/update location settings.
var RequiredScopes = []string{
	"MERCHANT_PROFILE_READ",
	"MERCHANT_PROFILE_WRITE",
	"ITEMS_READ",
	"ITEMS_WRITE",
}

// OAuthConfig describes an OAuth2 authorization attempt.
type OAuthConfig struct {
	ClientID     string // Square application ID
	ClientSecret string // Square application secret
	Environment  string // "production" or "sandbox"
	CallbackPort int    // local port for the redirect listener
	Scopes       []string
	OpenBrowser  bool      // false prints the URL instead of launching a browser
	Out          io.Writer // where progress messages go

	// host overrides the environment-derived API host. Tests set it to
	// point the OAuth endpoints at a local server.
	host string
}

// apiHost returns the Square host this config talks to.
func (c OAuthConfig) apiHost() (string, error) {
	if c.host != "" {
		return c.host, nil
	}
	return hostFor(c.Environment)
}

// hostFor returns the Square API host for an environment.
func hostFor(environment string) (string, error) {
	switch environment {
	case "sandbox":
		return SandboxHost, nil
	case "production", "":
		return ProductionHost, nil
	default:
		return "", fmt.Errorf("unknown environment %q — use 'production' or 'sandbox'", environment)
	}
}

// BaseURLFor returns the Square API base URL (including /v2) for an
// environment.
func BaseURLFor(environment string) (string, error) {
	host, err := hostFor(environment)
	if err != nil {
		return "", err
	}
	return host + "/v2", nil
}

// RedirectURL returns the local callback URL Mise listens on. This
// exact value must be registered as the application's redirect URL
// in the Square Developer Dashboard.
func RedirectURL(port int) string {
	if port == 0 {
		port = DefaultCallbackPort
	}
	return fmt.Sprintf("http://localhost:%d%s", port, CallbackPath)
}

// oauth2Config builds the x/oauth2 config used to construct the
// authorization URL.
//
// Mise uses x/oauth2 for the authorization URL and endpoint types, but
// performs the token exchange itself: Square's /oauth2/token endpoint
// takes a JSON body and returns an RFC3339 "expires_at" timestamp
// rather than the form-encoded request and "expires_in" seconds that
// x/oauth2's Exchange assumes.
func (c OAuthConfig) oauth2Config() (*oauth2.Config, error) {
	host, err := c.apiHost()
	if err != nil {
		return nil, err
	}
	scopes := c.Scopes
	if len(scopes) == 0 {
		scopes = RequiredScopes
	}
	return &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  host + "/oauth2/authorize",
			TokenURL: host + "/oauth2/token",
		},
		RedirectURL: RedirectURL(c.CallbackPort),
		Scopes:      scopes,
	}, nil
}

// AuthorizeURL returns the URL the operator must visit to grant Mise
// access, carrying the given anti-CSRF state value.
func (c OAuthConfig) AuthorizeURL(state string) (string, error) {
	cfg, err := c.oauth2Config()
	if err != nil {
		return "", err
	}
	// session=false forces Square to show the login screen rather than
	// silently reusing an existing dashboard session, so the operator
	// can pick the right merchant account.
	return cfg.AuthCodeURL(state, oauth2.SetAuthURLParam("session", "false")), nil
}

// Authorize runs the full OAuth2 authorization-code flow: it starts a
// local callback listener, sends the operator to Square's consent
// screen, waits for the redirect, and exchanges the code for a token.
func (c OAuthConfig) Authorize(ctx context.Context) (*credentials.Credentials, error) {
	if c.ClientID == "" {
		return nil, fmt.Errorf("missing Square application ID")
	}
	if c.ClientSecret == "" {
		return nil, fmt.Errorf("missing Square application secret")
	}
	if _, err := hostFor(c.Environment); err != nil {
		return nil, err
	}

	out := c.Out
	if out == nil {
		out = io.Discard
	}
	port := c.CallbackPort
	if port == 0 {
		port = DefaultCallbackPort
	}

	state, err := randomState()
	if err != nil {
		return nil, err
	}

	// Bind the listener before opening the browser so a port conflict
	// surfaces immediately instead of as a dead redirect.
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("cannot listen on port %d for the OAuth callback: %w\n"+
			"Another process may be using it — retry with --callback-port <port> "+
			"(and register the matching redirect URL in your Square application)", port, err)
	}

	results := make(chan callbackResult, 1)
	server := &http.Server{
		Handler:           callbackHandler(state, results),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	authURL, err := c.AuthorizeURL(state)
	if err != nil {
		return nil, err
	}

	if c.OpenBrowser {
		fmt.Fprintf(out, "Opening your browser to authorize Mise with Square...\n")
		if err := openBrowser(authURL); err != nil {
			fmt.Fprintf(out, "Could not open a browser automatically (%v).\n", err)
			fmt.Fprintf(out, "Open this URL manually:\n\n  %s\n\n", authURL)
		}
	} else {
		fmt.Fprintf(out, "Open this URL to authorize Mise with Square:\n\n  %s\n\n", authURL)
	}
	fmt.Fprintf(out, "Waiting for authorization (listening on %s)...\n", RedirectURL(port))

	waitCtx, cancel := context.WithTimeout(ctx, oauthTimeout)
	defer cancel()

	select {
	case <-waitCtx.Done():
		if ctx.Err() != nil {
			return nil, fmt.Errorf("authorization cancelled: %w", ctx.Err())
		}
		return nil, fmt.Errorf("timed out after %s waiting for Square authorization", oauthTimeout)
	case res := <-results:
		if res.err != nil {
			return nil, res.err
		}
		return c.Exchange(ctx, res.code)
	}
}

// callbackResult carries the outcome of the OAuth redirect from the
// callback HTTP handler back to Authorize.
type callbackResult struct {
	code string
	err  error
}

// callbackHandler serves the single redirect request Square makes back
// to the local listener.
func callbackHandler(wantState string, results chan<- callbackResult) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(CallbackPath, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()

		send := func(res callbackResult) {
			select {
			case results <- res:
			default: // a result was already delivered; ignore duplicates
			}
		}

		if errCode := q.Get("error"); errCode != "" {
			detail := q.Get("error_description")
			if detail == "" {
				detail = errCode
			}
			writeCallbackPage(w, http.StatusBadRequest, "Authorization failed", detail)
			send(callbackResult{err: fmt.Errorf("Square authorization failed (%s): %s", errCode, detail)})
			return
		}

		// The state parameter is what stops another site from feeding
		// Mise an authorization code that isn't ours.
		if got := q.Get("state"); got != wantState {
			writeCallbackPage(w, http.StatusBadRequest, "Authorization failed",
				"The authorization response did not match this request.")
			send(callbackResult{err: fmt.Errorf("OAuth state mismatch — the authorization response did not match this request")})
			return
		}

		code := q.Get("code")
		if code == "" {
			writeCallbackPage(w, http.StatusBadRequest, "Authorization failed",
				"Square did not return an authorization code.")
			send(callbackResult{err: fmt.Errorf("Square did not return an authorization code")})
			return
		}

		writeCallbackPage(w, http.StatusOK, "Authorization received",
			"Mise is finishing the connection. You can close this tab and return to your terminal.")
		send(callbackResult{code: code})
	})
	return mux
}

// callbackPageTemplate is the minimal page the operator sees in their
// browser after Square redirects back.
const callbackPageTemplate = `<!doctype html>
<html><head><meta charset="utf-8"><title>%s</title></head>
<body style="font-family:system-ui,sans-serif;max-width:32rem;margin:6rem auto;padding:0 1rem">
<h1 style="font-size:1.25rem">%s</h1>
<p style="color:#555">%s</p>
</body></html>
`

func writeCallbackPage(w http.ResponseWriter, status int, title, message string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	safeTitle := html.EscapeString(title)
	fmt.Fprintf(w, callbackPageTemplate, safeTitle, safeTitle, html.EscapeString(message))
}

// tokenResponse is Square's /oauth2/token response body.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at"` // RFC3339, not "expires_in" seconds
	MerchantID   string `json:"merchant_id"`
	RefreshToken string `json:"refresh_token"`
	ShortLived   bool   `json:"short_lived"`

	// OAuth-level error fields. The /oauth2 endpoints do not use the
	// same "errors" array shape as the rest of the Square API, and they
	// use two shapes of their own: {"error","error_description"} for
	// grant failures and {"type","message"} for rejected applications.
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
	Type             string `json:"type"`
	Message          string `json:"message"`
}

// Exchange trades an authorization code for an access token.
func (c OAuthConfig) Exchange(ctx context.Context, code string) (*credentials.Credentials, error) {
	return c.postToken(ctx, map[string]string{
		"client_id":     c.ClientID,
		"client_secret": c.ClientSecret,
		"code":          code,
		"grant_type":    "authorization_code",
		"redirect_uri":  RedirectURL(c.CallbackPort),
	})
}

// Refresh exchanges a refresh token for a fresh access token. Square
// access tokens expire (30 days in production), so long-lived
// workspaces re-authenticate through this path rather than sending the
// operator back to the browser.
func Refresh(ctx context.Context, creds *credentials.Credentials) (*credentials.Credentials, error) {
	return OAuthConfig{
		ClientID:     creds.ClientID,
		ClientSecret: creds.ClientSecret,
		Environment:  creds.Environment,
	}.refresh(ctx, creds)
}

// refresh is Refresh's implementation, on OAuthConfig so that tests
// can redirect the token endpoint to a local server.
func (cfg OAuthConfig) refresh(ctx context.Context, creds *credentials.Credentials) (*credentials.Credentials, error) {
	if creds.Method != credentials.MethodOAuth2 {
		return nil, fmt.Errorf("only OAuth2 credentials can be refreshed")
	}
	if creds.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token stored — run 'mise init' to re-authorize")
	}
	if creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, fmt.Errorf("no Square application credentials stored — run 'mise init' to re-authorize")
	}

	refreshed, err := cfg.postToken(ctx, map[string]string{
		"client_id":     creds.ClientID,
		"client_secret": creds.ClientSecret,
		"refresh_token": creds.RefreshToken,
		"grant_type":    "refresh_token",
	})
	if err != nil {
		return nil, err
	}

	// Square may omit the refresh token on a refresh response — keep
	// the existing one when it does.
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = creds.RefreshToken
	}
	return refreshed, nil
}

// postToken performs a token request against Square's /oauth2/token
// endpoint and converts the response into stored credentials.
func (c OAuthConfig) postToken(ctx context.Context, body map[string]string) (*credentials.Credentials, error) {
	host, err := c.apiHost()
	if err != nil {
		return nil, err
	}

	payload, marshalErr := json.Marshal(body)
	if marshalErr != nil {
		return nil, fmt.Errorf("cannot encode token request: %w", marshalErr)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, host+"/oauth2/token", strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("cannot create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Square-Version", APIVersion)
	req.Header.Set("User-Agent", UserAgent)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("cannot read token response: %w", err)
	}

	var tr tokenResponse
	if err := json.Unmarshal(raw, &tr); err != nil {
		return nil, fmt.Errorf("cannot parse token response (HTTP %d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode >= 400 || tr.Error != "" {
		detail := firstNonEmpty(tr.ErrorDescription, tr.Message, strings.TrimSpace(string(raw)))
		code := firstNonEmpty(tr.Error, tr.Type, http.StatusText(resp.StatusCode))
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Code:       code,
			Message:    detail,
			Retryable:  resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500,
		}
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("Square returned an empty access token")
	}

	creds := &credentials.Credentials{
		Version:      1,
		Provider:     ProviderName,
		Method:       credentials.MethodOAuth2,
		Environment:  NormalizeEnvironment(c.Environment),
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		MerchantID:   tr.MerchantID,
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		ObtainedAt:   time.Now().UTC(),
	}

	if tr.ExpiresAt != "" {
		expiry, err := time.Parse(time.RFC3339, tr.ExpiresAt)
		if err != nil {
			return nil, fmt.Errorf("cannot parse token expiry %q: %w", tr.ExpiresAt, err)
		}
		expiry = expiry.UTC()
		creds.ExpiresAt = &expiry
	}

	return creds, nil
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// NormalizeEnvironment maps an empty environment to Square's default.
func NormalizeEnvironment(environment string) string {
	if environment == "" {
		return "production"
	}
	return environment
}

// randomState generates the anti-CSRF state parameter for the
// authorization request.
func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cannot generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// openBrowser launches the operator's default browser at a URL.
func openBrowser(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("refusing to open non-HTTP URL %q", rawURL)
	}

	switch runtime.GOOS {
	case "windows":
		// rundll32 hands the URL to the shell's protocol handler
		// without going through cmd.exe's argument parsing.
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", rawURL).Start()
	case "darwin":
		return exec.Command("open", rawURL).Start()
	default:
		return exec.Command("xdg-open", rawURL).Start()
	}
}
