package types

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	"gorm.io/gorm"
)

// KnowledgeBaseType represents the type of the knowledge base
const (
	// KnowledgeBaseTypeDocument represents the document knowledge base type
	KnowledgeBaseTypeDocument = "document"
)

// KnowledgeBase represents a knowledge base entity
type KnowledgeBase struct {
	ModelTiers ModelTierConfig `json:"model_tiers" gorm:"type:jsonb;not null;default:'{}'"`
	// Unique identifier of the knowledge base
	ID string `yaml:"id"                      json:"id"                      gorm:"type:varchar(36);primaryKey"`
	// Name of the knowledge base
	Name string `yaml:"name"                    json:"name"`
	Type string `yaml:"type"                    json:"type"                    gorm:"type:varchar(32);default:'document'"`
	// Description of the knowledge base
	Description string `yaml:"description"             json:"description"`
	// Workspace ID
	TenantID uint64 `yaml:"tenant_id"               json:"tenant_id"`
	// CreatorID records the user who created this team knowledge base.
	CreatorID string `yaml:"creator_id"              json:"creator_id"              gorm:"type:varchar(36);index"`
	// Chunking configuration
	ChunkingConfig ChunkingConfig `yaml:"chunking_config"         json:"chunking_config"         gorm:"type:json"`
	// Image processing configuration
	ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config" gorm:"type:json"`
	// ID of the embedding model
	EmbeddingModelID string `yaml:"embedding_model_id"      json:"embedding_model_id"`
	// Summary model ID
	SummaryModelID string `yaml:"summary_model_id"        json:"summary_model_id"`
	// VLM config
	VLMConfig VLMConfig `yaml:"vlm_config"              json:"vlm_config"              gorm:"type:json"`
	// QuestionGenerationConfig stores question generation configuration for document knowledge bases
	QuestionGenerationConfig *QuestionGenerationConfig `yaml:"question_generation_config" json:"question_generation_config" gorm:"column:question_generation_config;type:json"`
	// IndexingStrategy controls which indexing pipelines are active for this knowledge base.
	// Pipelines: vector search and keyword search.
	IndexingStrategy IndexingStrategy `yaml:"indexing_strategy"       json:"indexing_strategy"       gorm:"column:indexing_strategy;type:json"`
	// Creation time of the knowledge base
	CreatedAt time.Time `yaml:"created_at"              json:"created_at"`
	// Last updated time of the knowledge base
	UpdatedAt time.Time `yaml:"updated_at"              json:"updated_at"`
	// Deletion time of the knowledge base
	DeletedAt gorm.DeletedAt `yaml:"deleted_at"              json:"deleted_at"              gorm:"index"`
	// Knowledge count (not stored in database, calculated on query)
	KnowledgeCount int64 `yaml:"knowledge_count"         json:"knowledge_count"         gorm:"-"`
	// Chunk count (not stored in database, calculated on query)
	ChunkCount   int64 `yaml:"chunk_count"             json:"chunk_count"             gorm:"-"`
	IsProcessing bool  `yaml:"is_processing"           json:"is_processing"           gorm:"-"`
	// ProcessingCount indicates the number of knowledge items being processed (for document type knowledge bases)
	ProcessingCount int64 `yaml:"processing_count"        json:"processing_count"        gorm:"-"`
}

