package types

// SandboxNetworkPolicy controls outbound access for a Docker sandbox.
type SandboxNetworkPolicy struct {
	DenyEgressByDefault bool `json:"deny_egress_by_default,omitempty"`
}
