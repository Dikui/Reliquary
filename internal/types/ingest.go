package types

import (
	"encoding/json"
	"time"
)

const (
	MaxIngestBatchItems         = 50
	MaxIngestCheckURLs          = 100
	MaxIngestInlineAssetBytes   = 10 * 1024 * 1024
	MaxIngestInlineRequestBytes = 20 * 1024 * 1024
	MaxIngestContentRunes       = 5_000_000
	MaxIngestItemBytes          = 64 * 1024 * 1024
	MaxIngestBatchBytes         = 128 * 1024 * 1024
	MaxIngestMCPBytes           = MaxIngestBatchBytes + 1024*1024
	MaxIngestSourceRefRunes     = 2048
	MaxIngestIdentifierRunes    = 2048
)

// IngestRequest is sourced content collected deliberately by an external agent.
// Agent annotations are stored separately from article metadata and processing input.
type IngestRequest struct {
	RecoveryFailureID  string             `json:"recovery_failure_id,omitempty"`
	RawInput           json.RawMessage    `json:"-"`
	Library            string             `json:"library"`
	EntryID            string             `json:"entry_id,omitempty"`
	SourceRef          string             `json:"source_ref,omitempty"`
	Identifier         string             `json:"identifier,omitempty"`
	Graph              *bool              `json:"graph"`
	SourceRefProvided  bool               `json:"-"`
	IdentifierProvided bool               `json:"-"`
	URL                string             `json:"url"`
	Content            string             `json:"content"`
	Acquisition        string             `json:"acquisition"`
	ContentTypeHint    string             `json:"content_type_hint,omitempty"`
	Missing            []IngestMissing    `json:"missing,omitempty"`
	InputError         string             `json:"-"`
	Title              string             `json:"title,omitempty"`
	Author             string             `json:"author,omitempty"`
	Publisher          string             `json:"publisher,omitempty"`
	PublishedAt        string             `json:"published_at,omitempty"`
	Images             []IngestImage      `json:"images,omitempty"`
	Thread             []IngestThreadItem `json:"thread,omitempty"`
	Quotes             []IngestQuote      `json:"quotes,omitempty"`
	CollectReason      string             `json:"collect_reason,omitempty"`
	AgentSummary       string             `json:"agent_summary,omitempty"`
	AgentTags          []string           `json:"agent_tags,omitempty"`
	SourceName         string             `json:"source_name,omitempty"`
}

type EntryContentUpdate struct {
	Content      string        `json:"content"`
	BaseBodyHash string        `json:"base_body_hash,omitempty"`
	Acquisition  string        `json:"acquisition,omitempty"`
	Images       []IngestImage `json:"images,omitempty"`
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
	Diagnostics      *IngestDiagnosticReference `json:"diagnostics,omitempty"`
	LibraryName      string                     `json:"library_name"`
	SourceUnverified bool                       `json:"source_unverified"`
	LibraryID        string                     `json:"library_id"`
	SourceRef        string                     `json:"source_ref,omitempty"`
	Identifier       string                     `json:"identifier,omitempty"`
	Graph            *bool                      `json:"graph"`
	HasUpdate        bool                       `json:"has_update"`
	LatestEntryID    string                     `json:"latest_entry_id,omitempty"`
	Index            int                        `json:"index"`
	EntryID          string                     `json:"entry_id"`
	Status           string                     `json:"status"`
	Message          string                     `json:"message"`
	Reason           string                     `json:"reason"`
	ImageFailures    []IngestImageFailure       `json:"image_failures"`
	Completeness     []IngestMissing            `json:"completeness"`
}

type IngestBatchRequest struct {
	Items []IngestRequest `json:"items"`
}
type IngestBatchResult struct {
	Items []IngestResult `json:"items"`
}
type IngestCheckRequest struct {
	Library string   `json:"library"`
	URLs    []string `json:"urls"`
}
type IngestCheckResult struct {
	Reason     string `json:"reason,omitempty"`
	URL        string `json:"url"`
	Status     string `json:"status"`
	EntryID    string `json:"entry_id"`
	Incomplete bool   `json:"incomplete"`
}
type IngestCheckResponse struct {
	Items []IngestCheckResult `json:"items"`
}
type IncompleteArticle struct {
	LibraryName                 string          `json:"library_name"`
	SourceUnverified            bool            `json:"source_unverified"`
	LibraryTemplate             string          `json:"library_template"`
	LibraryID                   string          `json:"library_id"`
	EntryID                     string          `json:"entry_id"`
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
	EntryID      string     `json:"entry_id,omitempty"`
}

func (IngestAsset) TableName() string { return "ingest_assets" }

type IngestDiscard struct {
	SpaceID       uint64    `json:"space_id" gorm:"primaryKey"`
	LibraryID     string    `json:"library_id" gorm:"primaryKey"`
	Fingerprint   string    `json:"fingerprint" gorm:"primaryKey"`
	NormalizedURL string    `json:"normalized_url"`
	EntryID       string    `json:"entry_id" gorm:"column:entry_id"`
	DiscardedAt   time.Time `json:"discarded_at"`
	Reason        string    `json:"reason"`
}

func (IngestDiscard) TableName() string { return "ingest_discards" }
