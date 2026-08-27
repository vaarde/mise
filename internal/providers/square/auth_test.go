package square

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/credentials"
)

func TestHostForEnvironment(t *testing.T) {
	sandbox, err := hostFor("sandbox")
	require.NoError(t, err)
	assert.Equal(t, SandboxHost, sandbox)

	production, err := hostFor("production")
	require.NoError(t, err)
	assert.Equal(t, ProductionHost, production)

	// An unset environment defaults to production — the safe reading of
	// an ambiguous config is "this is the real account".
	defaulted, err := hostFor("")
	require.NoError(t, err)
	assert.Equal(t, ProductionHost, defaulted)

	_, err = hostFor("staging")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging")
}

func TestBaseURLFor(t *testing.T) {
	sandbox, err := BaseURLFor("sandbox")
	require.NoError(t, err)
	assert.Equal(t, SandboxBaseURL, sandbox)

	production, err := BaseURLFor("production")
	require.NoError(t, err)
	assert.Equal(t, ProductionBaseURL, production)
}

func TestRedirectURL(t *testing.T) {
	assert.Equal(t, "http://localhost:8666/mise/callback", RedirectURL(0))
	assert.Equal(t, "http://localhost:9999/mise/callback", RedirectURL(9999))
}

func TestAuthorizeURL(t *testing.T) {
	cfg := OAuthConfig{
		ClientID:     "sq0idp-app",
		ClientSecret: "sq0csp-secret",
		Environment:  "sandbox",
		CallbackPort: 8666,
	}

	raw, err := cfg.AuthorizeURL("state-token")
	require.NoError(t, err)

	parsed, err := url.Parse(raw)
	require.NoError(t, err)
	assert.Equal(t, "connect.squareupsandbox.com", parsed.Host)
	assert.Equal(t, "/oauth2/authorize", parsed.Path)

	q := parsed.Query()
	assert.Equal(t, "sq0idp-app", q.Get("client_id"))
	assert.Equal(t, "state-token", q.Get("state"))
	assert.Equal(t, "code", q.Get("response_type"))
	assert.Equal(t, "false", q.Get("session"))
	assert.Equal(t, RedirectURL(8666), q.Get("redirect_uri"))
	assert.Equal(t, strings.Join(RequiredScopes, " "), q.Get("scope"))
	assert.NotContains(t, raw, "sq0csp-secret", "the application secret must never appear in the browser URL")
}

func TestAuthorizeURLRejectsUnknownEnvironment(t *testing.T) {
	_, err := OAuthConfig{ClientID: "id", Environment: "staging"}.AuthorizeURL("s")
	require.Error(t, err)
}

func TestRandomStateIsUnique(t *testing.T) {
	first, err := randomState()
	require.NoError(t, err)
	second, err := randomState()
	require.NoError(t, err)

	assert.NotEqual(t, first, second)
	assert.GreaterOrEqual(t, len(first), 32)
}

// callbackRequest drives the OAuth callback handler with a query
// string and returns the HTTP status plus the delivered result.
func callbackRequest(t *testing.T, wantState, query string) (int, callbackResult) {
	t.Helper()

	results := make(chan callbackResult, 1)
	handler := callbackHandler(wantState, results)

	req := httptest.NewRequest(http.MethodGet, CallbackPath+"?"+query, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	select {
	case res := <-results:
		return rec.Code, res
	case <-time.After(time.Second):
		t.Fatal("callback handler did not deliver a result")
		return 0, callbackResult{}
	}
}

func TestCallbackHandlerSuccess(t *testing.T) {
	status, res := callbackRequest(t, "expected", "code=auth-code&state=expected")

	assert.Equal(t, http.StatusOK, status)
	require.NoError(t, res.err)
	assert.Equal(t, "auth-code", res.code)
}

func TestCallbackHandlerRejectsStateMismatch(t *testing.T) {
	status, res := callbackRequest(t, "expected", "code=auth-code&state=forged")

	assert.Equal(t, http.StatusBadRequest, status)
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "state mismatch")
	assert.Empty(t, res.code, "a code from a mismatched state must never be used")
}

func TestCallbackHandlerReportsSquareError(t *testing.T) {
	status, res := callbackRequest(t, "expected",
		"error=access_denied&error_description=Merchant+declined&state=expected")

	assert.Equal(t, http.StatusBadRequest, status)
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "Merchant declined")
}

func TestCallbackHandlerRequiresCode(t *testing.T) {
	status, res := callbackRequest(t, "expected", "state=expected")

	assert.Equal(t, http.StatusBadRequest, status)
	require.Error(t, res.err)
	assert.Contains(t, res.err.Error(), "authorization code")
}

