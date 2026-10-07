package sandbox

import (
	"reliquary/internal/types"
)

// SandboxIdentity is the comparable projection of a config: two configs with
// equal identities can operate each other's sandboxes.
type SandboxIdentity struct {
	Provider              string
	AllowPrivateEndpoints bool

	// Control plane - whether an existing sandbox can still be reclaimed.
	APIURL string
	APIKey string
}

// IdentityOf projects a stored config onto its identity. Only the active
// provider's fields are read: a sub-struct left behind by an earlier provider
// switch says nothing about where today's sandboxes live.
//
// Because named configs inherit nothing, the stored row is the whole picture and
// no deployment baseline is consulted. Unknown type strings are compared verbatim
// rather than rejected: a typo yields an identity that matches nothing, which
// errs toward refusing the edit, and ParseSandboxType reports it on save.
//
// This runs no SSRF guard and returns no error on purpose. It answers "would this
// edit strand anything", a question that must stay answerable when the OLD
// endpoint no longer resolves — precisely the situation in which an admin needs
// to re-point the config. Validating the incoming URLs is the save path's job.
func IdentityOf(tenantCfg *types.TenantSandboxConfig) SandboxIdentity {
	if tenantCfg == nil {
		return SandboxIdentity{}
	}
	identity := SandboxIdentity{
		Provider:              tenantCfg.SandboxType,
		AllowPrivateEndpoints: tenantCfg.AllowPrivateEndpoints,
	}
	switch SandboxType(tenantCfg.SandboxType) {
	case SandboxTypeDocker:
		// The daemon endpoint is both planes at once: containers are created,
		// exec'd and deleted over the same socket, so re-pointing it strands
		// every sandbox this config owns. The image is not part of the
		// identity — changing it only affects sandboxes created afterwards.
		if docker := tenantCfg.Docker; docker != nil {
			identity.APIURL = docker.Host
			identity.APIKey = docker.TLSCertPath
		}
	}
	return identity
}
