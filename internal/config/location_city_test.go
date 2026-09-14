package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/vaarde/mise/internal/provider"
)

func TestGenerateLocationsFilePersistsCityMetadata(t *testing.T) {
	dir := t.TempDir()
	locations := []provider.Location{
		{
			ID:       "LOC_ATL",
			Name:     "Mise Test - Atlanta",
			State:    "GA",
			Address:  "191 Peachtree St NE, Atlanta, GA",
			Timezone: "America/New_York",
			Metadata: map[string]string{"status": "ACTIVE", "city": "Atlanta"},
		},
	}

	_, err := GenerateLocationsFile(dir, locations)
	require.NoError(t, err)

	raw, err := os.ReadFile(filepath.Join(dir, LocationsFileName))
	require.NoError(t, err)

	var parsed struct {
		Locations []struct {
			City string `yaml:"city"`
		} `yaml:"locations"`
	}
	require.NoError(t, yaml.Unmarshal(raw, &parsed))
	require.Len(t, parsed.Locations, 1)
	assert.Equal(t, "Atlanta", parsed.Locations[0].City)
}
