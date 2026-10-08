package types

import "strings"

// WebSearchConfigForResponse returns a copy safe for HTTP responses.
// When maskSecrets is true, api_key is omitted and a configured proxy_url
// is replaced with RedactedSecretPlaceholder.
func WebSearchConfigForResponse(cfg *WebSearchConfig, maskSecrets bool) *WebSearchConfig {
	if cfg == nil {
		return nil
	}
	out := *EffectiveWebSearchConfig(cfg)
	if !maskSecrets {
		return &out
	}
	out.APIKey = ""
	if strings.TrimSpace(out.ProxyURL) != "" {
		out.ProxyURL = RedactedSecretPlaceholder
	}
	return &out
}

// ParserEngineConfigForResponse returns a copy with secret fields redacted
// when maskSecrets is true.
func ParserEngineConfigForResponse(cfg *ParserEngineConfig, maskSecrets bool) *ParserEngineConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if !maskSecrets {
		return &out
	}
	if out.MinerUAPIKey != "" {
		out.MinerUAPIKey = RedactedSecretPlaceholder
	}
	return &out
}

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

// MergeWebSearchConfigForUpdate applies preserve semantics to secret fields on
// tenant KV PUT.
func MergeWebSearchConfigForUpdate(incoming, existing *WebSearchConfig) *WebSearchConfig {
	out := *EffectiveWebSearchConfig(incoming)
	var prev WebSearchConfig
	if existing != nil {
		prev = *EffectiveWebSearchConfig(existing)
	}
	out.APIKey = PreserveIfRedacted(out.APIKey, prev.APIKey)
	out.ProxyURL = PreserveIfRedacted(out.ProxyURL, prev.ProxyURL)
	return &out
}

// MergeParserEngineConfigForUpdate applies preserve semantics to secret fields
// on tenant KV PUT.
func MergeParserEngineConfigForUpdate(incoming, existing *ParserEngineConfig) *ParserEngineConfig {
	if incoming == nil {
		return nil
	}
	out := *incoming
	var prev ParserEngineConfig
	if existing != nil {
		prev = *existing
	}
	out.MinerUAPIKey = PreserveIfRedacted(out.MinerUAPIKey, prev.MinerUAPIKey)
	// Chat attachment parser rules are configured per agent; preserve any legacy
	// tenant-level rules when the settings UI omits this field on engine updates.
	if incoming.ChatParserEngineRules == nil && existing != nil {
		out.ChatParserEngineRules = existing.ChatParserEngineRules
	}
	return &out
}

// SandboxConfigForResponse returns a copy of cfg safe to serialize into an API
// response. When maskSecrets is true every secret-bearing field is replaced
// with RedactedSecretPlaceholder. Unset secrets stay empty so the UI can tell
// "configured" apart from "not configured".
func SandboxConfigForResponse(cfg *TenantSandboxConfig, maskSecrets bool) *TenantSandboxConfig {
	if cfg == nil {
		return nil
	}
	out := *cfg
	if !maskSecrets {
		return &out
	}

	// EnvVars values are encrypted at rest and may hold credentials, so they
	// are masked as a class rather than by name.
	if len(out.EnvVars) > 0 {
		envVars := make(map[string]string, len(out.EnvVars))
		for name, value := range out.EnvVars {
			if value != "" {
				value = RedactedSecretPlaceholder
			}
			envVars[name] = value
		}
		out.EnvVars = envVars
	}

	return &out
}

// MergeSandboxConfigForUpdate resolves redacted placeholders in incoming
// against the currently stored config, so a client that never received the
// real secret can submit the rest of the form without wiping it. Pointers
// the editor does not own (SkillImage) are kept from existing.
func MergeSandboxConfigForUpdate(incoming, existing *TenantSandboxConfig) *TenantSandboxConfig {
	if incoming == nil {
		return nil
	}
	out := *incoming

	// Only keys present in incoming survive: deleting a row in the UI must
	// actually remove the variable rather than silently restore it.
	if len(out.EnvVars) > 0 {
		envVars := make(map[string]string, len(out.EnvVars))
		for name, value := range out.EnvVars {
			prev := ""
			if existing != nil {
				prev = existing.EnvVars[name]
			}
			envVars[name] = PreserveIfRedacted(value, prev)
		}
		out.EnvVars = envVars
	}

	if incoming.Network != nil {
		network := *incoming.Network
		out.Network = &network
	}

	// SkillImage is owned by the install path, not
	// by the sandbox settings form. The editor rebuilds the payload without
	// that field, so copying incoming as-is would wipe a live snapshot on
	// every runtime save and leave sessions booting the base template while the
	// skill rows still claim ready. Reading them from the stored row instead —
	// and clearing when there is no stored row — also means a crafted PUT can
	// neither plant nor wipe a pointer.
	out.SkillImage = nil
	if existing != nil {
		if existing.SkillImage != nil {
			image := *existing.SkillImage
			out.SkillImage = &image
		}
		// The runtime form omits skill_rollout. An empty incoming value must
		// not reset a saved "new_session" choice; the skills panel sends the
		// explicit next_turn token when the admin switches back.
		if strings.TrimSpace(out.SkillRollout) == "" {
			out.SkillRollout = existing.SkillRollout
		}
	}

	return &out
}
