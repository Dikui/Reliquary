package sandbox

import (
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

// Identity comes from the stored row alone. Nothing about the deployment can
// change it, which is what makes "did this edit strand a sandbox" answerable
// without reaching for process-level sandbox configuration.
func TestIdentityOfReadsStoredFieldsOnly(t *testing.T) {
	identity := IdentityOf(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Host: "unix:///daemon.sock", TLSCertPath: "certs",
		},
	})

	require.Equal(t, SandboxIdentity{
		Provider: "docker",
		APIURL:   "unix:///daemon.sock",
		APIKey:   "certs",
	}, identity)
}

func TestIdentityOfDoesNotInherit(t *testing.T) {
	blank := IdentityOf(&types.TenantSandboxConfig{SandboxType: "docker"})
	explicit := IdentityOf(&types.TenantSandboxConfig{SandboxType: "docker", Docker: &types.DockerSandboxConfig{Host: "unix:///daemon.sock"}})
	require.Equal(t, SandboxIdentity{Provider: "docker"}, blank)
	require.NotEqual(t, blank, explicit)
	require.Equal(t, "unix:///daemon.sock", explicit.APIURL)
}

// Backends that hold no remote resources carry no credentials, but switching
// between them still has to register as a change.
func TestIdentityOfDockerWithoutHostCarriesProviderOnly(t *testing.T) {
	docker := IdentityOf(&types.TenantSandboxConfig{SandboxType: "docker"})
	disabled := IdentityOf(&types.TenantSandboxConfig{SandboxType: "disabled"})

	require.Equal(t, SandboxIdentity{Provider: "docker"}, docker)
	require.NotEqual(t, docker, disabled)
}

// A nil config must not panic: Update compares against it when creating.
func TestIdentityOfToleratesNilConfig(t *testing.T) {
	require.Equal(t, SandboxIdentity{}, IdentityOf(nil))
}
