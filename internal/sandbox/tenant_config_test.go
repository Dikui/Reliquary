package sandbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func globalTestConfig() *Config {
	cfg := &Config{Type: SandboxTypeDocker, DefaultTimeout: 60 * time.Second, DockerImage: "global-image", DockerHost: "unix:///global.sock", DockerIdleTTL: 10 * time.Minute, DockerHTTPTimeout: 30 * time.Second}
	cfg.Network = resolveNetworkPolicy(nil)
	return cfg
}

func TestResolveEffectiveConfigNilTenantKeepsGlobal(t *testing.T) {
	global := globalTestConfig()
	got, err := ResolveEffectiveConfig(nil, global)
	require.NoError(t, err)
	require.Equal(t, *global, *got)
}

func TestResolveEffectiveConfigNilTenantMaterializesNetworkDefaults(t *testing.T) {
	global := &Config{Type: SandboxTypeDisabled, DefaultTimeout: time.Second}

	got, err := ResolveEffectiveConfig(nil, global)

	require.NoError(t, err)
	require.NotNil(t, got.Network.AllowInternetAccess)
	require.True(t, *got.Network.AllowInternetAccess)
}

func TestDefaultConfigMaterializesNetworkDefaults(t *testing.T) {
	got := DefaultConfig()

	require.NotNil(t, got.Network.AllowInternetAccess)
	require.True(t, *got.Network.AllowInternetAccess)
}

func TestResolveEffectiveConfigDoesNotMutateGlobal(t *testing.T) {
	global := globalTestConfig()
	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{SandboxType: "docker", Docker: &types.DockerSandboxConfig{Image: "tenant-image", Host: "unix:///tenant.sock"}}, global)
	require.NoError(t, err)
	require.Equal(t, "unix:///global.sock", global.DockerHost)
}

func TestResolveEffectiveConfigDoesNotInheritProviderFields(t *testing.T) {
	global := globalTestConfig()
	got, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{SandboxType: "docker", Docker: &types.DockerSandboxConfig{Image: "tenant-image", Host: "unix:///tenant.sock"}}, global)
	require.NoError(t, err)
	require.Equal(t, "tenant-image", got.DockerImage)
	require.Equal(t, "unix:///tenant.sock", got.DockerHost)
}

func TestResolveEffectiveConfigDefaultsNetworkToEgressOpenInboundClosed(t *testing.T) {
	// No stored policy at all: the shipped default must still be explicit, so
	// the adapters do not have to re-derive it.
	got, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{SandboxType: "docker", Docker: &types.DockerSandboxConfig{Image: "tenant-image", Host: "unix:///tenant.sock"}}, globalTestConfig())
	require.NoError(t, err)

	require.NotNil(t, got.Network.AllowInternetAccess)
	require.True(t, *got.Network.AllowInternetAccess)
}

func TestResolveEffectiveConfigDisabled(t *testing.T) {
	tenantCfg := &types.TenantSandboxConfig{SandboxType: "disabled"}

	got, err := ResolveEffectiveConfig(tenantCfg, globalTestConfig())

	require.NoError(t, err)
	require.Equal(t, SandboxTypeDisabled, got.Type)
}

func TestResolveEffectiveConfigRejectsUnknownType(t *testing.T) {
	tenantCfg := &types.TenantSandboxConfig{SandboxType: "quantum"}

	_, err := ResolveEffectiveConfig(tenantCfg, globalTestConfig())

	require.Error(t, err)
}

func TestResolveEffectiveConfigRejectsDockerHostNetwork(t *testing.T) {
	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image:       "reliquary:test",
			NetworkMode: "host",
		},
	}, DefaultConfig())
	require.Error(t, err)
}

// A blank host must come out of config resolution already pointing at the
// daemon the Docker CLI would use, because that resolved value is what the
// connectivity check dials. Leaving it empty here is what made the check fail
// on a Colima or Docker Desktop host, where /var/run/docker.sock is absent.
func TestResolveEffectiveConfigDetectsLocalDockerHostWhenBlank(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/from-env.sock")

	effective, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker:      &types.DockerSandboxConfig{Image: "reliquary:test"},
	}, DefaultConfig())

	require.NoError(t, err)
	require.Equal(t, "unix:///tmp/from-env.sock", effective.DockerHost)
}

