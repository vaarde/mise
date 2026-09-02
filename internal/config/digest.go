package config

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// Digest fingerprints a set of resource declarations.
//
// A saved plan records the digest of the config it was computed from, so
// "mise apply --plan" can tell the operator when the YAML has moved on
// since the plan was reviewed. The plan still carries the full desired
// state and would apply exactly what was previewed — which is the point:
// what was previewed is no longer what the files say.
//
// The digest covers what a plan depends on — each resource's name, scope
// and properties — and nothing else. Which file a resource lives in, and
// the order the files were walked in, deliberately do not change it.
func Digest(resources []ResourceDef) (string, error) {
	entries := make([]map[string]interface{}, 0, len(resources))
	for _, res := range resources {
		entries = append(entries, map[string]interface{}{
			"name":       res.FullName(),
			"locations":  res.Locations,
			"properties": res.Properties,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i]["name"].(string) < entries[j]["name"].(string)
	})

	// encoding/json sorts map keys, so the same declarations always
	// produce the same bytes regardless of YAML ordering.
	data, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("cannot fingerprint the configuration: %w", err)
	}

	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