func TestCallbackPageEscapesUntrustedText(t *testing.T) {
	// error_description comes off the wire, so it must be escaped
	// before it lands in the page Mise serves back to the browser.
	results := make(chan callbackResult, 1)
	handler := callbackHandler("expected", results)

	req := httptest.NewRequest(http.MethodGet,
		CallbackPath+"?error=bad&error_description="+url.QueryEscape("<script>alert(1)</script>"), nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.NotContains(t, rec.Body.String(), "<script>")
	assert.Contains(t, rec.Body.String(), "&lt;script&gt;")
}

// tokenServer stands in for Square's /oauth2/token endpoint.
func tokenServer(t *testing.T, status int, body interface{}, capture *map[string]string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/oauth2/token", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		assert.Equal(t, APIVersion, r.Header.Get("Square-Version"))

		if capture != nil {
			raw, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var parsed map[string]string
			require.NoError(t, json.Unmarshal(raw, &parsed))
			*capture = parsed
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestExchangeStoresToken(t *testing.T) {
	expiry := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)

	var sent map[string]string
	srv := tokenServer(t, http.StatusOK, map[string]interface{}{
		"access_token":  "sq0atp-access",
		"token_type":    "bearer",
		"expires_at":    expiry.Format(time.RFC3339),
		"merchant_id":   "MERCHANT123",
		"refresh_token": "sq0rtp-refresh",
	}, &sent)

	cfg := OAuthConfig{
		ClientID:     "sq0idp-app",
		ClientSecret: "sq0csp-secret",
		Environment:  "sandbox",
		CallbackPort: 8666,
		host:         srv.URL,
	}

	creds, err := cfg.Exchange(context.Background(), "auth-code")
	require.NoError(t, err)

	assert.Equal(t, "authorization_code", sent["grant_type"])
	assert.Equal(t, "auth-code", sent["code"])
	assert.Equal(t, "sq0idp-app", sent["client_id"])
	assert.Equal(t, "sq0csp-secret", sent["client_secret"])
	assert.Equal(t, RedirectURL(8666), sent["redirect_uri"])

	assert.Equal(t, ProviderName, creds.Provider)
	assert.Equal(t, credentials.MethodOAuth2, creds.Method)
	assert.Equal(t, "sandbox", creds.Environment)
	assert.Equal(t, "sq0atp-access", creds.AccessToken)
	assert.Equal(t, "sq0rtp-refresh", creds.RefreshToken)
	assert.Equal(t, "MERCHANT123", creds.MerchantID)
	require.NotNil(t, creds.ExpiresAt)
	assert.True(t, expiry.Equal(*creds.ExpiresAt))
	assert.NoError(t, creds.Validate())
}

func TestExchangeWithoutExpiryLeavesTokenNonExpiring(t *testing.T) {
	srv := tokenServer(t, http.StatusOK, map[string]interface{}{
		"access_token": "sq0atp-access",
		"merchant_id":  "MERCHANT123",
	}, nil)

	creds, err := OAuthConfig{Environment: "sandbox", host: srv.URL}.Exchange(context.Background(), "code")
	require.NoError(t, err)
	assert.Nil(t, creds.ExpiresAt)
	assert.False(t, creds.Expired())
}

func TestExchangeSurfacesOAuthError(t *testing.T) {
	srv := tokenServer(t, http.StatusUnauthorized, map[string]interface{}{
		"error":             "invalid_grant",
		"error_description": "Authorization code is expired",
	}, nil)

	_, err := OAuthConfig{Environment: "sandbox", host: srv.URL}.Exchange(context.Background(), "stale-code")
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok, "expected a *APIError, got %T", err)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Equal(t, "invalid_grant", apiErr.Code)
	assert.Contains(t, apiErr.Message, "expired")
	assert.False(t, apiErr.Retryable, "a bad authorization code must not be retried")
}

func TestExchangeSurfacesRejectedApplicationError(t *testing.T) {
	// Square answers a bad application ID/secret with a different error
	// shape than a bad grant.
	srv := tokenServer(t, http.StatusUnauthorized, map[string]interface{}{
		"type":    "service.not_authorized",
		"message": "Not Authorized",
	}, nil)

	_, err := OAuthConfig{Environment: "sandbox", host: srv.URL}.Exchange(context.Background(), "code")
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok, "expected a *APIError, got %T", err)
	assert.Equal(t, "service.not_authorized", apiErr.Code)
	assert.Equal(t, "Not Authorized", apiErr.Message)
	assert.NotContains(t, apiErr.Message, "{", "the raw JSON body should not leak into the message")
}

