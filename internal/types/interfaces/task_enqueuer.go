package interfaces

import "github.com/hibiken/asynq"

// TaskEnqueuer abstracts task enqueueing. *asynq.Client satisfies this interface.
// Tests can provide lightweight task enqueuers through the same interface.
type TaskEnqueuer interface {
	Enqueue(task *asynq.Task, opts ...asynq.Option) (*asynq.TaskInfo, error)
}