func TestResolveEffectiveConfigMapsDockerNoneToDeniedEgress(t *testing.T) {
	effective, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image:       "reliquary:test",
			NetworkMode: "none",
		},
	}, DefaultConfig())
	require.NoError(t, err)
	require.True(t, effective.Network.DeniesEgressByDefault(),
		"docker network_mode=none is this backend's deny-all switch")
}

func TestResolveEffectiveConfigKeepsDockerBridgeEgressOpen(t *testing.T) {
	t.Setenv("DOCKER_HOST", "unix:///tmp/from-env.sock")
	effective, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image:       "reliquary:test",
			NetworkMode: "bridge",
		},
	}, DefaultConfig())
	require.NoError(t, err)
	require.False(t, effective.Network.DeniesEgressByDefault())
}

func TestResolveEffectiveConfigRejectsPlaintextDockerTCP(t *testing.T) {
	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType:           "docker",
		AllowPrivateEndpoints: true,
		Docker: &types.DockerSandboxConfig{
			Image: "reliquary:test",
			Host:  "tcp://10.0.0.5:2376",
		},
	}, DefaultConfig())
	require.Error(t, err)
}

// The same bar has to apply to a host nobody typed. A blank field is filled in
// from DOCKER_HOST, and on a deployment pointed at a plaintext daemon that
// resolved value is what gets dialled — so validating only the stored string
// would accept a config that cannot work and say so only at the first sandbox.
func TestResolveEffectiveConfigRejectsResolvedPlaintextDockerTCP(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.5:2375")

	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType:           "docker",
		AllowPrivateEndpoints: true,
		Docker:                &types.DockerSandboxConfig{Image: "reliquary:test"},
	}, DefaultConfig())
	require.Error(t, err)
}

// A resolved host is still only a daemon endpoint, so it answers to the same
// outbound policy an explicitly typed one does.
func TestResolveEffectiveConfigRejectsResolvedPrivateDockerHostWithoutOptIn(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.5:2376")

	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image:       "reliquary:test",
			TLSCertPath: "/etc/reliquary/docker-certs",
		},
	}, DefaultConfig())
	require.ErrorIs(t, err, ErrUnsafeOutboundURL)
}

// A missing image is a field the admin can see and fix in the form, so it must
// still be reported ahead of anything about the daemon endpoint.
func TestResolveEffectiveConfigReportsMissingImageBeforeHostProblems(t *testing.T) {
	t.Setenv("DOCKER_HOST", "tcp://10.0.0.5:2375")

	_, err := ResolveEffectiveConfig(&types.TenantSandboxConfig{
		SandboxType: "docker",
	}, DefaultConfig())
	require.ErrorIs(t, err, ErrSandboxConfigIncomplete)
}

