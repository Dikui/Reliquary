package sandbox

import (
	"errors"
	"fmt"
	"strings"
)

// ErrSandboxConfigIncomplete marks a named config that cannot build a working
// client because required fields are empty. Callers map it onto 400.
var ErrSandboxConfigIncomplete = errors.New("sandbox: config is missing required fields")

// MissingRequiredFields lists the fields cfg fails to supply for its own
// provider, named after the JSON keys the API and the settings form use so the
// message can be surfaced without translation. Disabled holds no
// backend-specific values; Docker must explicitly name its image.
func MissingRequiredFields(cfg *Config) []string {
	if cfg == nil {
		return nil
	}
	var missing []string
	require := func(field, value string) {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, field)
		}
	}
	switch cfg.Type {
	case SandboxTypeDocker:
		require("image", cfg.DockerImage)
	}
	return missing
}

// RequireCompleteConfig is the error form of MissingRequiredFields, shared by
// the save path and the resolve path so both reject the same configs with the
// same wording.
func RequireCompleteConfig(cfg *Config) error {
	missing := MissingRequiredFields(cfg)
	if len(missing) == 0 {
		return nil
	}
	provider := ""
	if cfg != nil {
		provider = string(cfg.Type)
	}
	return fmt.Errorf(
		"%w: %s backend requires %s",
		ErrSandboxConfigIncomplete, provider, strings.Join(missing, ", "),
	)
}
