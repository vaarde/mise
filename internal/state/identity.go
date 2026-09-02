package state

import (
	"fmt"
	"strings"
)

// Identity names the POS account a workspace's state describes.
//
// It exists because provider IDs are only meaningful within one account.
// Point a workspace at a second merchant and every ID recorded in state
// misses on the new account, so plan reads each resource as "deleted
// outside Mise" and proposes to create it — duplicating the entire
// configuration into the wrong place, with no error anywhere along the
// way. Recording the account and checking it is what makes that loud.
type Identity struct {
	// Provider is the platform name ("square").
	Provider string `json:"provider,omitempty"`

	// Environment is "production" or "sandbox". An empty value means
	// production, matching mise.yaml's default.
	Environment string `json:"environment,omitempty"`

	// AccountID identifies the account within the platform — Square's
	// merchant ID. It is empty when the adapter cannot name the account,
	// in which case only the platform and environment are compared.
	AccountID string `json:"account_id,omitempty"`
}

// String renders an identity for an error message.
func (i Identity) String() string {
	if i.IsZero() {
		return "an unrecorded account"
	}

	parts := []string{}
	if i.Provider != "" {
		parts = append(parts, i.Provider)
	}
	parts = append(parts, i.environment())
	if i.AccountID != "" {
		parts = append(parts, "account "+i.AccountID)
	}
	return strings.Join(parts, " ")
}

// environment normalizes the empty value to production, so a workspace
// that omits the field does not read as a different environment from one
// that spells it out.
func (i Identity) environment() string {
	if i.Environment == "" {
		return "production"
	}
	return i.Environment
}

// IsZero reports whether an identity records nothing at all. State
// written by an older Mise has no identity, so there is nothing to
// check against and the run is allowed to proceed.
func (i Identity) IsZero() bool {
	return i.Provider == "" && i.Environment == "" && i.AccountID == ""
}

// Check compares the identity recorded in state against the account the
// current workspace actually reaches, and explains any mismatch.
//
// Unknown fields never cause a mismatch: an adapter without
// provider.AccountIdentifier leaves AccountID empty on both sides, and
// state from an older Mise records nothing. The check tightens as more
// is known rather than refusing to run on incomplete information.
func (i Identity) Check(current Identity) error {
	if i.IsZero() {
		return nil
	}

	var reasons []string
	if i.Provider != "" && current.Provider != "" && i.Provider != current.Provider {
		reasons = append(reasons, fmt.Sprintf("provider %s vs %s", i.Provider, current.Provider))
	}
	if i.environment() != current.environment() {
		reasons = append(reasons, fmt.Sprintf("environment %s vs %s", i.environment(), current.environment()))
	}
	if i.AccountID != "" && current.AccountID != "" && i.AccountID != current.AccountID {
		reasons = append(reasons, fmt.Sprintf("account %s vs %s", i.AccountID, current.AccountID))
	}

	if len(reasons) == 0 {
		return nil
	}

	return fmt.Errorf(
		"this workspace was built against %s, but the current credentials reach %s (%s).\n"+
			"Applying it here would re-create every resource, because the recorded IDs do not exist on this account.\n"+
			"Check the environment in mise.yaml and which token is set, or start a separate workspace for this account",
		i, current, strings.Join(reasons, ", "))
}
