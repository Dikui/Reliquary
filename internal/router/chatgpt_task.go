package router

import (
	"context"
	"errors"
	"github.com/hibiken/asynq"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"time"
)

func chatGPTFinalDeferralMiddleware(queue interfaces.TaskEnqueuer, inspectors ...*asynq.Inspector) asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			err := next.ProcessTask(ctx, t)
			if types.IsChatGPTPermanentError(err) {
				return errors.Join(err, asynq.SkipRetry)
			}
			retried, ok := asynq.GetRetryCount(ctx)
			maximum, maxOK := asynq.GetMaxRetry(ctx)
			if !types.IsChatGPTDeferred(err) || !ok || !maxOK || retried < maximum || queue == nil {
				return err
			}
			// Asynq checks exhaustion before IsFailure. Requeue a deferred final
			// attempt with zero ordinary retries, preserving its remaining budget.
			name, _ := asynq.GetQueueName(ctx)
			var paused *types.ChatGPTDeferredError
			errors.As(err, &paused)
			when := max(time.Now().Add(time.Minute).Unix(), paused.RetryAt.Unix())
			options := []asynq.Option{asynq.Queue(name), asynq.ProcessAt(time.Unix(when, 0)), asynq.MaxRetry(0)}
			taskID, idOK := asynq.GetTaskID(ctx)
			var info *asynq.TaskInfo
			infoErr := errors.New("task inspector or task id unavailable")
			if len(inspectors) > 0 && inspectors[0] != nil && idOK {
				info, infoErr = inspectors[0].GetTaskInfo(name, taskID)
			}
			if infoErr == nil && info != nil {
				options = append(options, asynq.Timeout(info.Timeout), asynq.Retention(info.Retention))
				if !info.Deadline.IsZero() {
					options = append(options, asynq.Deadline(info.Deadline))
				}
				if info.Group != "" {
					options = append(options, asynq.Group(info.Group))
				}
			} else {
				// A metadata outage must still defer the task. Reuse the remaining
				// execution timeout without inventing an unknown absolute deadline.
				if deadline, ok := ctx.Deadline(); ok {
					options = append(options, asynq.Timeout(max(time.Second, time.Until(deadline))))
				}
				logger.Warnf(ctx, "ChatGPT task deferral: metadata unavailable for queue=%s task_id=%s, preserving payload and headers with remaining timeout fallback: %v", name, taskID, infoErr)
			}
			_, enqueueErr := queue.Enqueue(asynq.NewTaskWithHeaders(t.Type(), t.Payload(), t.Headers()), options...)
			if enqueueErr != nil {
				return errors.Join(err, enqueueErr)
			}
			return asynq.RevokeTask
		})
	}
}
