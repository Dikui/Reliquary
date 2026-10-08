package interfaces

import (
	"context"
	"reliquary/internal/types"
)

// TaskDeadLetterRepository persists archived task failures for inspection.
type TaskDeadLetterRepository interface {
	// Insert records one dead letter. Best-effort caller: the asynq
	// middleware ignores the error so a failed insert never masks the
	// underlying task error.
	Insert(ctx context.Context, dl *types.TaskDeadLetter) error

	// ListByScope returns dead letters for the given scope tuple,
	// newest-first, paginated by failed-id cursor. `cursor` is the
	// stringified id of the oldest entry from the previous page; "" =
	// from the newest. Empty nextCursor = end of stream. `limit` is
	// clamped to [1, 200].
	ListByScope(ctx context.Context, scope, scopeID, cursor string, limit int) ([]*types.TaskDeadLetter, string, error)

	// ListByTaskType returns dead letters for the given task_type,
	// newest-first, with the same cursor semantics as ListByScope.
	ListByTaskType(ctx context.Context, taskType, cursor string, limit int) ([]*types.TaskDeadLetter, string, error)

	// DeleteByID drops a single dead letter (e.g. after operators have
	// requeued the task manually).
	DeleteByID(ctx context.Context, id int64) error
}
