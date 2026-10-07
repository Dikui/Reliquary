package handler

import (
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/sandbox"
	"reliquary/internal/types"
)

func TestSandboxCheckReasonDockerUnavailableIncludesHost(t *testing.T) {
	msg := sandboxCheckReason(&sandbox.RemoteError{
		Kind:     sandbox.RemoteErrorKindUnavailable,
		Provider: sandbox.SandboxTypeDocker,
		Message:  "Cannot connect to the Docker daemon at unix:///var/run/docker.sock. Is the docker daemon running?",
	})
	require.Contains(t, msg, "unix:///var/run/docker.sock")
	require.Contains(t, msg, "docker context")
}

func TestRunStatelessSandboxCheckRemovedWithLocalBackend(t *testing.T) {
	incoming := &types.TenantSandboxConfig{SandboxType: "local"}

	_, err := sandbox.ResolveEffectiveConfig(incoming, sandbox.DefaultConfig())

	require.ErrorIs(t, err, sandbox.ErrUnsupportedSandboxType)
}

// The probe has to run under the policy the admin configured, or
// egress_available answers a question nobody asked: whether the provider's
// default allows egress.
func TestDeepSandboxCheckUsesConfiguredNetworkPolicy(t *testing.T) {
	incoming := &types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker:      &types.DockerSandboxConfig{Image: "image", Host: "unix:///daemon.sock", NetworkMode: "none"},
		Network: &types.SandboxNetworkPolicy{
			DenyEgressByDefault: true,
		},
	}

	effective, err := sandbox.ResolveEffectiveConfig(incoming, sandbox.DefaultConfig())
	require.NoError(t, err)

	require.NotNil(t, effective.Network.AllowInternetAccess)
	require.False(t, *effective.Network.AllowInternetAccess)
}

// Under a deny-by-default policy a blocked probe is the policy working, not a
// misconfiguration, so it must not be reported as a failure.
func TestDeepSandboxCheckReportsEgressRestrictedRatherThanFailed(t *testing.T) {
	result := &SandboxCheckResponse{OK: true, Provider: "docker"}
	denied := false

	reportEgressProbe(result, sandbox.RemoteNetworkPolicy{
		AllowInternetAccess: &denied,
	}, false, "curl: (28) timeout", 0)

	require.True(t, result.OK, "a policy-blocked probe must not fail the check")
	item := result.Checks[len(result.Checks)-1]
	require.Equal(t, "egress_available", item.Name)
	require.Nil(t, item.OK)
	require.Equal(t, skipReasonEgressRestrictedByPolicy, item.Reason)
	require.Empty(t, item.Message, "skip reasons are localized by the UI")
}

func TestDeepSandboxCheckStillFailsEgressWhenPolicyAllowsAll(t *testing.T) {
	result := &SandboxCheckResponse{OK: true, Provider: "docker"}
	allowed := true

	reportEgressProbe(result, sandbox.RemoteNetworkPolicy{
		AllowInternetAccess: &allowed,
	}, false, "curl: (28) timeout", 120)

	require.False(t, result.OK)
	item := result.Checks[len(result.Checks)-1]
	require.NotNil(t, item.OK)
	require.False(t, *item.OK)
}
