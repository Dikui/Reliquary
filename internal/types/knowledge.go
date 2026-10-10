package types

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	// KnowledgeTypeManual represents the manual knowledge type
	KnowledgeTypeManual = "manual"
)

// Channel constants identify through which channel a knowledge entry was ingested.
// Aligned with Message.Channel values ("web", "api") but allows finer granularity.
const (
	ChannelWeb              = "web"               // Web UI (default)
	ChannelAPI              = "api"               // External API call
	ChannelBrowserExtension = "browser_extension" // Browser extension / plugin

	ChannelRSS = "rss" // RSS / Atom feed

)

// Knowledge parse status constants
const (
	// ParseStatusPending indicates the knowledge is waiting to be processed
	ParseStatusPending = "pending"
	// ParseStatusProcessing indicates the knowledge is being processed
	ParseStatusProcessing = "processing"
	// ParseStatusFinalizing indicates the primary parse has finished but
	// enrichment subtasks (summary, question generation, context extract)
	// are still in flight. The user-facing intuition behind this state is
	// "the document is queryable for vector search but is still spending
	// resources" — cancel-parse can interrupt enrichment from here.
	// pending_subtasks_count holds the outstanding subtask count; the
	// last subtask to finish atomically promotes the row to completed.
	ParseStatusFinalizing = "finalizing"
	// ParseStatusCompleted indicates the knowledge has been processed
	// successfully AND every enrichment subtask has reached a terminal
	// state. No further resources will be spent on the document until
	// the user explicitly re-parses it.
	ParseStatusCompleted = "completed"
	// ParseStatusFailed indicates the knowledge processing failed
	ParseStatusFailed = "failed"
	// ParseStatusDeleting indicates the knowledge is being deleted (used to prevent async task conflicts)
	ParseStatusDeleting = "deleting"
	// ParseStatusCancelled indicates parsing was cancelled by the user.
	// Same short-circuit semantics as ParseStatusDeleting for in-flight and
	// queued downstream tasks, but the knowledge row and any already-written
	// chunks/index are kept so the user can re-trigger parsing via reparse.
	ParseStatusCancelled = "cancelled"
	// ParseStatusRejected is an intake decision: source content is retained for
	// owner review but it is never a formal knowledge document.
	ParseStatusRejected = "rejected"
)

// Summary status constants for async summary generation
const (
	// SummaryStatusNone indicates no summary task is needed
	SummaryStatusNone = "none"
	// SummaryStatusPending indicates the summary task is waiting to be processed
	SummaryStatusPending = "pending"
	// SummaryStatusProcessing indicates the summary is being generated
	SummaryStatusProcessing = "processing"
	// SummaryStatusCompleted indicates the summary has been generated successfully
	SummaryStatusCompleted = "completed"
	// SummaryStatusSkipped indicates the document has no usable text to summarize.
	SummaryStatusSkipped = "skipped"
	// SummaryStatusFailed indicates the summary generation failed
	SummaryStatusFailed = "failed"
)

// ManualKnowledgeFormat represents the format of the manual knowledge
const (
	ManualKnowledgeFormatMarkdown = "markdown"
	ManualKnowledgeStatusDraft    = "draft"
	ManualKnowledgeStatusPublish  = "publish"
)

// KnowledgeListFilter aggregates optional filters for listing knowledge entries
// under a knowledge base. Empty / zero fields mean "no filter on that dimension".
type KnowledgeListFilter struct {
	// ContentType filters the article purpose; unclassified matches an empty classification.
	ContentType string
	// Keyword performs a LIKE match on file_name / title when non-empty.
	Keyword string
	// FileType filters by file_type, or by type for the special values "manual" / "url".
	FileType string
	// ParseStatus filters by parse_status when non-empty (e.g. pending, processing, completed, failed).
	ParseStatus string
	// Source filters by ingestion channel when non-empty (web, api, rss).
	// The special values "manual" and "url" are routed to the `type` column to match
	// FileType semantics, so callers can filter "manually created" / "URL imported" entries.
	Source string
	// UpdatedFrom, when non-zero, keeps rows with updated_at >= UpdatedFrom.
	UpdatedFrom time.Time
	// UpdatedTo, when non-zero, keeps rows with updated_at <= UpdatedTo.
	UpdatedTo time.Time
}

