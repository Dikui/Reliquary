package types

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSandboxConfigForResponseMasksSecrets(t *testing.T) {
	cfg := &TenantSandboxConfig{
		SandboxType: "docker",
		EnvVars:     map[string]string{"HF_TOKEN": "hf-secret"},
	}

	out := SandboxConfigForResponse(cfg, true)

	require.Equal(t, RedactedSecretPlaceholder, out.EnvVars["HF_TOKEN"])
	// Non-secret fields stay visible.
	require.Equal(t, "docker", out.SandboxType)
	// Original must be untouched.
	require.Equal(t, "hf-secret", cfg.EnvVars["HF_TOKEN"])
}

func TestSandboxConfigForResponseSkipsMaskingWhenDisabled(t *testing.T) {
	cfg := &TenantSandboxConfig{EnvVars: map[string]string{"TOKEN": "secret"}}

	out := SandboxConfigForResponse(cfg, false)
	require.Equal(t, "secret", out.EnvVars["TOKEN"])

}

func TestSandboxConfigForResponseEmptySecretStaysEmpty(t *testing.T) {
	cfg := &TenantSandboxConfig{EnvVars: map[string]string{"TOKEN": ""}}
	out := SandboxConfigForResponse(cfg, true)
	require.Empty(t, out.EnvVars["TOKEN"])
}

func TestSandboxConfigForResponseNil(t *testing.T) {
	require.Nil(t, SandboxConfigForResponse(nil, true))
}

func TestMergeSandboxConfigForUpdatePreservesRedactedSecrets(t *testing.T) {
	existing := &TenantSandboxConfig{
		EnvVars: map[string]string{"HF_TOKEN": "old-hf", "GONE": "old-gone"},
	}
	incoming := &TenantSandboxConfig{
		EnvVars: map[string]string{"HF_TOKEN": RedactedSecretPlaceholder},
	}

	out := MergeSandboxConfigForUpdate(incoming, existing)

	require.Equal(t, "old-hf", out.EnvVars["HF_TOKEN"])
	require.NotContains(t, out.EnvVars, "GONE", "env vars removed by the user must not resurrect")
}

func TestMergeSandboxConfigForUpdateDropsNetworkWhenIncomingOmitsIt(t *testing.T) {
	existing := &TenantSandboxConfig{
		Network: &SandboxNetworkPolicy{DenyEgressByDefault: true},
	}

	out := MergeSandboxConfigForUpdate(&TenantSandboxConfig{}, existing)

	require.Nil(t, out.Network,
		"network is an editor-owned field: omitting it must clear it, unlike SkillImage")
}

func TestMergeSandboxConfigForUpdateHandlesNilExisting(t *testing.T) {
	incoming := &TenantSandboxConfig{EnvVars: map[string]string{"TOKEN": RedactedSecretPlaceholder}}

	out := MergeSandboxConfigForUpdate(incoming, nil)
	require.Empty(t, out.EnvVars["TOKEN"])
}

func TestMergeSandboxConfigForUpdateNilIncoming(t *testing.T) {
	require.Nil(t, MergeSandboxConfigForUpdate(nil, &TenantSandboxConfig{}))
}

func TestMergeSandboxConfigForUpdatePreservesSkillImage(t *testing.T) {
	existing := &TenantSandboxConfig{
		SkillImage: &SkillImageConfig{SnapshotID: "snap-1", OwnerFingerprint: "fp-1", Generation: 3},
	}
	incoming := &TenantSandboxConfig{
		SkillImage: &SkillImageConfig{SnapshotID: "forged-snap", OwnerFingerprint: "forged-fp"},
	}

	out := MergeSandboxConfigForUpdate(incoming, existing)

	require.Equal(t, "snap-1", out.SkillImage.SnapshotID,
		"a settings save must not replace the install-owned snapshot pointer")
	require.Equal(t, "fp-1", out.SkillImage.OwnerFingerprint)
	require.Equal(t, 3, out.SkillImage.Generation)
	out.SkillImage.SnapshotID = "mutated"
	require.Equal(t, "snap-1", existing.SkillImage.SnapshotID,
		"merge must copy SkillImage so later mutation cannot touch the stored row")
}

func TestMergeSandboxConfigForUpdateIgnoresIncomingSkillImageOnCreate(t *testing.T) {
	incoming := &TenantSandboxConfig{
		SkillImage: &SkillImageConfig{SnapshotID: "forged-snap"},
	}

	out := MergeSandboxConfigForUpdate(incoming, nil)

	require.Nil(t, out.SkillImage, "create must not accept a client-supplied skill image")
}

// The editor never sends skill_image: that pointer is written only by an
// install or a removal. A merge that copied the incoming payload as-is would
// therefore wipe a live snapshot on every "保存运行配置", leaving the skill
// rows in place while every session fell back to the base template.
func TestMergeSandboxConfigForUpdateKeepsSkillImageWhenEditorOmitsIt(t *testing.T) {
	existing := &TenantSandboxConfig{
		SandboxType: "docker",
		SkillImage: &SkillImageConfig{
			SnapshotID:       "snap-2",
			Generation:       2,
			BaseTemplateID:   "base-template",
			OwnerFingerprint: "fp-1",
		},
	}
	incoming := &TenantSandboxConfig{
		SandboxType: "docker",
	}

	out := MergeSandboxConfigForUpdate(incoming, existing)

	require.NotNil(t, out.SkillImage)
	require.Equal(t, "snap-2", out.SkillImage.SnapshotID)
	require.Equal(t, 2, out.SkillImage.Generation)
	require.Equal(t, "base-template", out.SkillImage.BaseTemplateID)
	require.Equal(t, "fp-1", out.SkillImage.OwnerFingerprint)

	out.SkillImage.SnapshotID = "mutated"
	require.Equal(t, "snap-2", existing.SkillImage.SnapshotID,
		"merge must copy the stored image, not share the pointer")
}

func TestMergeSandboxConfigForUpdateKeepsSkillRolloutWhenEditorOmitsIt(t *testing.T) {
	existing := &TenantSandboxConfig{
		SandboxType:  "docker",
		SkillRollout: SkillRolloutNewSession,
	}
	incoming := &TenantSandboxConfig{
		SandboxType: "docker",
	}

	out := MergeSandboxConfigForUpdate(incoming, existing)

	require.Equal(t, SkillRolloutNewSession, out.SkillRollout,
		"a runtime save must not reset the skills-panel rollout choice")
}

func TestMergeSandboxConfigForUpdateHonoursExplicitSkillRollout(t *testing.T) {
	existing := &TenantSandboxConfig{SkillRollout: SkillRolloutNewSession}
	incoming := &TenantSandboxConfig{SkillRollout: SkillRolloutNextTurn}

	out := MergeSandboxConfigForUpdate(incoming, existing)

	require.Equal(t, SkillRolloutNextTurn, out.SkillRollout)
}

func TestMergeSandboxConfigForUpdateDoesNotMutateInputs(t *testing.T) {
	existing := &TenantSandboxConfig{EnvVars: map[string]string{"HF_TOKEN": "secret"}}
	incoming := &TenantSandboxConfig{EnvVars: map[string]string{"HF_TOKEN": RedactedSecretPlaceholder}}
	_ = MergeSandboxConfigForUpdate(incoming, existing)
	require.Equal(t, RedactedSecretPlaceholder, incoming.EnvVars["HF_TOKEN"])
	require.Equal(t, "secret", existing.EnvVars["HF_TOKEN"])
}
