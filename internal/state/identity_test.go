package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdentityRejectsADifferentAccount(t *testing.T) {
	// Provider IDs only exist within the account that issued them. Point
	// a workspace at a second merchant and every recorded ID misses, so
	// plan reads each resource as deleted and proposes to create it —
	// duplicating the whole configuration into the wrong account.
	recorded := Identity{Provider: "square", Environment: "production", AccountID: "MERCHANT_A"}
	current := Identity{Provider: "square", Environment: "production", AccountID: "MERCHANT_B"}

	err := recorded.Check(current)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MERCHANT_A")
	assert.Contains(t, err.Error(), "MERCHANT_B")
	assert.Contains(t, err.Error(), "re-create every resource")
}

func TestIdentityRejectsADifferentEnvironment(t *testing.T) {
	recorded := Identity{Provider: "square", Environment: "sandbox"}
	current := Identity{Provider: "square", Environment: "production"}

	err := recorded.Check(current)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sandbox")
	assert.Contains(t, err.Error(), "production")
}

func TestIdentityTreatsEmptyEnvironmentAsProduction(t *testing.T) {
	// mise.yaml may omit the environment, and that means production. A
	// workspace that spells it out must not read as a different account
	// from one that leaves it to the default.
	recorded := Identity{Provider: "square", Environment: ""}
	current := Identity{Provider: "square", Environment: "production"}

	assert.NoError(t, recorded.Check(current))
	assert.NoError(t, current.Check(recorded))
}

func TestIdentitySkipsWhatItCannotKnow(t *testing.T) {
	// An adapter without provider.AccountIdentifier leaves the account
	// empty. The check tightens as more is known rather than refusing to
	// run on incomplete information.
	recorded := Identity{Provider: "toast", Environment: "production"}
	current := Identity{Provider: "toast", Environment: "production", AccountID: "GUID_1"}

	assert.NoError(t, recorded.Check(current))
}

func TestZeroIdentityChecksNothing(t *testing.T) {
	// State written by an older Mise records no identity at all.
	var recorded Identity
	assert.True(t, recorded.IsZero())
	assert.NoError(t, recorded.Check(Identity{Provider: "square", AccountID: "MERCHANT_A"}))
}

func TestIdentityAcceptsTheSameAccount(t *testing.T) {
	id := Identity{Provider: "square", Environment: "sandbox", AccountID: "MERCHANT_A"}
	assert.NoError(t, id.Check(id))
}

func TestIdentityStringNamesWhatItKnows(t *testing.T) {
	id := Identity{Provider: "square", Environment: "sandbox", AccountID: "MERCHANT_A"}
	assert.Equal(t, "square sandbox account MERCHANT_A", id.String())

	assert.Equal(t, "an unrecorded account", Identity{}.String())
}
