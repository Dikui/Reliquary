package types

import "time"

const TypeEntryBatch = "entry:batch"

type EntryBatchFilters struct {
	Libraries       []string `json:"libraries,omitempty"`
	DomainIDs       []string `json:"domain_ids,omitempty"`
	EntityIDs       []string `json:"entity_ids,omitempty"`
	PublishedFrom   string   `json:"published_from,omitempty"`
	PublishedTo     string   `json:"published_to,omitempty"`
	IncludeRejected bool     `json:"include_rejected,omitempty"`
	Keyword         string   `json:"keyword,omitempty"`
	FileType        string   `json:"file_type,omitempty"`
	Starred         *bool    `json:"starred,omitempty"`
	ParseStatus     string   `json:"parse_status,omitempty"`
	Source          string   `json:"source,omitempty"`
	SourceKind      string   `json:"source_kind,omitempty"`
	StartTime       string   `json:"start_time,omitempty"`
	EndTime         string   `json:"end_time,omitempty"`
}
type EntryBatchSelection struct {
	IDs     []string           `json:"ids,omitempty"`
	Filters *EntryBatchFilters `json:"filters,omitempty"`
}
type EntryBatchRequest struct {
	Action             string              `json:"action"`
	Selection          EntryBatchSelection `json:"selection"`
	Starred            *bool               `json:"starred,omitempty"`
	PreviewFingerprint string              `json:"preview_fingerprint,omitempty"`
}
type EntryBatchReason struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}
type EntryBatchSample struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	LibraryID string `json:"library_id"`
}
type EntryBatchPreview struct {
	Fingerprint string             `json:"fingerprint"`
	Matched     int                `json:"matched"`
	Eligible    int                `json:"eligible"`
	Skipped     int                `json:"skipped"`
	Blocked     int                `json:"blocked"`
	Reasons     []EntryBatchReason `json:"reasons"`
	Samples     []EntryBatchSample `json:"samples"`
}
type EntryBatchItem struct {
	BatchID   string    `json:"-" gorm:"primaryKey"`
	EntryID   string    `json:"entry_id" gorm:"primaryKey"`
	SpaceID   uint64    `json:"-"`
	Title     string    `json:"title"`
	LibraryID string    `json:"library_id"`
	Status    string    `json:"status"`
	Message   string    `json:"message,omitempty"`
	Warning   string    `json:"warning,omitempty"`
	TaskID    string    `json:"task_id,omitempty"`
	Attempt   int       `json:"-"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

func (EntryBatchItem) TableName() string { return "entry_batch_items" }

type EntryBatchResult struct {
	BatchID   string           `json:"batch_id"`
	Action    string           `json:"action"`
	Status    string           `json:"status"`
	Total     int              `json:"total"`
	Succeeded int              `json:"succeeded"`
	Failed    int              `json:"failed"`
	Skipped   int              `json:"skipped"`
	Pending   int              `json:"pending"`
	Running   int              `json:"running"`
	Warnings  int              `json:"warnings"`
	Items     []EntryBatchItem `json:"items"`
	Page      int              `json:"page"`
	PageSize  int              `json:"page_size"`
}
type EntryBatchPayload struct {
	SpaceID   uint64            `json:"space_id"`
	BatchID   string            `json:"batch_id"`
	Request   EntryBatchRequest `json:"request"`
	Initiator TaskInitiator     `json:"initiator"`
}
