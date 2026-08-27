package square

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

const twoLocationsResponse = `{
  "locations": [
    {
      "id": "LOC_ATL",
      "name": "Atlanta - Peachtree St",
      "status": "ACTIVE",
      "type": "PHYSICAL",
      "country": "US",
      "currency": "USD",
      "merchant_id": "MERCHANT123",
      "business_name": "Mise Test Kitchen",
      "timezone": "America/New_York",
      "address": {
        "address_line_1": "1100 Peachtree St NE",
        "address_line_2": "Suite 200",
        "locality": "Atlanta",
        "administrative_district_level_1": "GA",
        "postal_code": "30309",
        "country": "US"
      }
    },
    {
      "id": "LOC_MOBILE",
      "name": "Food Truck",
      "status": "ACTIVE",
      "type": "MOBILE",
      "timezone": "America/Chicago",
      "address": {}
    }
  ]
}`

// squareServer stands in for the Square API, asserting the request
// Mise makes and returning a canned response.
func squareServer(t *testing.T, wantPath string, status int, body string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, wantPath, r.URL.Path)
		assert.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		assert.Equal(t, APIVersion, r.Header.Get("Square-Version"))
		assert.Equal(t, UserAgent, r.Header.Get("User-Agent"))

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestClientListLocations(t *testing.T) {
	srv := squareServer(t, "/locations", http.StatusOK, twoLocationsResponse)

	client := NewClient(srv.URL, "test-token")
	locations, err := client.ListLocations(context.Background())
	require.NoError(t, err)
	require.Len(t, locations, 2)

	atl := locations[0]
	assert.Equal(t, "LOC_ATL", atl.ID)
	assert.Equal(t, "Atlanta - Peachtree St", atl.Name)
	assert.Equal(t, "GA", atl.State, "state drives location group filtering")
	assert.Equal(t, "America/New_York", atl.Timezone)
	assert.Equal(t, "1100 Peachtree St NE, Suite 200, Atlanta, GA, 30309", atl.Address)
	assert.Equal(t, "MERCHANT123", atl.Metadata["merchant_id"])
	assert.Equal(t, "PHYSICAL", atl.Metadata["type"])
	assert.Equal(t, "Atlanta", atl.Metadata["city"])

	truck := locations[1]
	assert.Equal(t, "LOC_MOBILE", truck.ID)
	assert.Empty(t, truck.Address, "a location with no address should not render stray separators")
	assert.Empty(t, truck.State)
}

func TestClientListLocationsEmptyAccount(t *testing.T) {
	srv := squareServer(t, "/locations", http.StatusOK, `{"locations": []}`)

	locations, err := NewClient(srv.URL, "test-token").ListLocations(context.Background())
	require.NoError(t, err)
	assert.Empty(t, locations)
}

func TestClientListLocationsUnauthorized(t *testing.T) {
	srv := squareServer(t, "/locations", http.StatusUnauthorized,
		`{"errors":[{"category":"AUTHENTICATION_ERROR","code":"UNAUTHORIZED","detail":"This request could not be authorized."}]}`)

	_, err := NewClient(srv.URL, "test-token").ListLocations(context.Background())
	require.Error(t, err)

	apiErr, ok := err.(*APIError)
	require.True(t, ok, "expected a *APIError, got %T", err)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Equal(t, "UNAUTHORIZED", apiErr.Code)
	assert.False(t, apiErr.Retryable, "a bad token will still be bad on the next try")
}

func TestClientRetriesServerErrors(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"errors":[{"code":"SERVICE_UNAVAILABLE","detail":"try again"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"locations": []}`))
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, "test-token").ListLocations(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 3, calls, "a 503 should be retried up to the attempt limit")
}

func TestClientDoesNotRetryClientErrors(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"errors":[{"code":"BAD_REQUEST","detail":"nope"}]}`))
	}))
	t.Cleanup(srv.Close)

	_, err := NewClient(srv.URL, "test-token").ListLocations(context.Background())
	require.Error(t, err)
	assert.Equal(t, 1, calls, "a 400 is the operator's problem, not a transient one")
}

func TestProviderConfigureAndListLocations(t *testing.T) {
	srv := squareServer(t, "/locations", http.StatusOK, twoLocationsResponse)

	p := &SquareProvider{}
	require.NoError(t, p.Configure(provider.ProviderConfig{
		Platform:    ProviderName,
		Environment: "sandbox",
		Credentials: provider.CredentialsConfig{AccessToken: "test-token"},
	}))
	assert.Equal(t, SandboxBaseURL, p.baseURL)

	// Point the configured provider at the test server.
	p.client = NewClient(srv.URL, "test-token")

	locations, err := p.ListLocations(context.Background())
	require.NoError(t, err)
	assert.Len(t, locations, 2)
}

func TestProviderConfigureRequiresToken(t *testing.T) {
	err := (&SquareProvider{}).Configure(provider.ProviderConfig{Environment: "sandbox"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mise init")
}

func TestProviderConfigureRejectsUnknownEnvironment(t *testing.T) {
	err := (&SquareProvider{}).Configure(provider.ProviderConfig{
		Environment: "staging",
		Credentials: provider.CredentialsConfig{AccessToken: "tok"},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "staging")
}

func TestProviderListLocationsRequiresConfigure(t *testing.T) {
	_, err := (&SquareProvider{}).ListLocations(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestProviderIsRegistered(t *testing.T) {
	p, err := provider.Get(ProviderName)
	require.NoError(t, err)
	assert.Equal(t, ProviderName, p.Name())
	assert.Contains(t, p.ResourceTypes(), "square_catalog_tax")
}
