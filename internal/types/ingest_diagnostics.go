package types

import (
	"context"
	"time"
)

type privateIngestContentKey struct{}

func WithPrivateIngestContent(ctx context.Context) context.Context {
	return context.WithValue(ctx, privateIngestContentKey{}, true)
}
func PrivateIngestContent(ctx context.Context) bool {
	v, _ := ctx.Value(privateIngestContentKey{}).(bool)
	return v
}

type IngestDiagnosticReference struct {
	FailureID     string `json:"failure_id"`
	PackageStatus string `json:"package_status"`
	Recorded      bool   `json:"recorded"`
}

type FailureMaterial struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Source string `json:"source,omitempty"`
}

type IngestFailure struct {
	ID                string            `json:"id"`
	SpaceID           uint64            `json:"space_id"`
	RequestID         string            `json:"request_id"`
	CreatedAt         time.Time         `json:"created_at"`
	ExpiresAt         time.Time         `json:"expires_at"`
	Transport         string            `json:"transport"`
	APIKeyID          string            `json:"api_key_id,omitempty"`
	APIKeyName        string            `json:"api_key_name,omitempty"`
	LibraryID         string            `json:"library_id,omitempty"`
	Library           string            `json:"library,omitempty"`
	ItemIndex         int               `json:"item_index"`
	EntryID           string            `json:"entry_id,omitempty"`
	Stage             string            `json:"stage"`
	ErrorCode         string            `json:"error_code"`
	Reason            string            `json:"reason"`
	Retryable         bool              `json:"retryable"`
	Outcome           string            `json:"outcome"`
	PackageStatus     string            `json:"package_status"`
	Missing           []string          `json:"missing"`
	Materials         []FailureMaterial `json:"materials"`
	Version           string            `json:"version,omitempty"`
	Config            map[string]any    `json:"config,omitempty"`
	RecoveryEntryID   string            `json:"recovery_entry_id,omitempty"`
	RecoveryRequestID string            `json:"recovery_request_id,omitempty"`
	RecoveryFailureID string            `json:"recovery_failure_id,omitempty"`
}

type FailureQuery struct {
	Page      int        `json:"page,omitempty"`
	PageSize  int        `json:"page_size,omitempty"`
	From      *time.Time `json:"from,omitempty"`
	To        *time.Time `json:"to,omitempty"`
	APIKeyID  string     `json:"api_key_id,omitempty"`
	LibraryID string     `json:"library_id,omitempty"`
	ErrorCode string     `json:"error_code,omitempty"`
	RequestID string     `json:"request_id,omitempty"`
	ID        string     `json:"id,omitempty"`
}

type FailurePage struct {
	Items    []IngestFailure `json:"items"`
	Total    int             `json:"total"`
	Page     int             `json:"page"`
	PageSize int             `json:"page_size"`
}

type FailureMaterialRead struct {
	Data        []byte          `json:"-"` // HTTP serves image bytes; MCP receives only the authenticated URL.
	Material    FailureMaterial `json:"material"`
	Content     string          `json:"content,omitempty"`
	URL         string          `json:"url,omitempty"`
	Offset      int             `json:"offset"`
	Length      int             `json:"length"`
	TotalLength int             `json:"total_length"`
	NextOffset  *int            `json:"next_offset,omitempty"`
}
