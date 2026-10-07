package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"reliquary/internal/types"
)

// SkillImageFingerprint identifies the provider account a skill snapshot lives
// in. Snapshots are not visible across accounts, so when credentials change the
// stored snapshot silently stops existing for us - this fingerprint is how we
// notice and fall back instead of booting sessions against a dead image ID.
func SkillImageFingerprint(provider, apiKey, apiURL string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		strings.TrimSpace(provider),
		strings.TrimSpace(apiKey),
		strings.TrimSpace(apiURL),
	}, "\n")))
	return hex.EncodeToString(sum[:])
}

// SkillImageActive reports whether a config's stored snapshot is the image its
// sessions actually boot.
//
// It exists so the agent side can ask the same question the session side
// already answers, from the same inputs: a skill announced to the model while
// sessions keep booting the base template is a skill that cannot be invoked,
// and one hidden while the image carries it is an install nobody can use. It is
// false for backends that cannot snapshot at all (disabled), which is
// what "the base template is kept" means for them.
func SkillImageActive(tenantCfg *types.TenantSandboxConfig) bool {
	if tenantCfg == nil {
		return false
	}
	switch SandboxType(tenantCfg.SandboxType) {
	case SandboxTypeDocker:
		return DockerSkillImageOverride(tenantCfg) != ""
	}
	return false
}

// SkillOwnerFingerprint is the provider-account identity stamped onto a
// skill snapshot. The install path refuses to record a snapshot when this is
// empty: a pointer with no owner is discarded at session start.
func SkillOwnerFingerprint(tenantCfg *types.TenantSandboxConfig) string {
	if tenantCfg == nil {
		return ""
	}
	switch SandboxType(tenantCfg.SandboxType) {
	case SandboxTypeDocker:
		return dockerSkillOwnerFingerprint(tenantCfg.Docker)
	}
	return ""
}

const dockerLocalDaemonIdentity = "local-daemon"

// dockerSkillOwnerIdentity is the daemon identity a skill snapshot is stamped
// with. It reads the STORED config rather than a resolved one so that every
// caller — the install path, SkillImageActive and ResolveEffectiveConfig —
// computes the fingerprint from the same inputs.
func dockerSkillOwnerIdentity(docker *types.DockerSandboxConfig) (host, tls string) {
	if docker == nil {
		return "", ""
	}
	host = strings.TrimSpace(docker.Host)
	if host == "" {
		host = dockerLocalDaemonIdentity
	}
	return host, strings.TrimSpace(docker.TLSCertPath)
}

// DockerSkillImageOverride returns the committed skill image the docker backend
// should boot instead of the config's base image, or "" to keep the base.
func DockerSkillImageOverride(tenantCfg *types.TenantSandboxConfig) string {
	if tenantCfg == nil || tenantCfg.Docker == nil {
		return ""
	}
	host, tls := dockerSkillOwnerIdentity(tenantCfg.Docker)
	return skillImageTemplateOverride(tenantCfg.SkillImage, "docker", tls, host)
}

func dockerSkillOwnerFingerprint(docker *types.DockerSandboxConfig) string {
	if docker == nil {
		return ""
	}
	host, tls := dockerSkillOwnerIdentity(docker)
	return SkillImageFingerprint("docker", tls, host)
}

// skillImageTemplateOverride returns the snapshot ID that should replace the
// base template, or "" when the base template must be kept.
func skillImageTemplateOverride(
	image *types.SkillImageConfig, provider, apiKey, apiURL string,
) string {
	if image == nil || strings.TrimSpace(image.SnapshotID) == "" {
		return ""
	}
	if image.OwnerFingerprint == "" {
		return ""
	}
	if image.OwnerFingerprint != SkillImageFingerprint(provider, apiKey, apiURL) {
		return ""
	}
	return strings.TrimSpace(image.SnapshotID)
}
