package engine

import (
	"time"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/state"
)

// ToResourceDefs converts fetched resources into config file definitions.
//
// A resource present at every location is written as ${group.all} rather
// than an explicit list — that is the form an operator wants to edit, and
// it keeps the file stable when a new location is added to the account.
func (r *FetchResult) ToResourceDefs() []config.ResourceDef {
	allLocationIDs := make(map[string]bool, len(r.Locations))
	for _, l := range r.Locations {
		allLocationIDs[l.ID] = true
	}

	defs := make([]config.ResourceDef, 0, len(r.Resources))
	for _, res := range r.Resources {
		defs = append(defs, config.ResourceDef{
			Type:       res.Type,
			Name:       res.Name,
			Locations:  locationsField(res.LocationIDs, allLocationIDs),
			Properties: res.Properties,
		})
	}
	return defs
}

// locationsField picks between the group shorthand and an explicit list.
func locationsField(locationIDs []string, allLocationIDs map[string]bool) interface{} {
	if len(allLocationIDs) == 0 || len(locationIDs) == len(allLocationIDs) {
		return config.GroupAll
	}

	// Preserve the sorted order assigned during the fetch.
	explicit := make([]string, len(locationIDs))
	copy(explicit, locationIDs)
	return explicit
}

// ToState builds the state file that records what Mise knows about the
// live POS: which config name maps to which provider ID, and what the
// properties looked like at fetch time.
//
// The state file is a cache, not a source of truth — it is what makes
// drift detection possible, by giving Mise a "last known good" to compare
// the live API against.
func (r *FetchResult) ToState(providerName string, fetchedAt time.Time) *state.State {
	s := state.New(providerName)
	s.LastFetch = &fetchedAt

	for _, res := range r.Resources {
		s.Resources[state.ResourceKey(res.Type, res.Name)] = &state.ResourceState{
			ProviderID: res.ProviderID,
			Type:       res.Type,
			Name:       res.Name,
			Locations:  res.LocationIDs,
			Properties: res.Properties,
			Version:    res.Version,
			LastSynced: fetchedAt,
		}
	}

	for _, l := range r.Locations {
		s.Locations[l.ID] = &state.LocationState{
			Name:     l.Name,
			State:    l.State,
			Timezone: l.Timezone,
		}
	}

	return s
}
