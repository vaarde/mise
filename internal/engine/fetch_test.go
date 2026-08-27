package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/vaarde/mise/internal/provider"
)

// stubProvider is a scripted POS adapter. Each entry in resources is
// keyed by "type@location", mirroring how a location-scoped API behaves.
type stubProvider struct {
	locations []provider.Location
	types     []string
	resources map[string][]*provider.Resource
	readErr   map[string]error

	mu            sync.Mutex
	readAllCalls  int
	maxConcurrent int
	concurrent    int
	delay         time.Duration
}

func (s *stubProvider) Name() string { return "stub" }

func (s *stubProvider) Configure(provider.ProviderConfig) error { return nil }

func (s *stubProvider) ListLocations(context.Context) ([]provider.Location, error) {
	return s.locations, nil
}

func (s *stubProvider) ResourceTypes() []string { return s.types }

func (s *stubProvider) ReadAll(ctx context.Context, resourceType, locationID string) ([]*provider.Resource, error) {
	key := resourceType + "@" + locationID

	s.mu.Lock()
	s.readAllCalls++
	s.concurrent++
	if s.concurrent > s.maxConcurrent {
		s.maxConcurrent = s.concurrent
	}
	err := s.readErr[key]
	s.mu.Unlock()

	if s.delay > 0 {
		select {
		case <-time.After(s.delay):
		case <-ctx.Done():
		}
	}

	defer func() {
		s.mu.Lock()
		s.concurrent--
		s.mu.Unlock()
	}()

	if err != nil {
		return nil, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}

	// Return copies so the engine cannot mutate the fixture between reads.
	out := make([]*provider.Resource, 0, len(s.resources[key]))
	for _, res := range s.resources[key] {
		clone := *res
		clone.LocationID = locationID
		out = append(out, &clone)
	}
	return out, nil
}

func (s *stubProvider) Read(context.Context, string, string, string) (*provider.Resource, error) {
	return nil, fmt.Errorf("not implemented")
}

func (s *stubProvider) Create(context.Context, string, *provider.Resource, string) (string, error) {
	return "", fmt.Errorf("not implemented")
}

func (s *stubProvider) Update(context.Context, string, string, *provider.Resource, string) error {
	return fmt.Errorf("not implemented")
}

func (s *stubProvider) Delete(context.Context, string, string, string) error {
	return fmt.Errorf("not implemented")
}

func tax(id, name, percentage string) *provider.Resource {
	return &provider.Resource{
		Type:       "square_catalog_tax",
		Name:       name,
		ProviderID: id,
		Version:    "1",
		Properties: map[string]interface{}{"name": name, "percentage": percentage},
	}
}

func TestFetchDeduplicatesResourcesAcrossLocations(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A", Name: "Atlanta"}, {ID: "LOC_B", Name: "Boston"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_A": {tax("TAX_1", "State Tax", "4.5")},
			"square_catalog_tax@LOC_B": {tax("TAX_1", "State Tax", "4.5")},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err)

	// One catalog object seen at two locations is one resource, not two.
	require.Len(t, result.Resources, 1)
	assert.Equal(t, "TAX_1", result.Resources[0].ProviderID)
	assert.Equal(t, []string{"LOC_A", "LOC_B"}, result.Resources[0].LocationIDs)
}

func TestFetchRecordsPartialLocationCoverage(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}, {ID: "LOC_B"}, {ID: "LOC_C"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_A": {tax("TAX_1", "Alcohol Tax", "6.0")},
			"square_catalog_tax@LOC_C": {tax("TAX_1", "Alcohol Tax", "6.0")},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err)

	require.Len(t, result.Resources, 1)
	assert.Equal(t, []string{"LOC_A", "LOC_C"}, result.Resources[0].LocationIDs)
}

func TestFetchSkipsRequestedTypes(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}},
		types:     []string{"square_location", "square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_A": {tax("TAX_1", "State Tax", "4.5")},
			"square_location@LOC_A":    {{Type: "square_location", Name: "Atlanta", ProviderID: "LOC_A"}},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{SkipTypes: []string{"square_location"}})
	require.NoError(t, err)

	require.Len(t, result.Resources, 1)
	assert.Equal(t, "square_catalog_tax", result.Resources[0].Type)
}

