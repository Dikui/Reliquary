package types

import (
	"crypto/md5"
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"gorm.io/gorm"
)

// LibraryType represents the type of the library
const (
	// LibraryTypeDocument represents the document library type
	LibraryTypeDocument = "document"
)

// Library represents a library entity
type Library struct {
	SystemRole         string          `json:"system_role"`
	Template           string          `json:"template" gorm:"default:own"`
	Directory          string          `json:"directory"`
	ReviewCriteria     string          `json:"review_criteria"`
	SourceEvidenceRule string          `json:"source_evidence_rule"`
	FeedEnabled        bool            `json:"feed_enabled"`
	ModelTiers         ModelTierConfig `json:"model_tiers" gorm:"type:jsonb;not null;default:'{}'"`
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
	// Long references match on small child chunks but need the parent's context; fixed by template.
	if library.Template == TemplateExternalDurable {
		library.ChunkingConfig.EnableParentChild = true
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
	rules, _ := RulesForTemplate(library.Template)
	aux := struct {
		*alias
		AcceptedFields        map[string]string   `json:"accepted_fields"`
		Capabilities          LibraryCapabilities `json:"capabilities"`
		Rules                 TemplateRules       `json:"template_rules"`
		DeduplicationRule     string              `json:"deduplication_rule"`
		CollectContent        string              `json:"collect_content"`
		ExcludeContent        string              `json:"exclude_content"`
		DefaultReviewCriteria string              `json:"default_review_criteria"`
		ProcessingModelTiers  []ModelTier         `json:"processing_model_tiers"`
	}{
		alias: (*alias)(library), AcceptedFields: library.IngestFields(),
		Capabilities: library.Capabilities(), Rules: rules, DeduplicationRule: rules.Deduplication, CollectContent: library.Description, ExcludeContent: library.ReviewCriteria,
		DefaultReviewCriteria: library.DefaultReviewCriteria(), ProcessingModelTiers: library.ProcessingModelTiers(),
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

// BeforeCreate keeps internal constructors and SQL directory defaults aligned.
// Public creation validates the caller's template and directory before this hook.
func (library *Library) BeforeCreate(_ *gorm.DB) error {
	if library.ID == "" {
		library.ID = uuid.NewString()
	}
	if library.Template == "" {
		library.Template = TemplateOwn
	}
	if library.Directory == "" {
		library.Directory = fmt.Sprintf("custom-%x", md5.Sum([]byte(library.ID)))
	}
	return nil
}

// IngestFields describes shared admission fields in the public catalog.
func (library *Library) IngestFields() map[string]string {
	fields := map[string]string{"library": "Required library ID or directory"}
	fields["source_id"] = "Optional stable source ID, at most 2048 Unicode code points; requires source_revision"
	fields["source_revision"] = "Nonnegative int64, required with source_id; only strictly newer revisions are accepted"
	fields["source_ref"] = "Optional source identity, at most 2048 Unicode code points; changed content replaces the existing entry"
	fields["identifier"] = "Optional scheme:value, at most 2048 Unicode code points; scheme and DOI value are lowercased"
	if library.Template == TemplateGenerated {
		fields["mentioned_fact_ids"] = "Optional existing fact IDs to mention; empty array clears mentions; " +
			"never source evidence"
	}
	fields["acquisition"] = "Optional verbatim, extracted or model_written; omitted means unknown"
	return fields
}
