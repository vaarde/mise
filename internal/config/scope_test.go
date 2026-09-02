package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

// threeLocations is a small estate spanning two states.
func threeLocations() []provider.Location {
	return []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta", State: "GA"},
		{ID: "LOC_SAV", Name: "Savannah", State: "GA"},
		{ID: "LOC_NSH", Name: "Nashville", State: "TN"},
	}
}

func TestGroupMatchingNothingIsRejected(t *testing.T) {
	// This is the one that matters. A filter matching no location used
	// to resolve to an empty list, and Square reads an empty location
	// list as present_at_all_locations — so a typo in a state code
	// widened the write to every location on the account instead of
	// narrowing it to none.
	groups := map[string]LocationGroup{
		"florida": {Filter: map[string]interface{}{"state": "FL"}},
	}

	_, err := ResolveLocations("${group.florida}", groups, threeLocations())
	require.Error(t, err)

	assert.Contains(t, err.Error(), "florida")
	assert.Contains(t, err.Error(), "matched none")
	assert.Contains(t, err.Error(), "typo")
}

func TestGroupMatchingSomeLocationsIsFine(t *testing.T) {
	groups := map[string]LocationGroup{
		"georgia": {Filter: map[string]interface{}{"state": "GA"}},
	}

	ids, err := ResolveLocations("${group.georgia}", groups, threeLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SAV"}, ids)
}

func TestEmptyLocationListIsRejected(t *testing.T) {
	_, err := ResolveLocations([]interface{}{}, nil, threeLocations())
	require.Error(t, err)

	assert.Contains(t, err.Error(), "empty list")
	assert.Contains(t, err.Error(), "remove the field",
		"the error should say how to actually mean every location")
}

func TestWildcardWithNoLocationsIsRejected(t *testing.T) {
	// An account with no locations cannot have a resource scoped to
	// "everywhere" either — there is no everywhere yet.
	_, err := ResolveLocations(FilterAll, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no locations")
	assert.Contains(t, err.Error(), "mise fetch")
}

func TestUnscopedResourceWithNoLocationsIsRejected(t *testing.T) {
	_, err := ResolveLocations(nil, nil, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no locations")
}

func TestUnscopedResourceCoversEveryLocation(t *testing.T) {
	ids, err := ResolveLocations(nil, nil, threeLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_NSH", "LOC_SAV"}, ids,
		"a resource with no stated scope applies everywhere")
}
