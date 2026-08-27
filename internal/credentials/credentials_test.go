package credentials

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	expiry := time.Now().Add(30 * 24 * time.Hour).UTC().Truncate(time.Second)

	original := &Credentials{
		Provider:     "square",
		Method:       MethodOAuth2,
		Environment:  "sandbox",
		AccessToken:  "sq0atp-token",
		RefreshToken: "sq0rtp-refresh",
		ExpiresAt:    &expiry,
		MerchantID:   "MERCHANT123",
		ClientID:     "sq0idp-app",
		ClientSecret: "sq0csp-secret",
	}

	require.NoError(t, original.Save(dir))

	loaded, err := Load(dir)
	require.NoError(t, err)

	assert.Equal(t, 1, loaded.Version, "Save should stamp a version")
	assert.Equal(t, original.AccessToken, loaded.AccessToken)
	assert.Equal(t, original.RefreshToken, loaded.RefreshToken)
	assert.Equal(t, original.MerchantID, loaded.MerchantID)
	assert.Equal(t, original.ClientID, loaded.ClientID)
	assert.Equal(t, original.ClientSecret, loaded.ClientSecret)
	require.NotNil(t, loaded.ExpiresAt)
	assert.True(t, expiry.Equal(*loaded.ExpiresAt))
	assert.False(t, loaded.ObtainedAt.IsZero(), "Save should stamp ObtainedAt")
}

func TestSaveUsesRestrictivePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not meaningful on Windows")
	}

	dir := t.TempDir()
	creds := &Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "tok"}
	require.NoError(t, creds.Save(dir))

	fileInfo, err := os.Stat(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), fileInfo.Mode().Perm(), "credentials must not be world-readable")

	dirInfo, err := os.Stat(filepath.Join(dir, Dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm(), ".mise must not be world-readable")
}

func TestSaveTightensPermissionsOnExistingFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix file modes are not meaningful on Windows")
	}

	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, Dir), 0o700))
	require.NoError(t, os.WriteFile(Path(dir), []byte("{}"), 0o644))

	creds := &Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "tok"}
	require.NoError(t, creds.Save(dir))

	info, err := os.Stat(Path(dir))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	assert.False(t, Exists(dir))

	creds := &Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "tok"}
	require.NoError(t, creds.Save(dir))
	assert.True(t, Exists(dir))
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		creds   Credentials
		wantErr string
	}{
		{
			name:  "valid access token",
			creds: Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "tok"},
		},
		{
			name:    "missing provider",
			creds:   Credentials{Method: MethodAccessToken, AccessToken: "tok"},
			wantErr: "provider",
		},
		{
			name:    "missing method",
			creds:   Credentials{Provider: "square", AccessToken: "tok"},
			wantErr: "auth method",
		},
		{
			name:    "unknown method",
			creds:   Credentials{Provider: "square", Method: "magic", AccessToken: "tok"},
			wantErr: "unknown auth method",
		},
		{
			name:    "missing token",
			creds:   Credentials{Provider: "square", Method: MethodOAuth2},
			wantErr: "access token",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.creds.Validate()
			if tc.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

func TestSaveRejectsInvalidCredentials(t *testing.T) {
	dir := t.TempDir()
	creds := &Credentials{Provider: "square", Method: MethodAccessToken} // no token

	require.Error(t, creds.Save(dir))
	assert.False(t, Exists(dir), "an invalid save must not leave a file behind")
}

func TestFromEnvPrefersMisePrefixedVariable(t *testing.T) {
	t.Setenv("SQUARE_ACCESS_TOKEN", "plain")
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "prefixed")

	token, envVar := FromEnv("square")
	assert.Equal(t, "prefixed", token)
	assert.Equal(t, "MISE_SQUARE_ACCESS_TOKEN", envVar)
}

func TestFromEnvFallsBackToPlatformVariable(t *testing.T) {
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")
	t.Setenv("SQUARE_ACCESS_TOKEN", "  plain  ")

	token, envVar := FromEnv("square")
	assert.Equal(t, "plain", token, "surrounding whitespace should be trimmed")
	assert.Equal(t, "SQUARE_ACCESS_TOKEN", envVar)
}

func TestFromEnvEmpty(t *testing.T) {
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")
	t.Setenv("SQUARE_ACCESS_TOKEN", "")

	token, envVar := FromEnv("square")
	assert.Empty(t, token)
	assert.Empty(t, envVar)
}

func TestResolvePrefersEnvironmentOverStoredFile(t *testing.T) {
	dir := t.TempDir()
	stored := &Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "from-file"}
	require.NoError(t, stored.Save(dir))

	t.Setenv("SQUARE_ACCESS_TOKEN", "from-env")

	resolved, err := Resolve(dir, "square", "sandbox")
	require.NoError(t, err)
	assert.Equal(t, "from-env", resolved.AccessToken)
	assert.Equal(t, MethodAccessToken, resolved.Method)
	assert.Equal(t, "sandbox", resolved.Environment)
}

func TestResolveFallsBackToStoredFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MISE_SQUARE_ACCESS_TOKEN", "")
	t.Setenv("SQUARE_ACCESS_TOKEN", "")

	stored := &Credentials{Provider: "square", Method: MethodAccessToken, AccessToken: "from-file"}
	require.NoError(t, stored.Save(dir))

	resolved, err := Resolve(dir, "square", "sandbox")
	require.NoError(t, err)
	assert.Equal(t, "from-file", resolved.AccessToken)
}

func TestExpired(t *testing.T) {
	past := time.Now().Add(-time.Hour)
	future := time.Now().Add(time.Hour)

	assert.False(t, (&Credentials{}).Expired(), "a token with no expiry never expires")
	assert.True(t, (&Credentials{ExpiresAt: &past}).Expired())
	assert.False(t, (&Credentials{ExpiresAt: &future}).Expired())
}

func TestNeedsRefresh(t *testing.T) {
	soon := time.Now().Add(24 * time.Hour)
	later := time.Now().Add(30 * 24 * time.Hour)

	oauth := func(expiry *time.Time) *Credentials {
		return &Credentials{Method: MethodOAuth2, RefreshToken: "r", ExpiresAt: expiry}
	}

	assert.True(t, oauth(&soon).NeedsRefresh(), "a token expiring inside the window needs refreshing")
	assert.False(t, oauth(&later).NeedsRefresh())
	assert.False(t, oauth(nil).NeedsRefresh(), "no expiry means nothing to refresh")

	noRefreshToken := &Credentials{Method: MethodOAuth2, ExpiresAt: &soon}
	assert.False(t, noRefreshToken.NeedsRefresh())

	personalToken := &Credentials{Method: MethodAccessToken, ExpiresAt: &soon}
	assert.False(t, personalToken.NeedsRefresh(), "personal access tokens cannot be refreshed")
}
