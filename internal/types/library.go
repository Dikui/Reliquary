package types

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

// LibraryType represents the type of the library
const (
	// LibraryTypeDocument represents the document library type
	LibraryTypeDocument = "document"
)

// Library represents a library entity
type Library struct {
	ModelTiers ModelTierConfig `json:"model_tiers" gorm:"type:jsonb;not null;default:'{}'"`
	// Unique identifier of the library
	ID string `yaml:"id"                      json:"id"                      gorm:"type:varchar(36);primaryKey"`
	// Name of the library
	Name string `yaml:"name"                    json:"name"`
	Type string `yaml:"type"                    json:"type"                    gorm:"type:varchar(32);default:'document'"`
	// Description of the library
	Description string `yaml:"description"             json:"description"`
	// Workspace ID
	SpaceID uint64 `yaml:"space_id"               json:"space_id"`
	// CreatorID records the user who created this team library.
	CreatorID string `yaml:"creator_id"              json:"creator_id"              gorm:"type:varchar(36);index"`
	// Chunking configuration
	ChunkingConfig ChunkingConfig `yaml:"chunking_config"         json:"chunking_config"         gorm:"type:json"`
	// ID of the embedding model
	EmbeddingModelID string `yaml:"embedding_model_id"      json:"embedding_model_id"`
	// Summary model ID
	SummaryModelID string `yaml:"summary_model_id"        json:"summary_model_id"`
	// IndexingStrategy controls which indexing pipelines are active for this library.
	// Pipelines: vector search and keyword search.
	IndexingStrategy IndexingStrategy `yaml:"indexing_strategy"       json:"indexing_strategy"       gorm:"column:indexing_strategy;type:json"`
	// Creation time of the library
	CreatedAt time.Time `yaml:"created_at"              json:"created_at"`
	// Last updated time of the library
	UpdatedAt time.Time `yaml:"updated_at"              json:"updated_at"`
	// Deletion time of the library
	DeletedAt gorm.DeletedAt `yaml:"deleted_at"              json:"deleted_at"              gorm:"index"`
	// Entry count (not stored in database, calculated on query)
	EntryCount int64 `yaml:"entry_count"         json:"entry_count"         gorm:"-"`
	// Chunk count (not stored in database, calculated on query)
	ChunkCount   int64 `yaml:"chunk_count"             json:"chunk_count"             gorm:"-"`
	IsProcessing bool  `yaml:"is_processing"           json:"is_processing"           gorm:"-"`
	// ProcessingCount indicates the number of entry items being processed (for document type libraries)
	ProcessingCount int64 `yaml:"processing_count"        json:"processing_count"        gorm:"-"`
}

// LibraryConfig represents the library configuration
type LibraryConfig struct {
	ModelTiers *ModelTierConfig `json:"model_tiers,omitempty"`
	// Chunking configuration
	ChunkingConfig *ChunkingConfig `yaml:"chunking_config"         json:"chunking_config,omitempty"`
	// IndexingStrategy controls which indexing pipelines are active.
	// nil means "no change" when updating (preserves existing strategy).
	IndexingStrategy *IndexingStrategy `yaml:"indexing_strategy"       json:"indexing_strategy"`
}

// ParserEngineRule maps a set of file types to a specific parser engine.
type ParserEngineRule struct {
	FileTypes []string `yaml:"file_types" json:"file_types"`
	Engine    string   `yaml:"engine"     json:"engine"`
}

