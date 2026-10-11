package interfaces

import (
	"context"

	"reliquary/internal/queue"
)

// TaskHandler is a interface for handling asynchronous tasks
type TaskHandler interface {
	// Handle handles the task
	Handle(ctx context.Context, t *queue.Task) error
}