// Knowledge represents a knowledge entity in the system.
// It contains metadata about the knowledge source, its processing status,
// and references to the physical file if applicable.
type Knowledge struct {
	// Unique identifier of the knowledge
	ID string `json:"id"                 gorm:"type:varchar(36);primaryKey"`
	// Workspace ID
	TenantID uint64 `json:"tenant_id"`
	// ID of the knowledge base
	KnowledgeBaseID string `json:"knowledge_base_id"`
	// Type of the knowledge
	Type string `json:"type"`
	// Title of the knowledge
	Title string `json:"title"`
	// Description of the knowledge
	Description       string            `json:"description"`
	ContentType       string            `json:"content_type" gorm:"type:varchar(16)"`
	ProcessingMode    string            `json:"processing_mode" gorm:"type:varchar(16);not null;default:'full'"`
	StructuredSummary *KnowledgeSummary `json:"structured_summary" gorm:"type:jsonb"`
	Entities          []Entity          `json:"entities" gorm:"-"`
	Topics            []Topic           `json:"topics" gorm:"-"`
	// DescriptionSpecified distinguishes an explicitly supplied empty description
	// from an omitted field in partial update requests.
	DescriptionSpecified bool `json:"-" gorm:"-"`
	// Source of the knowledge (e.g. URL address for url type, "manual" for manual type)
	Source string `json:"source"             gorm:"type:varchar(2048)"`
	// Channel indicates through which channel the knowledge was ingested (web, api, rss)
	Channel string `json:"channel"            gorm:"type:varchar(50);default:'web'"`
	// Parse status of the knowledge
	ParseStatus string `json:"parse_status"`
	// PendingSubtasksCount is the outstanding enrichment subtask count
	// (summary + question + context chunks). Only meaningful while
	// ParseStatus == "finalizing"; defaults to 0 in any terminal state.
	PendingSubtasksCount int `json:"pending_subtasks_count" gorm:"type:int;not null;default:0"`
	// Summary status for async summary generation
	SummaryStatus string `json:"summary_status"     gorm:"type:varchar(32);default:none"`
	// Enable status of the knowledge
	EnableStatus string `json:"enable_status"`
	// ID of the embedding model
	EmbeddingModelID string `json:"embedding_model_id"`
	// File name of the knowledge
	FileName string `json:"file_name"`
	// File type of the knowledge
	FileType string `json:"file_type"`
	// File size of the knowledge
	FileSize int64 `json:"file_size"`
	// File hash of the knowledge
	FileHash         string                  `json:"file_hash"`
	NormalizedURL    string                  `json:"-" gorm:"type:text;not null;default:''"`
	ContentHash      string                  `json:"-" gorm:"type:char(64);not null;default:''"`
	ContentSimHash   int64                   `json:"-" gorm:"not null;default:0"`
	ContentRuneCount int                     `json:"-" gorm:"not null;default:0"`
	RepostSources    []KnowledgeRepostSource `json:"repost_sources" gorm:"-"`
	// DuplicateMatchMethod describes this intake attempt, not stored provenance.
	DuplicateMatchMethod string `json:"-" gorm:"-"`
	// File path of the knowledge
	FilePath string `json:"file_path"`
	// Storage size of the knowledge
	StorageSize int64 `json:"storage_size"`
	// Metadata of the knowledge
	Metadata JSON `json:"metadata"           gorm:"type:json"`
	// CustomMetadata is user-authored descriptive metadata. It is deliberately
	// separate from Metadata, which contains internal ingestion state and IDs.
	CustomMetadata JSON `json:"custom_metadata" gorm:"type:json;not null"`
	// Creation time of the knowledge
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the knowledge
	UpdatedAt time.Time `json:"updated_at"`
	// Processed time of the knowledge
	ProcessedAt *time.Time `json:"processed_at"`
	// Error message of the knowledge
	ErrorMessage string `json:"error_message"`
	// Deletion time of the knowledge
	DeletedAt gorm.DeletedAt `json:"deleted_at"         gorm:"index"`
	// Knowledge base name (not stored in database, populated on query)
	KnowledgeBaseName string `json:"knowledge_base_name" gorm:"-"`
}

// CustomMetadataText returns stable human-readable metadata for summaries and
// document-scoped model context. Internal ingestion metadata is intentionally
// excluded.
func (k *Knowledge) CustomMetadataText() string {
	if k == nil || len(k.CustomMetadata) == 0 {
		return ""
	}
	var values map[string]interface{}
	if err := json.Unmarshal(k.CustomMetadata, &values); err != nil || len(values) == 0 {
		return ""
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		if values[key] == nil {
			continue
		}
		value := strings.TrimSpace(fmt.Sprint(values[key]))
		if strings.TrimSpace(key) != "" && value != "" {
			lines = append(lines, fmt.Sprintf("%s: %s", strings.TrimSpace(key), value))
		}
	}
	return strings.Join(lines, "\n")
}

