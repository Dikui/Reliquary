package types

import "time"

// Span kinds — kept narrow because every kind has dedicated rendering on
// the frontend timeline:
//
//   - SpanKindRoot     — the per-(entry, attempt) trace root. Always
//     the parent_span_id ancestor of every other span
//     in that attempt. UI uses it for total elapsed.
//   - SpanKindStage describes one of the four canonical stages.
//     UI renders these as the timeline segments.
//   - SpanKindSubSpan describes work inside a stage (e.g. processing.call).
//     UI shows them as collapsible children.
//   - SpanKindGeneration, a SubSpan that wraps a model call. Same UI
//     treatment as SubSpan but tagged to identify model calls.
const (
	SpanKindRoot       = "root"
	SpanKindStage      = "stage"
	SpanKindSubSpan    = "subspan"
	SpanKindGeneration = "generation"
)

// Span statuses. We deliberately distinguish "failed" (this span itself
// errored) from "cancelled" (an upstream span failed and we abandoned this
// one without running it) so the UI can render the cause differently —
// "you broke X, so we never ran Y" vs. "Y itself broke".
const (
	SpanStatusPending   = "pending"
	SpanStatusRunning   = "running"
	SpanStatusDone      = "done"
	SpanStatusFailed    = "failed"
	SpanStatusSkipped   = "skipped"   // intentionally not run (e.g. disabled embeddings)
	SpanStatusCancelled = "cancelled" // not run because an upstream span failed
)

// Stages follow parsing, the processing call, body chunking, and embeddings.
const (
	StageDocReader  = "docreader"
	StageProcessing = "processing"
	StageChunking   = "chunking"
	StageEmbedding  = "embedding"
)

var AllStages = []string{StageDocReader, StageProcessing, StageChunking, StageEmbedding}

var StageDependencies = map[string][]string{
	StageDocReader:  nil,
	StageProcessing: {StageDocReader},
	StageChunking:   {StageProcessing},
	StageEmbedding:  {StageChunking},
}

// EntryProcessingSpan is one row in entry_processing_spans.
//
// Field tags pull double duty: GORM (storage) and JSON (API). ErrorDetail
// is excluded by default — handlers must opt in for admin views, matching
// how the dead-letter middleware already protects raw stack traces.
type EntryProcessingSpan struct {
	ID           int64      `gorm:"primaryKey;column:id"             json:"-"`
	EntryID      string     `gorm:"column:entry_id"              json:"entry_id"`
	Attempt      int        `gorm:"column:attempt"                   json:"attempt"`
	SpanID       string     `gorm:"column:span_id;size:64"           json:"span_id"`
	ParentSpanID string     `gorm:"column:parent_span_id;size:64"    json:"parent_span_id,omitempty"`
	Name         string     `gorm:"column:name;size:255"             json:"name"`
	Kind         string     `gorm:"column:kind;size:16"              json:"kind"`
	Status       string     `gorm:"column:status;size:16"            json:"status"`
	Input        JSONMap    `gorm:"column:input;type:jsonb"          json:"input,omitempty"`
	Output       JSONMap    `gorm:"column:output;type:jsonb"         json:"output,omitempty"`
	Metadata     JSONMap    `gorm:"column:metadata;type:jsonb"       json:"metadata,omitempty"`
	ErrorCode    string     `gorm:"column:error_code;size:64"        json:"error_code,omitempty"`
	ErrorMessage string     `gorm:"column:error_message;type:text"   json:"error_message,omitempty"`
	ErrorDetail  string     `gorm:"column:error_detail;type:text"    json:"-"`
	StartedAt    *time.Time `gorm:"column:started_at"                json:"started_at,omitempty"`
	FinishedAt   *time.Time `gorm:"column:finished_at"               json:"finished_at,omitempty"`
	DurationMs   int64      `gorm:"column:duration_ms"               json:"duration_ms,omitempty"`
	CreatedAt    time.Time  `gorm:"column:created_at;autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"column:updated_at;autoUpdateTime" json:"updated_at"`
}

// TableName pins the table because GORM's default pluralization
// ("entry_processing_spans") happens to match — explicit beats
// implicit.
func (EntryProcessingSpan) TableName() string {
	return "entry_processing_spans"
}

// SpanTreeNode is the API-only tree projection. The repo returns flat
// rows; the handler/tracker assembles SpanTreeNode for the response.
type SpanTreeNode struct {
	EntryProcessingSpan
	Children []*SpanTreeNode `json:"children,omitempty"`
}
