package types

type EntryProcessOverrides struct {
	ChunkingConfig *ChunkingConfig `json:"chunking_config,omitempty"`
}
type EffectiveProcessConfig struct{ ChunkingConfig ChunkingConfig }
