package service

import (
	"context"
	"strings"

	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func normalizeParserFileType(fileType string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
}

// ResolveProcessConfig merges KB defaults with per-upload overrides for the parse pipeline.
func ResolveProcessConfig(kb *types.KnowledgeBase, overrides *types.EntryProcessOverrides) types.EffectiveProcessConfig {
	eff := types.EffectiveProcessConfig{
		ChunkingConfig: kb.ChunkingConfig,
	}
	if overrides == nil {
		return eff
	}

	if overrides.ChunkingConfig != nil {
		eff.ChunkingConfig = mergeChunkingConfig(eff.ChunkingConfig, overrides.ChunkingConfig)
	}
	if len(overrides.ParserEngineRules) > 0 {
		eff.ChunkingConfig.ParserEngineRules = overrides.ParserEngineRules
	}
	return eff
}

// ValidateProcessOverrides validates batch overrides against file types in the upload.
func ValidateProcessOverrides(
	ctx context.Context,
	kb *types.KnowledgeBase,
	overrides *types.EntryProcessOverrides,
	fileTypes []string,
) error {
	if overrides == nil {
		return nil
	}

	eff := ResolveProcessConfig(kb, overrides)
	if err := types.ValidateEffectiveProcessPromptInstructions(eff); err != nil {
		return werrors.NewBadRequestError(err.Error())
	}

	return nil
}

// ApplyEntryProcessOverrides validates optional overrides, persists them on the
// entry record, and returns the effective config for task enqueue.
func ApplyEntryProcessOverrides(
	ctx context.Context,
	kb *types.KnowledgeBase,
	entry *types.Entry,
	processOverrides *types.EntryProcessOverrides,
	fileTypes []string,
	enableMultimodel *bool,
) (types.EffectiveProcessConfig, error) {
	eff := ResolveProcessConfig(kb, processOverrides)
	if processOverrides == nil {
		return eff, nil
	}
	if err := ValidateProcessOverrides(ctx, kb, processOverrides, fileTypes); err != nil {
		return eff, err
	}
	if err := entry.SetProcessOverrides(processOverrides); err != nil {
		return eff, err
	}
	return eff, nil
}

// reparseFileTypes derives the file types used to validate overrides on reparse.
// Manual entry has no file; URL imports validate as html.
func reparseFileTypes(k *types.Entry) []string {
	if k == nil || k.IsManual() {
		return nil
	}
	if k.Type == "url" {
		return []string{"html"}
	}
	ft := k.FileType
	if ft == "" && k.FileName != "" {
		ft = getFileType(k.FileName)
	}
	if ft == "" {
		return nil
	}
	return []string{ft}
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
	if len(override.ParserEngineRules) > 0 {
		result.ParserEngineRules = override.ParserEngineRules
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
	if override.TableMetadataInstructions != "" {
		result.TableMetadataInstructions = override.TableMetadataInstructions
	}
	return result
}

// MergeParserEngineOverrides merges upload overrides on top of space overrides safely.
func MergeParserEngineOverrides(spaceOverrides map[string]string, uploadOverrides map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range spaceOverrides {
		merged[k] = v
	}
	for k, v := range uploadOverrides {
		merged[k] = v
	}
	return merged
}
