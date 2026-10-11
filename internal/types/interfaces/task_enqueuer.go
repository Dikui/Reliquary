package interfaces

import (
	"context"
	"gorm.io/gorm"
	"reliquary/internal/queue"
)

// TaskEnqueuer abstracts task enqueueing for producers and their fixtures.
type TaskEnqueuer interface {
	Enqueue(task *queue.Task, opts ...queue.Option) (*queue.TaskInfo, error)
}

// TransactionalTaskEnqueuer inserts on the caller's PostgreSQL transaction.
type TransactionalTaskEnqueuer interface {
	TaskEnqueuer
	EnqueueTx(ctx context.Context, tx *gorm.DB, task *queue.Task, opts ...queue.Option) (*queue.TaskInfo, error)
}
