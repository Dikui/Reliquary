package router

import (
	"context"
	"errors"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

// Quota deferrals are handled transactionally by the queue without changing
// the job ID, execution options, or ordinary retry budget.
func chatGPTPermanentFailureMiddleware() queue.MiddlewareFunc {
	return func(next queue.Handler) queue.Handler {
		return queue.HandlerFunc(func(ctx context.Context, t *queue.Task) error {
			err := next.ProcessTask(ctx, t)
			if types.IsChatGPTPermanentError(err) {
				return errors.Join(err, queue.SkipRetry)
			}
			return err
		})
	}
}
