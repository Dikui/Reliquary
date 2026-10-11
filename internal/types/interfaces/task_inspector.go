package interfaces

import (
	"context"

	"reliquary/internal/types"
)

// TaskInspector abstracts queue inspection / cancellation against the
// task backend. Tests can provide a lightweight implementation.
// User cancellation removes queued article processing tasks and signals
// active workers to stop at their next checkpoint.
type TaskInspector interface {
	// CancelTasksForKnowledge removes pending/scheduled/retry tasks
	// whose payload references the given knowledge ID, and signals
	// active workers running such tasks to stop. Returns rough
	// counts of (deletedFromQueue, activeCancelled) for observability.
	// Errors are returned but callers should treat the operation as
	// best-effort: the row-level abort flag remains the source of
	// truth, this just prevents wasted work.
	CancelTasksForKnowledge(ctx context.Context, knowledgeID string) (deleted int, cancelled int, err error)

	// HasQueuedTasksForKnowledge reports whether any pending / scheduled
	// / retry / active task referencing the given knowledge ID still
	// lives in the queue backend. It is the read-only counterpart of
	// CancelTasksForKnowledge: the housekeeping sweep calls it before
	// flipping a long-idle "processing"/"finalizing" row to "failed" so
	// it can tell a genuinely orphaned row (no task anywhere) from one
	// whose enrichment subtasks are merely backlogged behind a busy
	// queue (no span heartbeat yet because no worker has picked them up).
	//
	// On backend error it returns (false, err); callers decide the fail-safe
	// direction. Inline task fixtures always return false because they do
	// not queue work, so their span and updated_at checks stay authoritative.
	HasQueuedTasksForKnowledge(ctx context.Context, knowledgeID string) (bool, error)

	// QueueStats returns a read-only depth snapshot for every queue this
	// application enqueues into, for the System Admin runtime dashboard.
	//
	// The `supported` bool reports whether queue inspection is available
	// on the current backend. Callers use false to render an unavailable
	// state rather than an empty table.
	//
	// Queues with no records appear as zeroed rows. Database errors are
	// returned so unavailable storage is distinguishable from empty queues.
	QueueStats(ctx context.Context) (stats []types.QueueStat, supported bool, err error)

	// WorkerServerStats returns live worker capacity and busy workers in
	// the running process for each configured pool.
	WorkerServerStats(ctx context.Context) (stats []types.WorkerServerStat, supported bool, err error)
}

// RuntimeTaskInspector is the optional operator surface implemented by queue
// backends that retain inspectable task state. It is separate from
// TaskInspector so in-memory fixtures and light-weight tests do not need to implement
// queue mutations.
type RuntimeTaskInspector interface {
	ListRuntimeTasks(
		ctx context.Context,
		queue string,
		state types.RuntimeTaskState,
		cursor string,
		pageSize int,
	) (page types.RuntimeTaskPage, supported bool, err error)
	GetRuntimeTask(ctx context.Context, queue, taskID string) (task *types.RuntimeTaskInfo, supported bool, err error)
	RunRuntimeTask(ctx context.Context, queue, taskID string) (supported bool, err error)
	DeleteRuntimeTask(ctx context.Context, queue, taskID string) (supported bool, err error)
	// ForceDeleteRuntimeTask removes a queue record without checking the
	// operator-facing AllowedActions. Used when the business row is already
	// gone but a retry/pending task survived (orphan cleanup).
	ForceDeleteRuntimeTask(ctx context.Context, queue, taskID string) (supported bool, err error)
	// PurgeArchivedRuntimeTasks removes team and legacy tenantless archived
	// (finally-failed) tasks from one queue and returns the deletion count.
	// Other tenants and invalid payloads remain available for diagnostics.
	// It only ever touches the archived (dead-letter) set, so live
	// pending/active/scheduled/retry tasks are never affected. Business state
	// is intentionally left untouched: archived tasks have already had their
	// document status flipped to "failed" on their last retry, mirroring the
	// semantics of deleting archived records one by one.
	PurgeArchivedRuntimeTasks(ctx context.Context, queue string) (deleted int, supported bool, err error)
}
