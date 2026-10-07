package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// The zero value must mean "egress open, inbound closed", which is what lets a
// config with no network block behave identically to one saved with defaults.
func TestSandboxNetworkPolicyZeroValueIsTheDefault(t *testing.T) {
	var p SandboxNetworkPolicy

	require.False(t, p.DenyEgressByDefault, "egress must default to allowed")

	encoded, err := json.Marshal(&p)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded),
		"a default policy must serialize empty so it is indistinguishable from nil")
}
