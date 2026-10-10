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
	// Whether this knowledge base is temporary (ephemeral) and should be hidden from UI
	IsTemporary bool `yaml:"is_temporary"            json:"is_temporary"            gorm:"default:false"`
	// Description of the knowledge base
	Description string `yaml:"description"             json:"description"`
	// Workspace ID
	TenantID uint64 `yaml:"tenant_id"               json:"tenant_id"`
	// DomainID selects the editable template used for this knowledge base.
	DomainID string `yaml:"domain_id" json:"domain_id" gorm:"type:varchar(36);not null"`
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
	// Extract config
	// QuestionGenerationConfig stores question generation configuration for document knowledge bases
	QuestionGenerationConfig *QuestionGenerationConfig `yaml:"question_generation_config" json:"question_generation_config" gorm:"column:question_generation_config;type:json"`
	// AutoTagConfig controls asynchronous association of existing tags after parsing.
	AutoTagConfig *AutoTagConfig `yaml:"auto_tag_config" json:"auto_tag_config" gorm:"type:json"`
	// IndexingStrategy controls which indexing pipelines are active for this knowledge base.
	// Pipelines: vector search and keyword search.
	IndexingStrategy IndexingStrategy `yaml:"indexing_strategy"       json:"indexing_strategy"       gorm:"column:indexing_strategy;type:json"`
	// Pin state belongs to the personal space and is shared by all API callers.
	IsPinned bool `yaml:"is_pinned" json:"is_pinned" gorm:"default:false"`
	PinnedAt *time.Time `yaml:"pinned_at" json:"pinned_at"`

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
	// CreatorName 是 CreatorID 对应用户的展示名（username / email 等），
	// 仅在列表场景由 handler 批量回填，不落库；为空表示创建者无法解析（用户已删除、
	// CreatorID 为空的老数据等）。前端用它在卡片来源徽章上做 mine vs workspace 的二分。
	CreatorName string `yaml:"-"                       json:"creator_name,omitempty"  gorm:"-"`
}

// KnowledgeBaseConfig represents the knowledge base configuration
type KnowledgeBaseConfig struct {
	ModelTiers *ModelTierConfig `json:"model_tiers,omitempty"`
	// Chunking configuration
	ChunkingConfig ChunkingConfig `yaml:"chunking_config"         json:"chunking_config"`
	// Image processing configuration
	ImageProcessingConfig ImageProcessingConfig `yaml:"image_processing_config" json:"image_processing_config"`
	// AutoTagConfig controls optional automatic association of existing KB tags.
	AutoTagConfig *AutoTagConfig `yaml:"auto_tag_config" json:"auto_tag_config"`
	// IndexingStrategy controls which indexing pipelines are active.
	// nil means "no change" when updating (preserves existing strategy).
	IndexingStrategy *IndexingStrategy `yaml:"indexing_strategy"       json:"indexing_strategy"`
}

const (
	// DefaultAutoTagMaxTags is applied when max_tags is unset or non-positive.
	DefaultAutoTagMaxTags = 3
	// MaximumAutoTagMaxTags caps how many tags one document may auto-acquire.
	MaximumAutoTagMaxTags = 10
)

// AutoTagConfig controls asynchronous document auto-tagging. It is deliberately
// opt-in so upgrading an existing deployment does not add model calls or alter
// document tags unexpectedly.
type AutoTagConfig struct {
	Enabled bool   `yaml:"enabled" json:"enabled"`
	ModelID string `yaml:"model_id,omitempty" json:"model_id,omitempty"`
	MaxTags int    `yaml:"max_tags,omitempty" json:"max_tags,omitempty"`
	// Policy refines message classification while fixed tag kinds remain enforced by validation.
	Policy string `yaml:"policy,omitempty" json:"policy,omitempty"`
	// SkipIfTagged leaves documents that already carry tags untouched, so a
	// deliberate manual classification is not diluted by model guesses. It is
	// a pointer because the default is true: rows written before this field
	// existed decode to nil and must not silently flip to "always append".
	SkipIfTagged *bool `yaml:"skip_if_tagged,omitempty" json:"skip_if_tagged,omitempty"`
}

// ShouldSkipIfTagged reports whether documents with existing tags are left
// alone. Defaults to true for nil receivers and unset values.
func (c *AutoTagConfig) ShouldSkipIfTagged() bool {
	if c == nil || c.SkipIfTagged == nil {
		return true
	}
	return *c.SkipIfTagged
}

// Value serializes the automatic-tagging configuration for database storage.
func (c AutoTagConfig) Value() (driver.Value, error) { return json.Marshal(c) }

// Scan deserializes the automatic-tagging configuration from a database value.
func (c *AutoTagConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		if s, ok := value.(string); ok {
			b = []byte(s)
		} else {
			return nil
		}
	}
	return json.Unmarshal(b, c)
}

// Normalize applies defaults and bounds to the automatic-tagging configuration.
func (c *AutoTagConfig) Normalize() {
	if c == nil {
		return
	}
	if c.MaxTags <= 0 {
		c.MaxTags = DefaultAutoTagMaxTags
	}
	if c.MaxTags > MaximumAutoTagMaxTags {
		c.MaxTags = MaximumAutoTagMaxTags
	}
	if c.SkipIfTagged == nil {
		skip := true
		c.SkipIfTagged = &skip
	}
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

// DefaultParserEngine selects a retained parser independent of build tags.
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
	if kb.Type == "" {
		kb.Type = KnowledgeBaseTypeDocument
	}
	// Clear type-specific configs that don't belong

	if kb.Type != KnowledgeBaseTypeDocument {
		kb.AutoTagConfig = nil
	} else if kb.AutoTagConfig != nil {
		kb.AutoTagConfig.Normalize()
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
// can filter / enable / disable KB options based on what the selected agent type needs.
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
// `capabilities` field so clients (agent editor) can filter KBs by feature.
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