// ChunkingConfig represents the document splitting configuration
type ChunkingConfig struct {
	// Chunk size
	ChunkSize int `yaml:"chunk_size"    json:"chunk_size"`
	// Chunk overlap
	ChunkOverlap int `yaml:"chunk_overlap" json:"chunk_overlap"`
	// ChunkOverlapSet records JSON presence without changing the public payload.
	ChunkOverlapSet bool `yaml:"-" json:"-"`
	// Separators
	Separators []string `yaml:"separators"    json:"separators"`
	// ParserEngineRules configures which parser engine to use for each file type.
	// When empty, DefaultParserEngine is used (builtin/simple routing, except
	// types that only a specific engine can parse: ppt/pptx fall back to
	// markitdown). A linked anydoc binding is preferred for every type it
	// converts.
	ParserEngineRules []ParserEngineRule `yaml:"parser_engine_rules,omitempty" json:"parser_engine_rules,omitempty"`
	// EnableParentChild enables two-level parent-child chunking strategy.
	// When enabled, large parent chunks provide context while small child chunks
	// are used for vector matching. Retrieval matches on child but returns parent content.
	EnableParentChild bool `yaml:"enable_parent_child,omitempty" json:"enable_parent_child,omitempty"`
	// ParentChunkSize is the size of parent chunks (default: 4096).
	// Only used when EnableParentChild is true.
	ParentChunkSize int `yaml:"parent_chunk_size,omitempty" json:"parent_chunk_size,omitempty"`
	// ChildChunkSize is the size of child chunks used for embedding (default: 384).
	// Only used when EnableParentChild is true.
	ChildChunkSize int `yaml:"child_chunk_size,omitempty" json:"child_chunk_size,omitempty"`
	// Strategy selects the adaptive chunking tier. Empty / "legacy" preserves
	// the historical recursive splitter; "auto" lets a profiler pick between
	// heading-aware, heuristic and recursive tiers; "heading" / "heuristic" /
	// "recursive" pin the tier explicitly.
	Strategy string `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	// TokenLimit caps chunk size in approximate tokens. 0 = use ChunkSize
	// as a character count.
	TokenLimit int `yaml:"token_limit,omitempty" json:"token_limit,omitempty"`
	// Languages hints the heuristic patterns. Empty = auto-detect from content.
	// Examples: ["de"], ["en", "zh"].
	Languages []string `yaml:"languages,omitempty" json:"languages,omitempty"`
	// TableMetadataInstructions contains optional business guidance used when
	// generating searchable summaries for CSV/Excel tables. The system-owned
	// output contract remains fixed; these instructions only add domain context.
	TableMetadataInstructions string `yaml:"table_metadata_instructions,omitempty" json:"table_metadata_instructions,omitempty"`
}

// Office documents and PDFs use anydoc so a missing binding reports an error.
func DefaultParserEngine(fileType string) string {
	switch normalizeParserFileType(fileType) {
	case "doc", "docx", "docm", "odt", "rtf", "ppt", "pptx", "pptm", "odp", "xls", "xlsx", "xlsm", "ods", "epub", "pdf":
		return "anydoc"
	default:
		return "simple"
	}
}

// ResolveParserEngine returns the engine name for the given file type
// based on the configured rules. When no rule matches it returns the
// type-level default (see DefaultParserEngine).
func (c ChunkingConfig) ResolveParserEngine(fileType string) string {
	if rule := c.ResolveParserEngineRule(fileType); rule != nil {
		return rule.Engine
	}
	return DefaultParserEngine(fileType)
}

// ResolveParserEngineRule returns the parser rule for a file type.
func (c ChunkingConfig) ResolveParserEngineRule(fileType string) *ParserEngineRule {
	fileType = normalizeParserFileType(fileType)
	if fileType == "" {
		return nil
	}
	for i := range c.ParserEngineRules {
		for _, ft := range c.ParserEngineRules[i].FileTypes {
			if normalizeParserFileType(ft) == fileType {
				return &c.ParserEngineRules[i]
			}
		}
	}
	return nil
}

func normalizeParserFileType(fileType string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(fileType)), ".")
}

// ParseProviderScheme extracts the local file scheme used by stored paths.
func ParseProviderScheme(filePath string) string {
	if strings.HasPrefix(filePath, "local://") {
		return "local"
	}
	return ""
}

// Preserve explicit zero through API decoding and persisted JSON. Omitted
// overlap must remain omitted when a default Go config is saved and reloaded.
func (c *ChunkingConfig) UnmarshalJSON(data []byte) error {
	type plain ChunkingConfig
	var value plain
	payload := struct {
		*plain
		ChunkOverlap *int `json:"chunk_overlap"`
	}{plain: &value}
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.ChunkOverlap != nil {
		value.ChunkOverlap = *payload.ChunkOverlap
		value.ChunkOverlapSet = true
	}
	*c = ChunkingConfig(value)
	return nil
}

func (c ChunkingConfig) MarshalJSON() ([]byte, error) {
	type plain ChunkingConfig
	var overlap *int
	if c.ChunkOverlapSet || c.ChunkOverlap != 0 {
		overlap = &c.ChunkOverlap
	}
	return json.Marshal(struct {
		*plain
		ChunkOverlap *int `json:"chunk_overlap,omitempty"`
	}{plain: (*plain)(&c), ChunkOverlap: overlap})
}

// Value implements the driver.Valuer interface, used to convert ChunkingConfig to database value
func (c ChunkingConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface, used to convert database value to ChunkingConfig
func (c *ChunkingConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// EnsureDefaults 确保类型与配置具备默认值
func (library *Library) EnsureDefaults() {
	if library == nil {
		return
	}
	library.Type = LibraryTypeDocument
	if library.ChunkingConfig.ChunkSize <= 0 {
		library.ChunkingConfig.ChunkSize = 512
	}
	if !library.ChunkingConfig.ChunkOverlapSet && library.ChunkingConfig.ChunkOverlap == 0 {
		library.ChunkingConfig.ChunkOverlap = min(50, library.ChunkingConfig.ChunkSize/10)
	}
	if len(library.ChunkingConfig.Separators) == 0 {
		library.ChunkingConfig.Separators = []string{"\n\n", "\n", "。"}
	}

	// Ensure IndexingStrategy has defaults.
	// For existing rows where indexing_strategy is NULL, GORM Scan() returns
	// DefaultIndexingStrategy() (vector+keyword=true). This block handles the
	// case where a fresh struct was created in-memory without touching DB.
	if library.IndexingStrategy.IsZero() {
		library.IndexingStrategy = DefaultIndexingStrategy()
	}

}

// LibraryCapabilities describes the functional features a library exposes.
// It is computed from the Library's configuration (IndexingStrategy and Type)
// and surfaced in the JSON representation of a Library so that the frontend
// can identify which retrieval operations are available.
type LibraryCapabilities struct {
	// Vector means semantic (embedding) search is indexed.
	Vector bool `json:"vector"`
	// Keyword means BM25 / sparse keyword search is indexed.
	Keyword bool `json:"keyword"`
}

// Capabilities returns the computed capability flags for this Library.
// Safe to call on a nil Library (returns zero value).
func (library *Library) Capabilities() LibraryCapabilities {
	if library == nil {
		return LibraryCapabilities{}
	}
	return LibraryCapabilities{
		Vector:  library.IsVectorEnabled(),
		Keyword: library.IsKeywordEnabled(),
	}
}

// MarshalJSON augments the default JSON encoding of Library with a computed
// `capabilities` field so clients can inspect indexing support.
// It preserves all existing fields verbatim.
func (library *Library) MarshalJSON() ([]byte, error) {
	type alias Library
	aux := struct {
		*alias
		Capabilities LibraryCapabilities `json:"capabilities"`
	}{
		alias:        (*alias)(library),
		Capabilities: library.Capabilities(),
	}
	return json.Marshal(aux)
}

// IsVectorEnabled checks if vector (semantic) search is enabled.
func (library *Library) IsVectorEnabled() bool {
	return library != nil && library.IndexingStrategy.VectorEnabled
}

// IsKeywordEnabled checks if keyword (BM25) search is enabled.
func (library *Library) IsKeywordEnabled() bool {
	return library != nil && library.IndexingStrategy.KeywordEnabled
}

// NeedsEmbeddingModel returns true if any enabled pipeline requires an embedding model.
// Currently only vector and keyword search need embeddings.
func (library *Library) NeedsEmbeddingModel() bool {
	return library != nil && library.IndexingStrategy.NeedsEmbedding()
}