// GetMetadata returns the metadata as a map[string]string.
func (k *Knowledge) GetMetadata() map[string]string {
	metadata := make(map[string]string)
	if len(k.Metadata) == 0 {
		return metadata
	}
	metadataMap, err := k.Metadata.Map()
	if err != nil {
		return nil
	}
	for k, v := range metadataMap {
		metadata[k] = fmt.Sprintf("%v", v)
	}
	return metadata
}

// BeforeCreate initializes required defaults for new Knowledge entities.
func (k *Knowledge) BeforeCreate(tx *gorm.DB) (err error) {
	if k.ID == "" {
		k.ID = uuid.New().String()
	}
	// JSON.Value returns SQL NULL for an empty value. PostgreSQL's column
	// default is not applied when GORM explicitly inserts that NULL, so keep the
	// application-side representation aligned with the NOT NULL database
	// invariant for every knowledge creation path.
	if len(k.CustomMetadata) == 0 {
		k.CustomMetadata = JSON(`{}`)
	}
	return nil
}

// ManualKnowledgeMetadata stores metadata for manual Markdown knowledge content.
type ManualKnowledgeMetadata struct {
	Content   string `json:"content"`
	Format    string `json:"format"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// SourceInfo is supplied or extracted source evidence. Values remain strings so
// a publisher's original date precision is never invented by this service.
type SourceInfo struct {
	Author      string `json:"author,omitempty"`
	Publisher   string `json:"publisher,omitempty"`
	OriginalURL string `json:"original_url,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
}

// KnowledgeIntakeMetadata records the single-message intake decision. Original
// bodies stay in object storage; this only contains the audit/display fields.