func TestResolveEffectiveConfigUsesSkillSnapshotAsDockerImage(t *testing.T) {
	global := DefaultConfig()
	base := &types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image: "reliquary/sandbox:base",
			Host:  "unix:///var/run/docker.sock",
		},
	}
	fp := SkillImageFingerprint("docker", "", "unix:///var/run/docker.sock")

	t.Run("usable snapshot overrides the base image", func(t *testing.T) {
		cfg := *base
		cfg.Docker = &types.DockerSandboxConfig{
			Image: "reliquary/sandbox:base", Host: "unix:///var/run/docker.sock",
		}
		cfg.SkillImage = &types.SkillImageConfig{
			SnapshotID: "reliquary-skill/reliquary-sk-cfg1-g1", OwnerFingerprint: fp,
		}

		eff, err := ResolveEffectiveConfig(&cfg, global)

		require.NoError(t, err)
		require.Equal(t, "reliquary-skill/reliquary-sk-cfg1-g1", eff.DockerImage)
	})

	t.Run("fingerprint mismatch falls back to the base image", func(t *testing.T) {
		cfg := *base
		cfg.Docker = &types.DockerSandboxConfig{
			Image: "reliquary/sandbox:base", Host: "unix:///var/run/docker.sock",
		}
		cfg.SkillImage = &types.SkillImageConfig{
			SnapshotID: "reliquary-skill/reliquary-sk-cfg1-g1", OwnerFingerprint: "another-daemon",
		}

		eff, err := ResolveEffectiveConfig(&cfg, global)

		require.NoError(t, err)
		require.Equal(t, "reliquary/sandbox:base", eff.DockerImage,
			"a snapshot from another daemon is invisible; the session must still boot")
	})

	// A blank host follows the environment, so binding the fingerprint to the
	// resolved daemon would make "switch docker context" indistinguishable
	// from "rotate the credentials": sessions would silently lose every
	// skill, installs would be refused, and the snapshot prune would skip the
	// config forever. The images are on the same disk either way.
	t.Run("blank host survives a change of local daemon", func(t *testing.T) {
		newConfig := func() *types.TenantSandboxConfig {
			return &types.TenantSandboxConfig{
				SandboxType: "docker",
				Docker:      &types.DockerSandboxConfig{Image: "reliquary/sandbox:base"},
				SkillImage: &types.SkillImageConfig{
					SnapshotID: "reliquary-skill/reliquary-sk-cfg1-g1",
					OwnerFingerprint: SkillOwnerFingerprint(&types.TenantSandboxConfig{
						SandboxType: "docker",
						Docker:      &types.DockerSandboxConfig{Image: "reliquary/sandbox:base"},
					}),
				},
			}
		}

		for _, host := range []string{
			"unix:///tmp/colima.sock",
			"unix:///tmp/orbstack.sock",
		} {
			t.Setenv("DOCKER_HOST", host)
			cfg := newConfig()

			eff, err := ResolveEffectiveConfig(cfg, global)

			require.NoError(t, err)
			require.Equal(t, "reliquary-skill/reliquary-sk-cfg1-g1", eff.DockerImage,
				"DOCKER_HOST=%s must not retire the config's skill image", host)
			require.True(t, SkillImageActive(cfg))
		}
	})

	// An explicit host is still part of the identity: only an admin editing
	// the config can change it, and it may well be another machine.
	t.Run("an explicit host change retires the image", func(t *testing.T) {
		cfg := *base
		cfg.Docker = &types.DockerSandboxConfig{
			Image: "reliquary/sandbox:base", Host: "tcp://198.51.100.10:2376",
			TLSCertPath: "/certs",
		}
		cfg.SkillImage = &types.SkillImageConfig{
			SnapshotID: "reliquary-skill/reliquary-sk-cfg1-g1", OwnerFingerprint: fp,
		}

		eff, err := ResolveEffectiveConfig(&cfg, global)

		require.NoError(t, err)
		require.Equal(t, "reliquary/sandbox:base", eff.DockerImage)
	})
}

func TestSkillOwnerFingerprintForDocker(t *testing.T) {
	require.Empty(t, SkillOwnerFingerprint(&types.TenantSandboxConfig{SandboxType: "docker"}),
		"a docker type with no daemon block cannot own a snapshot")
	require.Empty(t, SkillOwnerFingerprint(&types.TenantSandboxConfig{SandboxType: "disabled"}))

	got := SkillOwnerFingerprint(&types.TenantSandboxConfig{
		SandboxType: "docker",
		Docker: &types.DockerSandboxConfig{
			Image: "img", Host: "unix:///var/run/docker.sock", TLSCertPath: "/certs",
		},
	})
	require.Equal(t, SkillImageFingerprint("docker", "/certs", "unix:///var/run/docker.sock"), got)
}

func TestSkillImageFingerprintIsStableAndDiscriminating(t *testing.T) {
	a := SkillImageFingerprint("other", "key-1", "https://a.example.com")
	require.Equal(t, a, SkillImageFingerprint("other", "key-1", "https://a.example.com"))
	require.NotEqual(t, a, SkillImageFingerprint("other", "key-2", "https://a.example.com"))
	require.NotEqual(t, a, SkillImageFingerprint("docker", "key-1", "https://a.example.com"))
}
