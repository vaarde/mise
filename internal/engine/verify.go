package engine

import (
	"context"
	"fmt"
	"sort"

	"github.com/vaarde/mise/internal/config"
	"github.com/vaarde/mise/internal/provider"
	"github.com/vaarde/mise/internal/state"
)

// VerifyIssue explains why one managed resource is not converged at a location.
type VerifyIssue struct {
	Resource string         `json:"resource"`
	Message  string         `json:"message"`
	Diffs    []PropertyDiff `json:"diffs,omitempty"`
}

// VerifyLocationResult is the convergence result for one location.
type VerifyLocationResult struct {
	LocationID   string        `json:"location_id"`
	LocationName string        `json:"location_name,omitempty"`
	Converged    bool          `json:"converged"`
	Issues       []VerifyIssue `json:"issues"`
}

// VerifyEvent is emitted as verification advances through the target estate.
type VerifyEvent struct {
	Type         string                `json:"type"`
	Verified     int                   `json:"verified"`
	Total        int                   `json:"total"`
	Converged    int                   `json:"converged,omitempty"`
	NonConverged int                   `json:"non_converged,omitempty"`
	Location     *VerifyLocationResult `json:"location,omitempty"`
}

// VerifyResult is the final convergence summary.
type VerifyResult struct {
	Verified     int                    `json:"verified"`
	Total        int                    `json:"total"`
	Converged    int                    `json:"converged"`
	NonConverged int                    `json:"non_converged"`
	Locations    []VerifyLocationResult `json:"locations"`
}

// Verify compares the live provider state against the exact desired values
// carried by a saved plan. It is read-only and does not rewrite Mise state.
//
// Verification is deliberately location-oriented even when a provider can
// batch account-wide writes. Square's ReadAll cache means this can report
// truthful per-location progress without repeatedly downloading its catalog.
func Verify(
	ctx context.Context,
	p provider.Provider,
	plan *PlanResult,
	st *state.State,
	emit func(VerifyEvent) error,
) (*VerifyResult, error) {
	if plan == nil {
		return nil, fmt.Errorf("verify requires a saved plan")
	}
	if st == nil {
		return nil, fmt.Errorf("verify requires Mise state")
	}

	locationNames := make(map[string]string, len(plan.Locations))
	for _, location := range plan.Locations {
		locationNames[location.ID] = location.Name
	}

	locationIDs := verifyLocationIDs(plan.Changes)
	result := &VerifyResult{
		Total:     len(locationIDs),
		Locations: make([]VerifyLocationResult, 0, len(locationIDs)),
	}

	resolvedDesired := make(map[string]map[string]interface{}, len(plan.Changes))
	unresolvedByResource := make(map[string][]string)
	for _, change := range plan.Changes {
		resolved, unresolved := config.ResolveRefs(change.Desired, stateLookup(st))
		properties, _ := resolved.(map[string]interface{})
		if properties == nil {
			properties = map[string]interface{}{}
		}
		resolvedDesired[change.FullName()] = properties
		if len(unresolved) > 0 {
			sort.Strings(unresolved)
			unresolvedByResource[change.FullName()] = unresolved
		}
	}

	for index, locationID := range locationIDs {
		locationResult, err := verifyLocation(ctx, p, plan.Changes, st, locationID,
			locationNames[locationID], resolvedDesired, unresolvedByResource)
		if err != nil {
			return nil, err
		}

		result.Locations = append(result.Locations, locationResult)
		result.Verified = index + 1
		if locationResult.Converged {
			result.Converged++
		} else {
			result.NonConverged++
		}

		if emit != nil {
			event := VerifyEvent{
				Type:     "verify_progress",
				Verified: result.Verified,
				Total:    result.Total,
				Location: &locationResult,
			}
			if err := emit(event); err != nil {
				return nil, err
			}
		}
	}

	if emit != nil {
		if err := emit(VerifyEvent{
			Type:         "verify_complete",
			Verified:     result.Verified,
			Total:        result.Total,
			Converged:    result.Converged,
			NonConverged: result.NonConverged,
		}); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func verifyLocation(
	ctx context.Context,
	p provider.Provider,
	changes []ResourceChange,
	st *state.State,
	locationID string,
	locationName string,
	resolvedDesired map[string]map[string]interface{},
	unresolvedByResource map[string][]string,
) (VerifyLocationResult, error) {
	result := VerifyLocationResult{
		LocationID:   locationID,
		LocationName: locationName,
		Converged:    true,
		Issues:       []VerifyIssue{},
	}

	changesForLocation := make([]ResourceChange, 0)
	types := map[string]bool{}
	for _, change := range changes {
		if containsString(change.LocationIDs, locationID) {
			changesForLocation = append(changesForLocation, change)
			types[change.ResourceType] = true
		}
	}

	liveByType := make(map[string]map[string]*provider.Resource, len(types))
	for resourceType := range types {
		resources, err := p.ReadAll(ctx, resourceType, locationID)
		if err != nil {
			return result, fmt.Errorf("verify %s at location %s: %w", resourceType, locationID, err)
		}
		byID := make(map[string]*provider.Resource, len(resources))
		for _, resource := range resources {
			byID[resource.ProviderID] = resource
		}
		liveByType[resourceType] = byID
	}

	for _, change := range changesForLocation {
		fullName := change.FullName()
		if unresolved := unresolvedByResource[fullName]; len(unresolved) > 0 {
			result.Converged = false
			result.Issues = append(result.Issues, VerifyIssue{
				Resource: fullName,
				Message:  fmt.Sprintf("cannot resolve %s", config.FormatRef(unresolved[0])),
			})
			continue
		}

		providerID := change.ProviderID
		if entry, ok := st.Resources[fullName]; ok && entry.ProviderID != "" {
			providerID = entry.ProviderID
		}
		if providerID == "" {
			result.Converged = false
			result.Issues = append(result.Issues, VerifyIssue{
				Resource: fullName,
				Message:  "resource has no provider ID in state",
			})
			continue
		}

		live := liveByType[change.ResourceType][providerID]
		if live == nil {
			result.Converged = false
			result.Issues = append(result.Issues, VerifyIssue{
				Resource: fullName,
				Message:  "resource is not present at this location",
			})
			continue
		}

		diffs := diffProperties(normalizeLiveProperties(live.Properties), resolvedDesired[fullName])
		if len(diffs) > 0 {
			result.Converged = false
			result.Issues = append(result.Issues, VerifyIssue{
				Resource: fullName,
				Message:  "live properties do not match the approved plan",
				Diffs:    diffs,
			})
		}
	}

	return result, nil
}

func verifyLocationIDs(changes []ResourceChange) []string {
	seen := map[string]bool{}
	for _, change := range changes {
		for _, locationID := range change.LocationIDs {
			seen[locationID] = true
		}
	}
	ids := make([]string, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
