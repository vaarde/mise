package engine

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vaarde/mise/internal/config"
)

// Wave is a set of changes with no dependencies on each other, so they
// can all be applied in one batch and in any order.
type Wave []ResourceChange

// OrderChanges groups a plan's changes into dependency waves.
//
// A menu item that charges a tax cannot be created before that tax
// exists, because the item's request has to name the tax's provider ID.
// Wave 0 holds everything that depends on nothing new; wave 1 holds
// everything whose dependencies are satisfied by wave 0, and so on.
//
// Only dependencies on resources *within this plan* constrain the order.
// A reference to something that already exists on the POS is satisfied
// before apply starts, so it never delays anything.
func OrderChanges(changes []ResourceChange, declaredProperties map[string]map[string]interface{}) ([]Wave, error) {
	pending := make(map[string]ResourceChange, len(changes))
	for _, change := range changes {
		// The map is keyed by name, so a repeated resource would
		// overwrite its twin and one of the two would never be applied —
		// silently, and with the winner decided by file walk order.
		// Config loading rejects duplicates; this catches the paths that
		// reach the graph another way, such as an edited saved plan.
		if _, dup := pending[change.FullName()]; dup {
			return nil, fmt.Errorf("%s appears twice in the same plan — "+
				"Mise identifies a resource by its type and name, so one of the two would be dropped",
				change.FullName())
		}
		pending[change.FullName()] = change
	}

	// Edges point from a resource to the in-plan resources it needs first.
	dependencies := make(map[string][]string, len(changes))
	for name := range pending {
		properties := declaredProperties[name]
		if properties == nil {
			properties = pending[name].Desired
		}

		var needed []string
		for _, dep := range config.Dependencies(properties) {
			// A dependency already satisfied on the POS is not a
			// constraint on ordering.
			if _, inPlan := pending[dep]; inPlan && dep != name {
				needed = append(needed, dep)
			}
		}
		sort.Strings(needed)
		dependencies[name] = needed
	}

	var waves []Wave
	done := make(map[string]bool, len(pending))

	for len(done) < len(pending) {
		var ready []string
		for name := range pending {
			if done[name] {
				continue
			}
			if allSatisfied(dependencies[name], done) {
				ready = append(ready, name)
			}
		}

		if len(ready) == 0 {
			return nil, cycleError(pending, dependencies, done)
		}

		// Sorting keeps apply order deterministic, which matters when
		// reading a log of what happened.
		sort.Strings(ready)

		wave := make(Wave, 0, len(ready))
		for _, name := range ready {
			wave = append(wave, pending[name])
			done[name] = true
		}
		waves = append(waves, wave)
	}

	return waves, nil
}

// allSatisfied reports whether every dependency has been applied.
func allSatisfied(needed []string, done map[string]bool) bool {
	for _, dep := range needed {
		if !done[dep] {
			return false
		}
	}
	return true
}

// cycleError explains which resources reference each other in a loop.
func cycleError(
	pending map[string]ResourceChange,
	dependencies map[string][]string,
	done map[string]bool,
) error {
	var stuck []string
	for name := range pending {
		if !done[name] {
			stuck = append(stuck, name)
		}
	}
	sort.Strings(stuck)

	details := make([]string, 0, len(stuck))
	for _, name := range stuck {
		var blockers []string
		for _, dep := range dependencies[name] {
			if !done[dep] {
				blockers = append(blockers, dep)
			}
		}
		details = append(details, fmt.Sprintf("%s needs %s", name, strings.Join(blockers, ", ")))
	}

	return fmt.Errorf("configuration has a circular reference, so there is no order that works:\n  %s",
		strings.Join(details, "\n  "))
}
