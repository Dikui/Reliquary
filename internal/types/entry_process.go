package types

// EntryProcessOverrides stores per-upload parse config overrides in entry metadata.
type EntryProcessOverrides struct {
	ParserEngineRules []ParserEngineRule `json:"parser_engine_rules,omitempty"`
	ChunkingConfig    *ChunkingConfig    `json:"chunking_config,omitempty"`
	// ParserEngineOverrides passes key-value configuration to document parsers
	// (e.g. mineru_parse_method=ocr). Merged with workspace-level overrides in the
	// parse pipeline; per-upload values take priority on conflict.
	ParserEngineOverrides map[string]string `json:"parser_engine_overrides,omitempty"`
}

// EffectiveProcessConfig is the merged view used by the parse pipeline.
type EffectiveProcessConfig struct {
	ChunkingConfig ChunkingConfig
}
