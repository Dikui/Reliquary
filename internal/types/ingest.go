package types

import "time"

const (
	MaxIngestBatchItems         = 50
	MaxIngestCheckURLs          = 100
	MaxIngestInlineAssetBytes   = 10 * 1024 * 1024
	MaxIngestInlineRequestBytes = 20 * 1024 * 1024
)

// IngestRequest is sourced content collected deliberately by an external agent.
// Agent annotations are stored separately from article metadata and processing input.
type IngestRequest struct {
	Kind            string             `json:"kind,omitempty"`
	DocumentID      string             `json:"document_id,omitempty"`
	URL             string             `json:"url"`
	Content         string             `json:"content"`
	Acquisition     string             `json:"acquisition"`
	ContentTypeHint string             `json:"content_type_hint,omitempty"`
	Missing         []IngestMissing    `json:"missing,omitempty"`
	InputError      string             `json:"-"`
	Title           string             `json:"title,omitempty"`
	Author          string             `json:"author,omitempty"`
	Publisher       string             `json:"publisher,omitempty"`
	PublishedAt     string             `json:"published_at,omitempty"`
	Images          []IngestImage      `json:"images,omitempty"`
	Thread          []IngestThreadItem `json:"thread,omitempty"`
	Quotes          []IngestQuote      `json:"quotes,omitempty"`
	CollectReason   string             `json:"collect_reason,omitempty"`
	AgentSummary    string             `json:"agent_summary,omitempty"`
	AgentTags       []string           `json:"agent_tags,omitempty"`
	SourceName      string             `json:"source_name,omitempty"`
}

type IngestImage struct {
	AssetID   string `json:"asset_id,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
	URL       string `json:"url,omitempty"`
	Data      string `json:"data,omitempty"`
	Caption   string `json:"caption,omitempty"`
	Text      string `json:"text,omitempty"`
}

type IngestMissing struct {
	Type        string    `json:"type"`
	SourceURL   string    `json:"source_url"`
	Reason      string    `json:"reason"`
	APIKeyID    uint64    `json:"api_key_id,omitempty"`
	APIKeyName  string    `json:"api_key_name,omitempty"`
	SubmittedAt time.Time `json:"submitted_at,omitempty"`
}

type IngestThreadItem struct {
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Content     string `json:"content"`
}

type IngestQuote struct {
	Content string `json:"content"`
	Author  string `json:"author,omitempty"`
	URL     string `json:"url,omitempty"`
}

type IngestImageFailure struct {
	Index  int    `json:"index"`
	URL    string `json:"url,omitempty"`
	Reason string `json:"reason"`
}

type IngestResult struct {
	Index         int                  `json:"index"`
	EntryID       string               `json:"article_id"`
	Status        string               `json:"status"`
	Message       string               `json:"message"`
	Reason        string               `json:"reason"`
	ImageFailures []IngestImageFailure `json:"image_failures"`
	Completeness  []IngestMissing      `json:"completeness"`
}

type IngestBatchRequest struct {
	Items []IngestRequest `json:"items"`
}
type IngestBatchResult struct {
	Items []IngestResult `json:"items"`
}
type IngestCheckRequest struct {
	URLs []string `json:"urls"`
}
type IngestCheckResult struct {
	URL        string `json:"url"`
	Status     string `json:"status"`
	EntryID    string `json:"article_id"`
	Incomplete bool   `json:"incomplete"`
}
type IngestCheckResponse struct {
	Items []IngestCheckResult `json:"items"`
}
type IncompleteArticle struct {
	Kind                        string          `json:"kind"`
	EntryID                     string          `json:"article_id"`
	URL                         string          `json:"url"`
	Title                       string          `json:"title"`
	Acquisition                 string          `json:"acquisition"`
	Missing                     []IngestMissing `json:"missing"`
	ExternalCompletionAttempted bool            `json:"external_completion_attempted"`
}
type IncompleteListResult struct {
	Items    []IncompleteArticle `json:"items"`
	Total    int64               `json:"total"`
	Page     int                 `json:"page"`
	PageSize int                 `json:"page_size"`
}
type IngestAssetResult struct {
	AssetID string `json:"asset_id"`
	Type    string `json:"type"`
	Size    int64  `json:"size"`
	Name    string `json:"name,omitempty"`
}
type IngestAsset struct {
	ID           string     `json:"asset_id" gorm:"type:varchar(36);primaryKey"`
	SpaceID      uint64     `json:"space_id"`
	Name         string     `json:"name"`
	Type         string     `json:"type" gorm:"column:file_type"`
	Size         int64      `json:"size"`
	Path         string     `json:"-"`
	CreatedAt    time.Time  `json:"created_at"`
	ReferencedAt *time.Time `json:"referenced_at,omitempty"`
	EntryID      string     `json:"article_id,omitempty"`
}

func (IngestAsset) TableName() string { return "ingest_assets" }

type IngestDiscard struct {
	SpaceID       uint64    `json:"space_id" gorm:"primaryKey"`
	NormalizedURL string    `json:"normalized_url" gorm:"primaryKey"`
	EntryID       string    `json:"article_id" gorm:"column:article_id"`
	DiscardedAt   time.Time `json:"discarded_at"`
	Reason        string    `json:"reason"`
}

func (IngestDiscard) TableName() string { return "ingest_discards" }