type KnowledgeIntakeMetadata struct {
	ModelCalls   []ModelCallCount `json:"model_calls,omitempty"`
	Source       string           `json:"source"`
	OriginalPath string           `json:"original_path,omitempty"`
	Images       []string         `json:"images,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Decision     string           `json:"decision"`
	SourceInfo   SourceInfo       `json:"source_info,omitempty"`
}

func (k *Knowledge) IntakeMetadata() (*KnowledgeIntakeMetadata, error) {
	if k == nil || len(k.Metadata) == 0 {
		return nil, nil
	}
	var raw struct {
		Intake *KnowledgeIntakeMetadata `json:"intake"`
	}
	if err := json.Unmarshal(k.Metadata, &raw); err != nil {
		return nil, err
	}
	return raw.Intake, nil
}

func (k *Knowledge) SetIntakeMetadata(intake *KnowledgeIntakeMetadata) error {
	var raw map[string]json.RawMessage
	if len(k.Metadata) > 0 && json.Unmarshal(k.Metadata, &raw) != nil {
		return fmt.Errorf("invalid knowledge metadata")
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	if intake == nil {
		delete(raw, "intake")
	} else {
		value, err := json.Marshal(intake)
		if err != nil {
			return err
		}
		raw["intake"] = value
	}
	value, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	k.Metadata = JSON(value)
	return nil
}

// ManualKnowledgePayload represents the payload for manual knowledge operations.
type ManualKnowledgePayload struct {
	Title          string                     `json:"title"`
	Content        string                     `json:"content"`
	Status         string                     `json:"status"`
	Channel        string                     `json:"channel"`
	ProcessConfig  *KnowledgeProcessOverrides `json:"process_config,omitempty"`
	SourceInfo     SourceInfo                 `json:"source_info,omitempty"`
	SourceMetadata map[string]string          `json:"-"`
}

// KnowledgeSearchScope keeps tenant and knowledge base boundaries explicit for repository queries.
type KnowledgeSearchScope struct {
	TenantID uint64
	KBID     string
}

// NewManualKnowledgeMetadata creates a new ManualKnowledgeMetadata instance.
func NewManualKnowledgeMetadata(content, status string, version int) *ManualKnowledgeMetadata {
	if version <= 0 {
		version = 1
	}
	return &ManualKnowledgeMetadata{
		Content:   content,
		Format:    ManualKnowledgeFormatMarkdown,
		Status:    status,
		Version:   version,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// ToJSON converts the metadata to JSON type.
func (m *ManualKnowledgeMetadata) ToJSON() (JSON, error) {
	if m == nil {
		return nil, nil
	}
	if m.Format == "" {
		m.Format = ManualKnowledgeFormatMarkdown
	}
	if m.Status == "" {
		m.Status = ManualKnowledgeStatusDraft
	}
	if m.Version <= 0 {
		m.Version = 1
	}
	if m.UpdatedAt == "" {
		m.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	bytes, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return JSON(bytes), nil
}

// ManualMetadata parses and returns manual knowledge metadata.
func (k *Knowledge) ManualMetadata() (*ManualKnowledgeMetadata, error) {
	if len(k.Metadata) == 0 {
		return nil, nil
	}
	var metadata ManualKnowledgeMetadata
	if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
		return nil, err
	}
	if metadata.Format == "" {
		metadata.Format = ManualKnowledgeFormatMarkdown
	}
	if metadata.Version <= 0 {
		metadata.Version = 1
	}
	return &metadata, nil
}

// SetManualMetadata sets manual knowledge metadata onto the knowledge instance.
func (k *Knowledge) SetManualMetadata(meta *ManualKnowledgeMetadata) error {
	if meta == nil {
		k.Metadata = nil
		return nil
	}
	jsonValue, err := meta.ToJSON()
	if err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if len(k.Metadata) > 0 {
		_ = json.Unmarshal(k.Metadata, &raw)
	}
	if raw == nil {
		raw = make(map[string]json.RawMessage)
	}
	var manual map[string]json.RawMessage
	if err := json.Unmarshal(jsonValue, &manual); err != nil {
		return err
	}
	for key, value := range manual {
		raw[key] = value
	}
	value, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	k.Metadata = JSON(value)
	return nil
}

// IsManual returns true if the knowledge item is manual Markdown knowledge.
func (k *Knowledge) IsManual() bool {
	return k != nil && k.Type == KnowledgeTypeManual
}

// EnsureManualDefaults sets default values for manual knowledge entries.
func (k *Knowledge) EnsureManualDefaults() {
	if k == nil {
		return
	}
	if k.Type == "" {
		k.Type = KnowledgeTypeManual
	}
	if k.FileType == "" {
		k.FileType = KnowledgeTypeManual
	}
	if k.Source == "" {
		k.Source = KnowledgeTypeManual
	}
	if k.Channel == "" {
		k.Channel = ChannelWeb
	}
}

// IsDraft returns whether the payload should be saved as draft.
func (p ManualKnowledgePayload) IsDraft() bool {
	return p.Status == "" || p.Status == ManualKnowledgeStatusDraft
}

const metadataKeyProcessOverrides = "process_overrides"

// ProcessOverrides parses process config overrides from knowledge metadata.
func (k *Knowledge) ProcessOverrides() (*KnowledgeProcessOverrides, error) {
	if k == nil || len(k.Metadata) == 0 {
		return nil, nil
	}
	metadataMap, err := k.Metadata.Map()
	if err != nil {
		return nil, err
	}
	raw, ok := metadataMap[metadataKeyProcessOverrides]
	if !ok || raw == nil {
		return nil, nil
	}
	bytes, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var overrides KnowledgeProcessOverrides
	if err := json.Unmarshal(bytes, &overrides); err != nil {
		return nil, err
	}
	return &overrides, nil
}

// SetProcessOverrides merges process config overrides into knowledge metadata.
func (k *Knowledge) SetProcessOverrides(o *KnowledgeProcessOverrides) error {
	if k == nil {
		return nil
	}
	metadataMap, err := k.Metadata.Map()
	if err != nil {
		return err
	}
	if o == nil {
		delete(metadataMap, metadataKeyProcessOverrides)
	} else {
		bytes, err := json.Marshal(o)
		if err != nil {
			return err
		}
		var value interface{}
		if err := json.Unmarshal(bytes, &value); err != nil {
			return err
		}
		metadataMap[metadataKeyProcessOverrides] = value
	}
	bytes, err := json.Marshal(metadataMap)
	if err != nil {
		return err
	}
	k.Metadata = JSON(bytes)
	return nil
}

// KnowledgeCheckParams defines parameters used to check if knowledge already exists.
type KnowledgeCheckParams struct {
	// File parameters
	FileName string
	// FileType scopes file-hash deduplication; callers checking file uploads should set it.
	FileType string
	FileSize int64
	FileHash string
	// URL parameters
	URL string
	// Text passage parameters
	Passages []string
	// Knowledge type
	Type string
}
