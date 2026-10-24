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
	// EntryTypeManual represents the manual entry type
	EntryTypeManual = "manual"
)

// Channel constants identify through which channel a entry entry was ingested.
// Aligned with Message.Channel values ("web", "api") but allows finer granularity.
const (
	ChannelWeb              = "web"               // Web UI (default)
	ChannelAPI              = "api"               // External API call
	ChannelBrowserExtension = "browser_extension" // Browser extension / plugin

	ChannelRSS = "rss" // RSS / Atom feed

)

// Entry parse status constants
const (
	// ParseStatusPending indicates the entry is waiting to be processed
	ParseStatusPending = "pending"
	// ParseStatusProcessing indicates the entry is being processed
	ParseStatusProcessing = "processing"
	// ParseStatusFinalizing indicates the primary parse has finished but
	// enrichment subtasks (summary, question generation, context extract)
	// are still in flight. The user-facing intuition behind this state is
	// "the document is queryable for vector search but is still spending
	// resources" — cancel-parse can interrupt enrichment from here.
	// pending_subtasks_count holds the outstanding subtask count; the
	// last subtask to finish atomically promotes the row to completed.
	ParseStatusFinalizing = "finalizing"
	// ParseStatusCompleted indicates the entry has been processed
	// successfully AND every enrichment subtask has reached a terminal
	// state. No further resources will be spent on the document until
	// the user explicitly re-parses it.
	ParseStatusCompleted = "completed"
	// ParseStatusFailed indicates the entry processing failed
	ParseStatusFailed = "failed"
	// ParseStatusDeleting indicates the entry is being deleted (used to prevent async task conflicts)
	ParseStatusDeleting = "deleting"
	// ParseStatusCancelled indicates parsing was cancelled by the user.
	// Same short-circuit semantics as ParseStatusDeleting for in-flight and
	// queued downstream tasks, but the entry row and any already-written
	// chunks/index are kept so the user can re-trigger parsing via reparse.
	ParseStatusCancelled = "cancelled"
	// ParseStatusRejected is an intake decision: source content is retained for
	// owner review but it is never a formal entry document.
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

// ManualEntryFormat represents the format of the manual entry
const (
	ManualEntryFormatMarkdown = "markdown"
	ManualEntryStatusDraft    = "draft"
	ManualEntryStatusPublish  = "publish"
)

// EntryListFilter aggregates optional filters for listing entry entries
// under a library. Empty / zero fields mean "no filter on that dimension".
type EntryListFilter struct {
	LibraryIDs      []string
	EntityIDs       []string
	PublishedFrom   time.Time
	PublishedTo     time.Time // Exclusive end of the selected publication day.
	IncludeRejected bool

	// SourceKind selects article, mine or report independently of ingestion channel.
	SourceKind string
	// Starred filters the user's article marker when supplied.
	Starred *bool
	// Keyword performs a LIKE match on file_name / title when non-empty.
	Keyword string
	// FileType filters by file_type, or by type for the special values "manual" / "url".
	FileType string
	// ParseStatus filters by parse_status when non-empty. Failed also includes
	// completed body indexes whose background processing failed.
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

// Entry represents a entry entity in the system.
// It contains metadata about the entry source, its processing status,
// and references to the physical file if applicable.
type Entry struct {
	ImageDescriptions []ProcessingImage `json:"image_descriptions,omitempty" gorm:"-"`

	SourceID       string `json:"source_id,omitempty" gorm:"not null;default:''"`
	SourceRevision *int64 `json:"source_revision,omitempty"`
	SourceRef      string `json:"source_ref,omitempty" gorm:"not null;default:''"`
	SourceBodyHash string `json:"-" gorm:"not null;default:''"`
	Identifier     string `json:"identifier,omitempty" gorm:"not null;default:''"`
	HasUpdate      bool   `json:"has_update" gorm:"-"`
	LatestEntryID  string `json:"latest_entry_id,omitempty" gorm:"-"`
	// Unique identifier of the entry
	ID string `json:"id"                 gorm:"type:varchar(36);primaryKey"`
	// Workspace ID
	SpaceID uint64 `json:"space_id"`
	// ID of the library
	LibraryID string `json:"library_id"`
	// Type of the entry
	Type string `json:"type"`
	// Title of the entry
	Title string `json:"title"`
	// Description of the entry
	Description          string        `json:"description"`
	CannotBeFact         bool          `json:"cannot_be_fact,omitempty" gorm:"-"`
	SourceUnverified     bool          `json:"source_unverified" gorm:"not null;default:false"`
	ReviewKind           string        `json:"review_kind,omitempty" gorm:"-"`
	SourceKind           string        `json:"source_kind" gorm:"type:varchar(16);not null;default:'article'"`
	VaultPath            string        `json:"vault_path" gorm:"type:text;not null;default:''"`
	VaultBody            string        `json:"-" gorm:"type:text;not null;default:''"`
	Starred              bool          `json:"starred" gorm:"not null;default:false"`
	CollectReason        string        `json:"collect_reason" gorm:"type:text;not null;default:''"`
	AgentSummary         string        `json:"agent_summary" gorm:"type:text;not null;default:''"`
	AgentTags            JSON          `json:"agent_tags" gorm:"type:jsonb;not null"`
	SourceName           string        `json:"source_name" gorm:"type:text;not null;default:''"`
	Acquisition          string        `json:"acquisition" gorm:"type:varchar(16);not null;default:''"`
	IngestAPIKeyID       uint64        `json:"ingest_api_key_id" gorm:"not null;default:0"`
	IngestAPIKeyName     string        `json:"ingest_api_key_name" gorm:"type:text;not null;default:''"`
	LastIngestAPIKeyID   uint64        `json:"last_ingest_api_key_id" gorm:"not null;default:0"`
	LastIngestAPIKeyName string        `json:"last_ingest_api_key_name" gorm:"type:text;not null;default:''"`
	RelatedEntryID       string        `json:"related_entry_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	StructuredSummary    *EntrySummary `json:"structured_summary" gorm:"type:jsonb"`
	EntitiesManual       bool          `json:"entities_manual" gorm:"not null;default:false"`
	Entities             []Entity      `json:"entities" gorm:"-"`
	// DescriptionSpecified distinguishes an explicitly supplied empty description
	// from an omitted field in partial update requests.
	DescriptionSpecified bool `json:"-" gorm:"-"`
	// Source of the entry (e.g. URL address for url type, "manual" for manual type)
	Source string `json:"source"             gorm:"type:varchar(2048)"`
	// Channel indicates through which channel the entry was ingested (web, api, rss)
	Channel string `json:"channel"            gorm:"type:varchar(50);default:'web'"`
	// Parse status of the entry
	ParseStatus string `json:"parse_status"`
	// PendingSubtasksCount is the outstanding enrichment subtask count
	// (summary + question + context chunks). Only meaningful while
	// ParseStatus == "finalizing"; defaults to 0 in any terminal state.
	PendingSubtasksCount int `json:"pending_subtasks_count" gorm:"type:int;not null;default:0"`
	// Summary status for async summary generation
	SummaryStatus string `json:"summary_status"     gorm:"type:varchar(32);default:none"`
	// Enable status of the entry
	EnableStatus string `json:"enable_status"`
	// ID of the embedding model
	EmbeddingModelID string `json:"embedding_model_id"`
	// File name of the entry
	FileName string `json:"file_name"`
	// File type of the entry
	FileType string `json:"file_type"`
	// File size of the entry
	FileSize int64 `json:"file_size"`
	// File hash of the entry
	FileHash         string              `json:"file_hash"`
	NormalizedURL    string              `json:"-" gorm:"type:text;not null;default:''"`
	ContentHash      string              `json:"-" gorm:"type:char(64);not null;default:''"`
	ContentSimHash   int64               `json:"-" gorm:"not null;default:0"`
	ContentRuneCount int                 `json:"-" gorm:"not null;default:0"`
	RepostSources    []EntryRepostSource `json:"repost_sources" gorm:"-"`
	// DuplicateMatchMethod describes this intake attempt, not stored provenance.
	DuplicateMatchMethod string `json:"-" gorm:"-"`
	// File path of the entry
	FilePath string `json:"file_path"`
	// Storage size of the entry
	StorageSize int64 `json:"storage_size"`
	// Metadata of the entry
	Metadata JSON `json:"metadata"           gorm:"type:json"`
	// CustomMetadata is user-authored descriptive metadata. It is deliberately
	// separate from Metadata, which contains internal ingestion state and IDs.
	CustomMetadata JSON `json:"custom_metadata" gorm:"type:json;not null"`
	// Creation time of the entry
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the entry
	UpdatedAt time.Time `json:"updated_at"`
	// Processed time of the entry
	ProcessedAt *time.Time `json:"processed_at"`
	// Error message of the entry
	ErrorMessage string `json:"error_message"`
	// Deletion time of the entry
	DeletedAt gorm.DeletedAt `json:"deleted_at"         gorm:"index"`
	// Library name (not stored in database, populated on query)
	LibraryName string `json:"library_name" gorm:"-"`
}

// CustomMetadataText returns stable human-readable metadata for summaries and
// document-scoped model context. Internal ingestion metadata is intentionally
// excluded.
func (k *Entry) CustomMetadataText() string {
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
func (k *Entry) GetMetadata() map[string]string {
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

// BeforeCreate initializes required defaults for new Entry entities.
func (k *Entry) BeforeCreate(tx *gorm.DB) (err error) {
	if k.ID == "" {
		k.ID = uuid.New().String()
	}
	// JSON.Value returns SQL NULL for an empty value. PostgreSQL's column
	// default is not applied when GORM explicitly inserts that NULL, so keep the
	// application-side representation aligned with the NOT NULL database
	// invariant for every entry creation path.
	if len(k.CustomMetadata) == 0 {
		k.CustomMetadata = JSON(`{}`)
	}
	if len(k.AgentTags) == 0 {
		k.AgentTags = JSON(`[]`)
	}

	return nil
}

type ManualEntryMetadata struct {
	Content   string `json:"content"`
	Format    string `json:"format"`
	Status    string `json:"status"`
	Version   int    `json:"version"`
	UpdatedAt string `json:"updated_at"`
}

// SourceInfo is supplied or extracted source evidence. Values remain strings so
// a publisher's original date precision is never invented by this service.
type SourceInfo struct {
	Author            string `json:"author,omitempty"`
	Publisher         string `json:"publisher,omitempty"`
	OriginalURL       string `json:"original_url,omitempty"`
	PublishedAt       string `json:"published_at,omitempty"`
	OriginalURLSource string `json:"original_url_source,omitempty"`
	Confirmed         bool   `json:"-"`
}

// EntryIntakeMetadata records the single-message intake decision. Original
// bodies stay in object storage; this only contains the audit/display fields.

type EntryIntakeMetadata struct {
	ModelCalls   []ModelCallCount `json:"model_calls,omitempty"`
	Source       string           `json:"source"`
	OriginalPath string           `json:"original_path,omitempty"`
	Images       []string         `json:"images,omitempty"`
	Reason       string           `json:"reason,omitempty"`
	Decision     string           `json:"decision"`
	UserRejected bool             `json:"user_rejected,omitempty"`
	SourceInfo   SourceInfo       `json:"source_info,omitempty"`
}

func (k *Entry) IntakeMetadata() (*EntryIntakeMetadata, error) {
	if k == nil || len(k.Metadata) == 0 {
		return nil, nil
	}
	var raw struct {
		Intake *EntryIntakeMetadata `json:"intake"`
	}
	if err := json.Unmarshal(k.Metadata, &raw); err != nil {
		return nil, err
	}
	return raw.Intake, nil
}

func (k *Entry) SetIntakeMetadata(intake *EntryIntakeMetadata) error {
	var raw map[string]json.RawMessage
	if len(k.Metadata) > 0 && json.Unmarshal(k.Metadata, &raw) != nil {
		return fmt.Errorf("invalid entry metadata")
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

// SearchScope keeps space and library boundaries explicit for repository queries.
type SearchScope struct {
	SpaceID   uint64
	LibraryID string
}

// NewManualEntryMetadata creates a new ManualEntryMetadata instance.
func NewManualEntryMetadata(content, status string, version int) *ManualEntryMetadata {
	if version <= 0 {
		version = 1
	}
	return &ManualEntryMetadata{
		Content:   content,
		Format:    ManualEntryFormatMarkdown,
		Status:    status,
		Version:   version,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// ToJSON converts the metadata to JSON type.
func (m *ManualEntryMetadata) ToJSON() (JSON, error) {
	if m == nil {
		return nil, nil
	}
	if m.Format == "" {
		m.Format = ManualEntryFormatMarkdown
	}
	if m.Status == "" {
		m.Status = ManualEntryStatusDraft
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

// ManualMetadata parses and returns manual entry metadata.
func (k *Entry) ManualMetadata() (*ManualEntryMetadata, error) {
	if len(k.Metadata) == 0 {
		return nil, nil
	}
	var metadata ManualEntryMetadata
	if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
		return nil, err
	}
	if metadata.Format == "" {
		metadata.Format = ManualEntryFormatMarkdown
	}
	if metadata.Version <= 0 {
		metadata.Version = 1
	}
	return &metadata, nil
}

// SetManualMetadata sets manual entry metadata onto the entry instance.
func (k *Entry) SetManualMetadata(meta *ManualEntryMetadata) error {
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

// IsManual returns true if the entry item is manual Markdown entry.
func (k *Entry) IsManual() bool {
	return k != nil && k.Type == EntryTypeManual
}

// EnsureManualDefaults sets default values for manual entry entries.
func (k *Entry) EnsureManualDefaults() {
	if k == nil {
		return
	}
	if k.Type == "" {
		k.Type = EntryTypeManual
	}
	if k.FileType == "" {
		k.FileType = EntryTypeManual
	}
	if k.Source == "" {
		k.Source = EntryTypeManual
	}
	if k.Channel == "" {
		k.Channel = ChannelWeb
	}
}

const metadataKeyProcessOverrides = "process_overrides"

// ProcessOverrides parses process config overrides from entry metadata.
func (k *Entry) ProcessOverrides() (*EntryProcessOverrides, error) {
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
	var overrides EntryProcessOverrides
	if err := json.Unmarshal(bytes, &overrides); err != nil {
		return nil, err
	}
	return &overrides, nil
}

// SetProcessOverrides merges process config overrides into entry metadata.
func (k *Entry) SetProcessOverrides(o *EntryProcessOverrides) error {
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

// EntryCheckParams defines parameters used to check if entry already exists.
type EntryCheckParams struct {
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
	// Entry type
	Type string
}
