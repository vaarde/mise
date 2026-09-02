package square

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/provider"
)

// squareLocation mirrors the fields Mise uses from Square's Location
// object (GET /v2/locations). Square returns many more fields; only
// what Mise maps into provider.Location is declared here.
type squareLocation struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Status  string `json:"status"` // ACTIVE or INACTIVE
	Type    string `json:"type"`   // PHYSICAL or MOBILE
	Country string `json:"country"`
	Address struct {
		AddressLine1                 string `json:"address_line_1"`
		AddressLine2                 string `json:"address_line_2"`
		Locality                     string `json:"locality"`                        // city
		AdministrativeDistrictLevel1 string `json:"administrative_district_level_1"` // state
		PostalCode                   string `json:"postal_code"`
		Country                      string `json:"country"`
	} `json:"address"`
	Timezone     string `json:"timezone"`
	Currency     string `json:"currency"`
	MerchantID   string `json:"merchant_id"`
	BusinessName string `json:"business_name"`
}

// listLocationsResponse is the GET /v2/locations response body.
type listLocationsResponse struct {
	Locations []squareLocation `json:"locations"`
}

// ListLocations calls GET /v2/locations and returns every location on
// the account. Square returns all locations in one response — this
// endpoint is not paginated.
func (c *Client) ListLocations(ctx context.Context) ([]provider.Location, error) {
	var resp listLocationsResponse
	if err := c.Get(ctx, "/locations", &resp); err != nil {
		return nil, err
	}

	locations := make([]provider.Location, 0, len(resp.Locations))
	for _, l := range resp.Locations {
		locations = append(locations, toProviderLocation(l))
	}
	return locations, nil
}

// AccountID returns the Square merchant ID the current token reaches.
//
// It implements provider.AccountIdentifier, which is what stops a
// workspace fetched from one merchant being applied to another. Square
// stamps every location with its merchant, so no extra call is needed —
// and every command lists locations anyway.
//
// A token that reaches more than one merchant would make the answer
// ambiguous, so that is reported rather than guessed at.
func (p *SquareProvider) AccountID(ctx context.Context) (string, error) {
	if p.client == nil {
		return "", fmt.Errorf("square provider is not configured")
	}

	var resp listLocationsResponse
	if err := p.client.Get(ctx, "/locations", &resp); err != nil {
		return "", err
	}

	merchants := make([]string, 0, 1)
	for _, l := range resp.Locations {
		if l.MerchantID == "" {
			continue
		}
		if !containsString(merchants, l.MerchantID) {
			merchants = append(merchants, l.MerchantID)
		}
	}
	sort.Strings(merchants)

	switch len(merchants) {
	case 0:
		// Square has always returned a merchant ID; if a future response
		// omits it, say so rather than reporting an empty account.
		return "", fmt.Errorf("Square did not report a merchant ID for any location")
	case 1:
		return merchants[0], nil
	default:
		return "", fmt.Errorf("this token reaches %d Square merchants (%s) — Mise manages one account per workspace",
			len(merchants), strings.Join(merchants, ", "))
	}
}

// toProviderLocation maps a Square location onto Mise's
// platform-agnostic Location type.
func toProviderLocation(l squareLocation) provider.Location {
	return provider.Location{
		ID:       l.ID,
		Name:     l.Name,
		Address:  formatAddress(l),
		State:    l.Address.AdministrativeDistrictLevel1,
		Timezone: l.Timezone,
		Metadata: map[string]string{
			"status":        l.Status,
			"type":          l.Type,
			"country":       l.Country,
			"currency":      l.Currency,
			"city":          l.Address.Locality,
			"postal_code":   l.Address.PostalCode,
			"merchant_id":   l.MerchantID,
			"business_name": l.BusinessName,
		},
	}
}

// formatAddress joins a Square address into a single display string.
func formatAddress(l squareLocation) string {
	parts := []string{
		l.Address.AddressLine1,
		l.Address.AddressLine2,
		l.Address.Locality,
		l.Address.AdministrativeDistrictLevel1,
		l.Address.PostalCode,
	}

	nonEmpty := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, ", ")
}