func TestFetchReadsUnscopedWhenAccountHasNoLocations(t *testing.T) {
	stub := &stubProvider{
		locations: nil,
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@": {tax("TAX_1", "State Tax", "4.5")},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err)

	require.Len(t, result.Resources, 1, "a catalog exists even when no locations do")
	assert.Empty(t, result.Resources[0].LocationIDs)
}

func TestFetchBoundsParallelism(t *testing.T) {
	locations := make([]provider.Location, 12)
	for i := range locations {
		locations[i] = provider.Location{ID: fmt.Sprintf("LOC_%02d", i)}
	}

	stub := &stubProvider{
		locations: locations,
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{},
		delay:     20 * time.Millisecond,
	}

	_, err := Fetch(context.Background(), stub, FetchOptions{Parallelism: 3})
	require.NoError(t, err)

	assert.LessOrEqual(t, stub.maxConcurrent, 3, "the semaphore must cap concurrent location reads")
	assert.Greater(t, stub.maxConcurrent, 1, "locations should actually run in parallel")
}

func TestFetchReportsReadFailures(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}, {ID: "LOC_B"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{},
		readErr: map[string]error{
			"square_catalog_tax@LOC_B": fmt.Errorf("429 rate limited"),
		},
	}

	_, err := Fetch(context.Background(), stub, FetchOptions{Parallelism: 1})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LOC_B")
	assert.Contains(t, err.Error(), "square_catalog_tax")
	assert.Contains(t, err.Error(), "rate limited")
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"GA State Sales Tax", "ga_state_sales_tax"},
		{"Happy Hour!", "happy_hour"},
		{"  Trimmed  ", "trimmed"},
		{"Multiple   Spaces", "multiple_spaces"},
		{"Café Latte", "caf_latte"},
		{"20% Off", "20_off"},
		{"---", "unnamed"},
		{"", "unnamed"},
		{"A/B & C", "a_b_c"},
	}

	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			assert.Equal(t, tc.want, slugify(tc.in))
		})
	}
}

func TestFetchDisambiguatesCollidingNames(t *testing.T) {
	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}},
		types:     []string{"square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_tax@LOC_A": {
				tax("TAX_2", "Sales Tax", "4.5"),
				tax("TAX_1", "Sales Tax", "7.0"),
				tax("TAX_3", "Sales Tax", "9.0"),
			},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err)
	require.Len(t, result.Resources, 3)

	names := []string{result.Resources[0].Name, result.Resources[1].Name, result.Resources[2].Name}
	assert.ElementsMatch(t, []string{"sales_tax", "sales_tax_2", "sales_tax_3"}, names)

	// Suffixes are handed out in provider-ID order, so a second fetch
	// produces the same names rather than shuffling them between files.
	byID := map[string]string{}
	for _, res := range result.Resources {
		byID[res.ProviderID] = res.Name
	}
	assert.Equal(t, "sales_tax", byID["TAX_1"])
	assert.Equal(t, "sales_tax_2", byID["TAX_2"])
	assert.Equal(t, "sales_tax_3", byID["TAX_3"])
}

func TestFetchNamingIsStableAcrossRuns(t *testing.T) {
	newStub := func() *stubProvider {
		return &stubProvider{
			locations: []provider.Location{{ID: "LOC_A"}, {ID: "LOC_B"}},
			types:     []string{"square_catalog_tax"},
			resources: map[string][]*provider.Resource{
				"square_catalog_tax@LOC_A": {tax("TAX_2", "Tax", "1"), tax("TAX_1", "Tax", "2")},
				"square_catalog_tax@LOC_B": {tax("TAX_1", "Tax", "2"), tax("TAX_2", "Tax", "1")},
			},
		}
	}

	first, err := Fetch(context.Background(), newStub(), FetchOptions{})
	require.NoError(t, err)
	second, err := Fetch(context.Background(), newStub(), FetchOptions{})
	require.NoError(t, err)

	require.Len(t, first.Resources, 2)
	for i := range first.Resources {
		assert.Equal(t, first.Resources[i].Name, second.Resources[i].Name)
		assert.Equal(t, first.Resources[i].ProviderID, second.Resources[i].ProviderID)
	}
}

func TestFetchResolvesReferences(t *testing.T) {
	item := &provider.Resource{
		Type:       "square_catalog_item",
		Name:       "Summer Lemonade",
		ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"name":     "Summer Lemonade",
			"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_1"},
			"tax_ids": []interface{}{
				provider.Ref{ResourceType: "square_catalog_tax", ProviderID: "TAX_1"},
			},
		},
	}
	category := &provider.Resource{
		Type: "square_catalog_category", Name: "Beverages", ProviderID: "CAT_1",
		Properties: map[string]interface{}{"name": "Beverages"},
	}

	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}},
		types:     []string{"square_catalog_category", "square_catalog_item", "square_catalog_tax"},
		resources: map[string][]*provider.Resource{
			"square_catalog_item@LOC_A":     {item},
			"square_catalog_category@LOC_A": {category},
			"square_catalog_tax@LOC_A":      {tax("TAX_1", "State Tax", "4.5")},
		},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err)
	assert.Empty(t, result.Warnings)

	var fetchedItem *FetchedResource
	for _, res := range result.Resources {
		if res.ProviderID == "ITEM_1" {
			fetchedItem = res
		}
	}
	require.NotNil(t, fetchedItem)

	assert.Equal(t, "ref(square_catalog_category.beverages)", fetchedItem.Properties["category"])
	assert.Equal(t, []interface{}{"ref(square_catalog_tax.state_tax)"}, fetchedItem.Properties["tax_ids"])
}

