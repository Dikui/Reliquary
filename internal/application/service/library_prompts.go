package service

import (
	"reliquary/internal/config"
	"reliquary/internal/types"
)

// The library is loaded once for an article; saving settings does not mutate it.
func libraryPrompt(library *types.Library, id string) string {
	if library != nil {
		if content, ok := library.PromptOverrides[id]; ok {
			return content
		}
	}
	return config.Prompt(id)
}
