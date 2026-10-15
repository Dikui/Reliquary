package types

import (
	"database/sql/driver"
	"encoding/json"
)

// IndexingStrategy controls which indexing pipelines are active for a library.
// Each boolean flag independently enables/disables a processing pipeline.
// When a document is uploaded, only the enabled pipelines will run.
type IndexingStrategy struct {
	// VectorEnabled enables semantic vector embedding and search
	VectorEnabled bool `yaml:"vector_enabled" json:"vector_enabled"`
	// KeywordEnabled enables keyword-based (BM25) search
	KeywordEnabled bool `yaml:"keyword_enabled" json:"keyword_enabled"`
}

// DefaultIndexingStrategy returns the default strategy matching the legacy behavior:
// vector and keyword indexing enabled.
func DefaultIndexingStrategy() IndexingStrategy {
	return IndexingStrategy{
		VectorEnabled:  true,
		KeywordEnabled: true,
	}
}

// NeedsEmbedding returns true if any pipeline that requires an embedding model is enabled.
func (s IndexingStrategy) NeedsEmbedding() bool {
	return s.VectorEnabled || s.KeywordEnabled
}

// NeedsChunks returns true if any pipeline that requires document chunks is enabled.
// Chunks are needed for vector indexing and keyword indexing.
func (s IndexingStrategy) NeedsChunks() bool {
	return s.VectorEnabled || s.KeywordEnabled
}

// HasAnyIndexing returns true if at least one indexing pipeline is enabled.
func (s IndexingStrategy) HasAnyIndexing() bool {
	return s.VectorEnabled || s.KeywordEnabled
}

// IsZero returns true if the strategy has no pipelines enabled (zero value).
func (s IndexingStrategy) IsZero() bool {
	return !s.VectorEnabled && !s.KeywordEnabled
}

// Value implements the driver.Valuer interface for GORM serialization.
func (s IndexingStrategy) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface for GORM deserialization.
// When the database column is NULL (existing rows before migration),
// it returns DefaultIndexingStrategy() for backward compatibility.
func (s *IndexingStrategy) Scan(value interface{}) error {
	if value == nil {
		*s = DefaultIndexingStrategy()
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		*s = DefaultIndexingStrategy()
		return nil
	}
	if err := json.Unmarshal(b, s); err != nil {
		*s = DefaultIndexingStrategy()
		return nil
	}
	return nil
}