func TestFetchWarnsOnUnresolvableReference(t *testing.T) {
	item := &provider.Resource{
		Type: "square_catalog_item", Name: "Orphan", ProviderID: "ITEM_1",
		Properties: map[string]interface{}{
			"name":     "Orphan",
			"category": provider.Ref{ResourceType: "square_catalog_category", ProviderID: "CAT_GONE"},
		},
	}

	stub := &stubProvider{
		locations: []provider.Location{{ID: "LOC_A"}},
		types:     []string{"square_catalog_item"},
		resources: map[string][]*provider.Resource{"square_catalog_item@LOC_A": {item}},
	}

	result, err := Fetch(context.Background(), stub, FetchOptions{})
	require.NoError(t, err, "one dangling reference must not fail the whole fetch")

	require.Len(t, result.Warnings, 1)
	assert.Contains(t, result.Warnings[0], "CAT_GONE")
	assert.Equal(t, "CAT_GONE", result.Resources[0].Properties["category"],
		"an unresolvable reference falls back to the raw provider ID")
}

func TestFetchResolvesNestedReferences(t *testing.T) {
	index := map[resourceKey]string{
		{resourceType: "square_catalog_tax", providerID: "TAX_1"}: "square_catalog_tax.state_tax",
	}

	value := map[string]interface{}{
		"outer": []interface{}{
			map[string]interface{}{
				"inner": provider.Ref{ResourceType: "square_catalog_tax", ProviderID: "TAX_1"},
			},
		},
	}

	resolved, unresolved := resolveRefs(value, index)
	assert.Empty(t, unresolved)

	outer := resolved.(map[string]interface{})["outer"].([]interface{})
	inner := outer[0].(map[string]interface{})
	assert.Equal(t, "ref(square_catalog_tax.state_tax)", inner["inner"])
}

func TestCountsByType(t *testing.T) {
	result := &FetchResult{Resources: []*FetchedResource{
		{Type: "square_catalog_tax"},
		{Type: "square_catalog_tax"},
		{Type: "square_catalog_item"},
	}}

	assert.Equal(t, 3, result.Total())
	assert.Equal(t, map[string]int{"square_catalog_tax": 2, "square_catalog_item": 1}, result.CountsByType())
}

func TestToResourceDefsUsesGroupAllWhenEverywhere(t *testing.T) {
	result := &FetchResult{
		Locations: []provider.Location{{ID: "LOC_A"}, {ID: "LOC_B"}},
		Resources: []*FetchedResource{
			{Type: "square_catalog_tax", Name: "everywhere", LocationIDs: []string{"LOC_A", "LOC_B"}},
			{Type: "square_catalog_tax", Name: "atlanta_only", LocationIDs: []string{"LOC_A"}},
		},
	}

	defs := result.ToResourceDefs()
	require.Len(t, defs, 2)
	assert.Equal(t, "${group.all}", defs[0].Locations)
	assert.Equal(t, []string{"LOC_A"}, defs[1].Locations)
}

func TestToState(t *testing.T) {
	fetchedAt := time.Date(2026, 9, 15, 14, 30, 0, 0, time.UTC)

	result := &FetchResult{
		Locations: []provider.Location{{ID: "LOC_A", Name: "Atlanta", State: "GA", Timezone: "America/New_York"}},
		Resources: []*FetchedResource{{
			Type:        "square_catalog_tax",
			Name:        "ga_state_sales_tax",
			ProviderID:  "TAX_1",
			Version:     "1700000000000",
			LocationIDs: []string{"LOC_A"},
			Properties:  map[string]interface{}{"percentage": "4.5"},
		}},
	}

	s := result.ToState("square", fetchedAt)

	assert.Equal(t, "square", s.Provider)
	require.NotNil(t, s.LastFetch)
	assert.Equal(t, fetchedAt, *s.LastFetch)

	entry, ok := s.Resources["square_catalog_tax.ga_state_sales_tax"]
	require.True(t, ok, "state is keyed by type.name")
	assert.Equal(t, "TAX_1", entry.ProviderID)
	assert.Equal(t, "1700000000000", entry.Version, "the version token enables optimistic concurrency on apply")
	assert.Equal(t, []string{"LOC_A"}, entry.Locations)

	location, ok := s.Locations["LOC_A"]
	require.True(t, ok)
	assert.Equal(t, "Atlanta", location.Name)
	assert.Equal(t, "GA", location.State)
}
