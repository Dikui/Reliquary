package types

import (
	"encoding/json"
	"time"
)

// TaskDeadLetter is one row in the `task_dead_letters` archive: a
// permanent record of a task whose retry budget was exhausted. The table
// is populated by the queue dead-letter middleware whenever a task exhausts
// its retry budget. Every registered task type shares this archive.
//
// Operators query by (Scope, ScopeID) — e.g. "all dead letters for Library
// abc", or by TaskType, e.g. "all document:process failures in the
// last 24h". The table has no TTL; rows are kept until manually pruned.
type TaskDeadLetter struct {
	// Auto-increment row id.
	ID int64 `json:"id" gorm:"primaryKey;autoIncrement"`
	// Space scope mirrored from the original task payload (best-effort:
	// if the middleware can't decode the payload, this falls back to 0).
	SpaceID uint64 `json:"space_id" gorm:"index"`
	// Task identifier (e.g. "document:process").
	TaskType string `json:"task_type" gorm:"type:varchar(64)"`
	// Logical task scope. Falls back to
	// "unknown" when the middleware doesn't recognize the task type's
	// payload.
	Scope string `json:"scope" gorm:"type:varchar(32)"`
	// Identifier within scope, e.g. libraryID for a library-scoped record.
	ScopeID string `json:"scope_id" gorm:"type:varchar(64)"`
	// Optional secondary identifier for grouping failures by source document.
	RelatedID string `json:"related_id" gorm:"type:varchar(64);default:''"`
	// Raw task payload at the time of failure. For queue tasks this is
	// the verbatim queue.Task.Payload(); for service-level dead letters
	// the consumer chooses what to record.
	Payload json.RawMessage `json:"payload" gorm:"type:jsonb"`
	// String form of the final error. Long stack traces are kept verbatim.
	LastError string `json:"last_error" gorm:"type:text;default:''"`
	// Total attempt count when the dead-letter record was written.
	FailCount int `json:"fail_count"`
	// Server-side write time.
	FailedAt time.Time `json:"failed_at"`
}

// TableName binds TaskDeadLetter to the `task_dead_letters` table.
func (TaskDeadLetter) TableName() string {
	return "task_dead_letters"
}

// Standard scope literals. Repositories accept any string, but consumers
// should prefer these so SQL queries (run from the ops console) don't
// have to guess at variations.
const (
	// Persisted discriminator stays stable so existing archives keep their scope.
	TaskScopeLibrary = "knowledge_base"
	TaskScopeEntry   = "knowledge"
	TaskScopeSpace   = "tenant"
	TaskScopeUnknown = "unknown"
)