// KnowledgeBaseConfig represents the knowledge base configuration
type KnowledgeBaseConfig struct {
	ModelTiers *ModelTierConfig `json:"model_tiers,omitempty"`
	// Chunking configuration
	ChunkingConfig *ChunkingConfig `yaml:"chunking_config"         json:"chunking_config,omitempty"`
	// Image processing configuration
	ImageProcessingConfig    *ImageProcessingConfig    `yaml:"image_processing_config" json:"image_processing_config,omitempty"`
	VLMConfig                *VLMConfig                `json:"vlm_config,omitempty"`
	QuestionGenerationConfig *QuestionGenerationConfig `json:"question_generation_config,omitempty"`
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

// ImageProcessingConfig represents the image processing configuration
type ImageProcessingConfig struct {
	// Model ID
	ModelID string `yaml:"model_id" json:"model_id"`
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

// Value implements the driver.Valuer interface, used to convert ImageProcessingConfig to database value
func (c ImageProcessingConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface, used to convert database value to ImageProcessingConfig
func (c *ImageProcessingConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// VLMConfig represents the VLM configuration
type VLMConfig struct {
	Enabled bool   `yaml:"enabled"  json:"enabled"`
	ModelID string `yaml:"model_id" json:"model_id"`
	// DescriptionLanguage controls the language used for generated image
	// captions. Empty means follow the document/request language.
	DescriptionLanguage string `yaml:"description_language,omitempty" json:"description_language,omitempty"`
	// CustomInstructions adds KB-specific image interpretation guidance without
	// replacing the system-owned OCR and Markdown output contract.
	CustomInstructions string `yaml:"custom_instructions,omitempty" json:"custom_instructions,omitempty"`

	// 兼容老版本
	// Model Name
	ModelName string `yaml:"model_name" json:"model_name"`
	// Base URL
	BaseURL string `yaml:"base_url" json:"base_url"`
	// API Key
	APIKey string `yaml:"api_key" json:"api_key"`
	// Interface Type: "ollama" or "openai"
	InterfaceType string `yaml:"interface_type" json:"interface_type"`
}

// IsEnabled 判断多模态是否启用（兼容新老版本）
// 新版本：Enabled && ModelID != ""
// 老版本：ModelName != "" && BaseURL != ""
func (c VLMConfig) IsEnabled() bool {
	// 新版本配置
	if c.Enabled && c.ModelID != "" {
		return true
	}
	// 兼容老版本配置
	if c.ModelName != "" && c.BaseURL != "" {
		return true
	}
	return false
}

// QuestionGenerationConfig represents the question generation configuration for document knowledge bases
// When enabled, the system will use LLM to generate questions for each chunk during document parsing
// These generated questions will be indexed separately to improve recall
type QuestionGenerationConfig struct {
	Enabled bool `yaml:"enabled"  json:"enabled"`
	// Number of questions to generate per chunk (default: 3, max: 10)
	QuestionCount int `yaml:"question_count" json:"question_count"`
	// CustomInstructions describes the intended audience or question style.
	// It is appended to the stable system question-generation template.
	CustomInstructions string `yaml:"custom_instructions,omitempty" json:"custom_instructions,omitempty"`
}

// Value implements the driver.Valuer interface
func (c QuestionGenerationConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface
func (c *QuestionGenerationConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// Value implements the driver.Valuer interface, used to convert VLMConfig to database value
func (c VLMConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface, used to convert database value to VLMConfig
func (c *VLMConfig) Scan(value interface{}) error {
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
func (kb *KnowledgeBase) EnsureDefaults() {
	if kb == nil {
		return
	}
	kb.Type = KnowledgeBaseTypeDocument
	if kb.ChunkingConfig.ChunkSize <= 0 {
		kb.ChunkingConfig.ChunkSize = 512
	}
	if !kb.ChunkingConfig.ChunkOverlapSet && kb.ChunkingConfig.ChunkOverlap == 0 {
		kb.ChunkingConfig.ChunkOverlap = min(50, kb.ChunkingConfig.ChunkSize/10)
	}
	if len(kb.ChunkingConfig.Separators) == 0 {
		kb.ChunkingConfig.Separators = []string{"\n\n", "\n", "。"}
	}

	// Ensure IndexingStrategy has defaults.
	// For existing rows where indexing_strategy is NULL, GORM Scan() returns
	// DefaultIndexingStrategy() (vector+keyword=true). This block handles the
	// case where a fresh struct was created in-memory without touching DB.
	if kb.IndexingStrategy.IsZero() {
		kb.IndexingStrategy = DefaultIndexingStrategy()
	}

}

// KBCapabilities describes the functional features a knowledge base exposes.
// It is computed from the KB's configuration (IndexingStrategy and Type)
// and surfaced in the JSON representation of a KnowledgeBase so that the frontend
// can identify which retrieval operations are available.
type KBCapabilities struct {
	// Vector means semantic (embedding) search is indexed.
	Vector bool `json:"vector"`
	// Keyword means BM25 / sparse keyword search is indexed.
	Keyword bool `json:"keyword"`
	// Context means structured knowledge context queries are supported.
	Context bool `json:"context"`
}

// Capabilities returns the computed capability flags for this KB.
// Safe to call on a nil KB (returns zero value).
func (kb *KnowledgeBase) Capabilities() KBCapabilities {
	if kb == nil {
		return KBCapabilities{}
	}
	return KBCapabilities{
		Vector:  kb.IsVectorEnabled(),
		Keyword: kb.IsKeywordEnabled(),
		Context: true,
	}
}

// MarshalJSON augments the default JSON encoding of KnowledgeBase with a computed
// `capabilities` field so clients can inspect indexing support.
// It preserves all existing fields verbatim.
func (kb *KnowledgeBase) MarshalJSON() ([]byte, error) {
	type alias KnowledgeBase
	aux := struct {
		*alias
		Capabilities KBCapabilities `json:"capabilities"`
	}{
		alias:        (*alias)(kb),
		Capabilities: kb.Capabilities(),
	}
	return json.Marshal(aux)
}

// IsVectorEnabled checks if vector (semantic) search is enabled.
func (kb *KnowledgeBase) IsVectorEnabled() bool {
	return kb != nil && kb.IndexingStrategy.VectorEnabled
}

// IsKeywordEnabled checks if keyword (BM25) search is enabled.
func (kb *KnowledgeBase) IsKeywordEnabled() bool {
	return kb != nil && kb.IndexingStrategy.KeywordEnabled
}

// NeedsEmbeddingModel returns true if any enabled pipeline requires an embedding model.
// Currently only vector and keyword search need embeddings.
func (kb *KnowledgeBase) NeedsEmbeddingModel() bool {
	return kb != nil && kb.IndexingStrategy.NeedsEmbedding()
}

// IsMultimodalEnabled 判断多模态是否启用，由 VLMConfig.IsEnabled() 决定。
func (kb *KnowledgeBase) IsMultimodalEnabled() bool {
	if kb == nil {
		return false
	}
	return kb.VLMConfig.IsEnabled()
}