func TestCallbackPageDoesNotClaimSuccessBeforeTheExchange(t *testing.T) {
	// The page is served before the token exchange runs, so it must not
	// tell the operator they are connected yet.
	results := make(chan callbackResult, 1)
	handler := callbackHandler("expected", results)

	req := httptest.NewRequest(http.MethodGet, CallbackPath+"?code=c&state=expected", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "Authorization received")
}

func TestExchangeRejectsEmptyToken(t *testing.T) {
	srv := tokenServer(t, http.StatusOK, map[string]interface{}{"merchant_id": "M"}, nil)

	_, err := OAuthConfig{Environment: "sandbox", host: srv.URL}.Exchange(context.Background(), "code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty access token")
}

func TestExchangeRejectsUnparseableExpiry(t *testing.T) {
	srv := tokenServer(t, http.StatusOK, map[string]interface{}{
		"access_token": "sq0atp-access",
		"expires_at":   "next tuesday",
	}, nil)

	_, err := OAuthConfig{Environment: "sandbox", host: srv.URL}.Exchange(context.Background(), "code")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expiry")
}

func TestRefreshKeepsExistingRefreshToken(t *testing.T) {
	var sent map[string]string
	srv := tokenServer(t, http.StatusOK, map[string]interface{}{
		"access_token": "sq0atp-new",
		"merchant_id":  "MERCHANT123",
		// Square may omit refresh_token on a refresh response.
	}, &sent)

	existing := &credentials.Credentials{
		Provider:     ProviderName,
		Method:       credentials.MethodOAuth2,
		Environment:  "sandbox",
		AccessToken:  "sq0atp-old",
		RefreshToken: "sq0rtp-refresh",
		ClientID:     "sq0idp-app",
		ClientSecret: "sq0csp-secret",
	}

	cfg := OAuthConfig{
		ClientID:     existing.ClientID,
		ClientSecret: existing.ClientSecret,
		Environment:  existing.Environment,
		host:         srv.URL,
	}

	refreshed, err := cfg.refresh(context.Background(), existing)
	require.NoError(t, err)

	assert.Equal(t, "refresh_token", sent["grant_type"])
	assert.Equal(t, "sq0rtp-refresh", sent["refresh_token"])
	assert.Equal(t, "sq0atp-new", refreshed.AccessToken)
	assert.Equal(t, "sq0rtp-refresh", refreshed.RefreshToken, "the old refresh token must be carried forward")
}

func TestRefreshRejectsUnrefreshableCredentials(t *testing.T) {
	tests := []struct {
		name    string
		creds   credentials.Credentials
		wantErr string
	}{
		{
			name:    "personal access token",
			creds:   credentials.Credentials{Method: credentials.MethodAccessToken},
			wantErr: "only OAuth2",
		},
		{
			name:    "no refresh token",
			creds:   credentials.Credentials{Method: credentials.MethodOAuth2},
			wantErr: "no refresh token",
		},
		{
			name:    "no application credentials",
			creds:   credentials.Credentials{Method: credentials.MethodOAuth2, RefreshToken: "r"},
			wantErr: "application credentials",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Refresh(context.Background(), &tc.creds)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestAuthorizeRequiresApplicationCredentials(t *testing.T) {
	_, err := OAuthConfig{Environment: "sandbox"}.Authorize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "application ID")

	_, err = OAuthConfig{ClientID: "id", Environment: "sandbox"}.Authorize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "application secret")
}

func TestAuthorizeCancelsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := OAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		Environment:  "sandbox",
		CallbackPort: freePort(t),
		OpenBrowser:  false,
		Out:          io.Discard,
	}

	_, err := cfg.Authorize(ctx)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cancelled")
}

func TestAuthorizeReportsPortConflict(t *testing.T) {
	// Hold the port for the duration of the test so Authorize's listener
	// cannot bind it.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	cfg := OAuthConfig{
		ClientID:     "id",
		ClientSecret: "secret",
		Environment:  "sandbox",
		CallbackPort: listener.Addr().(*net.TCPAddr).Port,
		Out:          io.Discard,
	}

	_, err = cfg.Authorize(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--callback-port")
}

// freePort returns a TCP port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := listener.Addr().(*net.TCPAddr).Port
	require.NoError(t, listener.Close())
	return port
}

func TestOpenBrowserRejectsNonHTTPURL(t *testing.T) {
	require.Error(t, openBrowser("file:///etc/passwd"))
	require.Error(t, openBrowser("javascript:alert(1)"))
}
