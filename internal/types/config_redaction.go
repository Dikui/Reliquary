package types

// CredentialsConfigForResponse returns a copy with app_secret redacted when
// maskSecrets is true.
func CredentialsConfigForResponse(cfg *CredentialsConfig, maskSecrets bool) *CredentialsConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if !maskSecrets {
		return &out
	}
	if out.WeKnoraCloud != nil {
		cloud := *out.WeKnoraCloud
		if cloud.AppSecret != "" {
			cloud.AppSecret = RedactedSecretPlaceholder
		}
		out.WeKnoraCloud = &cloud
	}
	return &out
}
