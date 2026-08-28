package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

func testLocations() []provider.Location {
	return []provider.Location{
		{ID: "LOC_ATL", Name: "Atlanta", State: "GA", Metadata: map[string]string{"status": "ACTIVE"}},
		{ID: "LOC_SAV", Name: "Savannah", State: "GA", Metadata: map[string]string{"status": "ACTIVE"}},
		{ID: "LOC_PDX", Name: "Portland", State: "OR", Metadata: map[string]string{"status": "ACTIVE"}},
		{ID: "LOC_SEA", Name: "Seattle", State: "WA", Metadata: map[string]string{"status": "INACTIVE"}},
	}
}

func TestParseGroupRef(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"${group.georgia}", "georgia", true},
		{"  ${group.all}  ", "all", true},
		{"${group.}", "", false},
		{"group.georgia", "", false},
		{"${georgia}", "", false},
		{"*", "", false},
		{"", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			got, ok := ParseGroupRef(tc.in)
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestResolveGroupWildcard(t *testing.T) {
	ids, err := ResolveGroup(LocationGroup{Filter: "*"}, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_PDX", "LOC_SAV", "LOC_SEA"}, ids)
}

func TestResolveGroupBySingleState(t *testing.T) {
	group := LocationGroup{Filter: map[string]interface{}{"state": "GA"}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SAV"}, ids)
}

func TestResolveGroupByStateList(t *testing.T) {
	group := LocationGroup{Filter: map[string]interface{}{
		"state": []interface{}{"CA", "OR", "WA"},
	}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_PDX", "LOC_SEA"}, ids)
}

func TestResolveGroupIsCaseInsensitive(t *testing.T) {
	// Operators type state codes both ways; a lowercase "ga" should not
	// silently match nothing.
	group := LocationGroup{Filter: map[string]interface{}{"state": "ga"}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SAV"}, ids)
}

func TestResolveGroupByExplicitIDs(t *testing.T) {
	group := LocationGroup{Filter: map[string]interface{}{
		"ids": []interface{}{"LOC_ATL", "LOC_SEA"},
	}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SEA"}, ids)
}

func TestResolveGroupByProviderMetadata(t *testing.T) {
	// An unrecognized field falls through to provider metadata, so an
	// adapter can expose filterable fields the config package never
	// learns about.
	group := LocationGroup{Filter: map[string]interface{}{"status": "INACTIVE"}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_SEA"}, ids)
}

func TestResolveGroupCombinesMatchersWithAnd(t *testing.T) {
	group := LocationGroup{Filter: map[string]interface{}{
		"state":  []interface{}{"GA", "OR"},
		"status": "ACTIVE",
	}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_PDX", "LOC_SAV"}, ids)
}

func TestResolveGroupMatchingNothing(t *testing.T) {
	group := LocationGroup{Filter: map[string]interface{}{"state": "TX"}}

	ids, err := ResolveGroup(group, testLocations())
	require.NoError(t, err)
	assert.Empty(t, ids)
}

func TestResolveGroupRejectsBadFilters(t *testing.T) {
	_, err := ResolveGroup(LocationGroup{Filter: nil}, testLocations())
	require.Error(t, err)

	_, err = ResolveGroup(LocationGroup{Filter: "everything"}, testLocations())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "everything")

	_, err = ResolveGroup(LocationGroup{Filter: map[string]interface{}{"state": 42}}, testLocations())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "state")
}

func TestResolveLocationsFromGroupReference(t *testing.T) {
	groups := map[string]LocationGroup{
		"georgia": {Filter: map[string]interface{}{"state": "GA"}},
		"all":     {Filter: "*"},
	}

	ids, err := ResolveLocations("${group.georgia}", groups, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SAV"}, ids)

	ids, err = ResolveLocations(GroupAll, groups, testLocations())
	require.NoError(t, err)
	assert.Len(t, ids, 4)
}

func TestResolveLocationsDefaultsToEverywhere(t *testing.T) {
	// A resource that does not state a scope applies everywhere.
	ids, err := ResolveLocations(nil, nil, testLocations())
	require.NoError(t, err)
	assert.Len(t, ids, 4)

	ids, err = ResolveLocations("*", nil, testLocations())
	require.NoError(t, err)
	assert.Len(t, ids, 4)
}

func TestResolveLocationsFromExplicitList(t *testing.T) {
	ids, err := ResolveLocations([]interface{}{"LOC_SAV", "LOC_ATL"}, nil, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_ATL", "LOC_SAV"}, ids, "explicit lists are sorted for stable output")

	ids, err = ResolveLocations([]string{"LOC_PDX"}, nil, testLocations())
	require.NoError(t, err)
	assert.Equal(t, []string{"LOC_PDX"}, ids)
}

func TestResolveLocationsRejectsUnknownGroup(t *testing.T) {
	groups := map[string]LocationGroup{"georgia": {Filter: "*"}}

	_, err := ResolveLocations("${group.florida}", groups, testLocations())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "florida")
	assert.Contains(t, err.Error(), "georgia", "the error should list what groups exist")
}

func TestResolveLocationsRejectsUnknownLocationID(t *testing.T) {
	_, err := ResolveLocations([]interface{}{"LOC_ATL", "LOC_GONE"}, nil, testLocations())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOC_GONE")
	assert.Contains(t, err.Error(), "mise fetch")
}

func TestResolveLocationsRejectsMalformedValue(t *testing.T) {
	_, err := ResolveLocations("group.georgia", nil, testLocations())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "${group.name}")

	_, err = ResolveLocations([]interface{}{42}, nil, testLocations())
	require.Error(t, err)
}
