package types

import (
	"errors"
	"time"
)

var (
	// ErrInvalidRuntimeTaskCursor means the client supplied a cursor that was
	// malformed or belonged to another queue/state view.
	ErrInvalidRuntimeTaskCursor = errors.New("invalid runtime task cursor")
	// ErrExpiredRuntimeTaskCursor means every task anchor retained in the
	// cursor has left the selected live state. The caller should refresh from
	// the first page rather than falling back to offset pagination.
	ErrExpiredRuntimeTaskCursor = errors.New("expired runtime task cursor")
)

// RuntimeTaskState is the stable operator-facing task lifecycle. It mirrors
// the durable task states while keeping the HTTP API independent
// from the queue library's Go enum.
type RuntimeTaskState string

const (
	RuntimeTaskCancelled RuntimeTaskState = "cancelled"
	RuntimeTaskPending   RuntimeTaskState = "pending"
	RuntimeTaskActive    RuntimeTaskState = "active"
	RuntimeTaskScheduled RuntimeTaskState = "scheduled"
	RuntimeTaskRetry     RuntimeTaskState = "retry"
	RuntimeTaskArchived  RuntimeTaskState = "archived"
	RuntimeTaskCompleted RuntimeTaskState = "completed"
)

func (s RuntimeTaskState) Valid() bool {
	switch s {
	case RuntimeTaskCancelled, RuntimeTaskPending, RuntimeTaskActive, RuntimeTaskScheduled,
		RuntimeTaskRetry, RuntimeTaskArchived, RuntimeTaskCompleted:
		return true
	default:
		return false
	}
}

// RuntimeTaskAction is returned by the backend for every task. The frontend
// renders only these actions instead of inferring safety from the state.
type RuntimeTaskAction string

const (
	RuntimeTaskActionCancel RuntimeTaskAction = "cancel"
	RuntimeTaskActionRunNow RuntimeTaskAction = "run_now"
	RuntimeTaskActionDelete RuntimeTaskAction = "delete"
)

// RuntimeTaskInfo is the safe SystemAdmin projection of one queue task.
// Raw payloads and results are deliberately excluded because they may contain
// document content, signed object URLs, or connector credentials.
type RuntimeTaskInfo struct {
	BatchID                       string              `json:"batch_id,omitempty"`
	ID                            string              `json:"id"`
	Queue                         string              `json:"queue"`
	Type                          string              `json:"type"`
	State                         RuntimeTaskState    `json:"state"`
	AllowedActions                []RuntimeTaskAction `json:"allowed_actions"`
	CancellationUnavailableReason string              `json:"cancellation_unavailable_reason,omitempty"`
	LastError                     string              `json:"last_error,omitempty"`
	LastFailedAt                  *time.Time          `json:"last_failed_at,omitempty"`
	NextProcessAt                 *time.Time          `json:"next_process_at,omitempty"`
	StartedAt                     *time.Time          `json:"started_at,omitempty"`
	CompletedAt                   *time.Time          `json:"completed_at,omitempty"`
	Deadline                      *time.Time          `json:"deadline,omitempty"`
	EnqueuedAt                    *time.Time          `json:"enqueued_at,omitempty"`
	Retried                       int                 `json:"retried"`
	MaxRetry                      int                 `json:"max_retry"`
	IsOrphaned                    bool                `json:"is_orphaned,omitempty"`
	Worker                        string              `json:"worker,omitempty"`
	SpaceID                       uint64              `json:"space_id,omitempty"`
	LibraryID                     string              `json:"library_id,omitempty"`
	EntryID                       string              `json:"entry_id,omitempty"`
	TaskID                        string              `json:"task_id,omitempty"`
	EntryCount                    int                 `json:"entry_count,omitempty"`
}

// RuntimeTaskPage is one stable slice of the operator task list. NextCursor
// is opaque to HTTP clients and remains tied to the selected queue/state.
type RuntimeTaskPage struct {
	Tasks      []RuntimeTaskInfo
	NextCursor string
	HasMore    bool
}

func (t RuntimeTaskInfo) Allows(action RuntimeTaskAction) bool {
	for _, allowed := range t.AllowedActions {
		if allowed == action {
			return true
		}
	}
	return false
}

// QueueConnectionStatus remains readable when PostgreSQL is offline.
type QueueConnectionStatus struct {
	State             string     `json:"state"`
	LastError         string     `json:"last_error,omitempty"`
	Since             *time.Time `json:"since,omitempty"`
	NextRetryAt       *time.Time `json:"next_retry_at,omitempty"`
	Alert             bool       `json:"alert"`
	AlertAfterSeconds int64      `json:"alert_after_seconds"`
}
