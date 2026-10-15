package service

import "reliquary/internal/types"

// ResolveProcessConfig merges Library defaults with saved Markdown processing overrides.
func ResolveProcessConfig(library *types.Library, overrides *types.EntryProcessOverrides) types.EffectiveProcessConfig {
	eff := types.EffectiveProcessConfig{
		ChunkingConfig: library.ChunkingConfig,
	}
	if overrides == nil {
		return eff
	}

	if overrides.ChunkingConfig != nil {
		eff.ChunkingConfig = mergeChunkingConfig(eff.ChunkingConfig, overrides.ChunkingConfig)
	}
	return eff
}

func mergeChunkingConfig(base types.ChunkingConfig, override *types.ChunkingConfig) types.ChunkingConfig {
	if override == nil {
		return base
	}
	result := base
	if override.ChunkSize != 0 {
		result.ChunkSize = override.ChunkSize
	}
	if override.ChunkOverlapSet || override.ChunkOverlap != 0 {
		result.ChunkOverlap = override.ChunkOverlap
		result.ChunkOverlapSet = override.ChunkOverlapSet
	}
	if len(override.Separators) > 0 {
		result.Separators = override.Separators
	}
	// EnableParentChild is authoritative: callers send a full chunking snapshot,
	// so an explicit false must be able to turn parent-child off (not just on).
	result.EnableParentChild = override.EnableParentChild
	if override.ParentChunkSize != 0 {
		result.ParentChunkSize = override.ParentChunkSize
	}
	if override.ChildChunkSize != 0 {
		result.ChildChunkSize = override.ChildChunkSize
	}
	if override.Strategy != "" {
		result.Strategy = override.Strategy
	}
	if override.TokenLimit != 0 {
		result.TokenLimit = override.TokenLimit
	}
	if len(override.Languages) > 0 {
		result.Languages = override.Languages
	}
	return result
}
