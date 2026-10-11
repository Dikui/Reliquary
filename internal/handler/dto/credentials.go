package dto

// CredentialFieldMetadata reports whether a credential has a stored value.
type CredentialFieldMetadata struct {
	Configured bool `json:"configured"`
}

// CredentialsResponse returns configured fields after a credential update.
type CredentialsResponse struct {
	Fields map[string]CredentialFieldMetadata `json:"fields"`
}
