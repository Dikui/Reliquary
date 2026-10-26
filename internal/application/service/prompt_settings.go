package service

import (
	"strings"

	"github.com/google/uuid"
	"reliquary/internal/config"
)

func promptIDFromSettingKey(key string) string {
	parts := strings.Split(key, ".")
	return parts[len(parts)-1]
}

func settingSpecForKey(key string) (settingSpec, bool) {
	if spec, ok := registry[key]; ok {
		return spec, true
	}
	parts := strings.Split(key, ".")
	if len(parts) != 4 || parts[0] != "prompt" || parts[1] != "library" {
		return settingSpec{}, false
	}
	if _, err := uuid.Parse(parts[2]); err != nil {
		return settingSpec{}, false
	}
	for _, id := range config.PromptScopeIDs(true) {
		if id == parts[3] {
			return settingSpec{Type: "string", Category: "prompts"}, true
		}
	}
	return settingSpec{}, false
}
