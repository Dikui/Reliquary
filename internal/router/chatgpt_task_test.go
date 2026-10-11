package router

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func chatGPTQueueConfig() queue.Config {
	return queue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, CheckInterval: 5 * time.Millisecond, RetryDelayFunc: func(int, error, *queue.Task) time.Duration { return time.Hour }}
}

func TestChatGPTPauseRetriesWithoutConsumingBudgetIncludingFinalAttempt(t *testing.T) {
	for _, budget := range []int{0, 2} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			db := pgtest.Open(t)
			client := queue.NewClient(db)
			pause := &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(5 * time.Minute)}
			handler := chatGPTPermanentFailureMiddleware()(queue.HandlerFunc(func(context.Context, *queue.Task) error { return fmt.Errorf("processing: %w", pause) }))
			require.NoError(t, client.Start(context.Background(), handler, chatGPTQueueConfig()))
			t.Cleanup(func() { require.NoError(t, client.Close()) })
			payload := []byte(`{"knowledge_id":"fixture"}`)
			headers := map[string]string{"fixture": "preserve-13", "trace": "trace-fixture"}
			deadline := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
			original, err := client.Enqueue(queue.NewTaskWithHeaders(types.TypeDocumentProcess, payload, headers), queue.MaxRetry(budget), queue.Timeout(2*time.Hour), queue.Deadline(deadline), queue.Retention(time.Hour), queue.Group("batch"))
			require.NoError(t, err)
			var info *queue.TaskInfo
			require.Eventually(t, func() bool {
				info, err = client.GetTaskInfo(context.Background(), types.QueueDefault, original.ID)
				return err == nil && info.State == queue.TaskStateScheduled
			}, 5*time.Second, 10*time.Millisecond)
			require.Zero(t, info.Retried)
			require.Equal(t, budget, info.MaxRetry)
			require.Greater(t, info.NextProcessAt.Sub(time.Now()), 4*time.Minute)
			require.Equal(t, original.ID, info.ID)
			require.Equal(t, original.Timeout, info.Timeout)
			require.Equal(t, original.Deadline, info.Deadline)
			require.Equal(t, original.Retention, info.Retention)
			require.Equal(t, original.Group, info.Group)
			require.JSONEq(t, string(payload), string(info.Payload))
			require.Equal(t, headers, info.Headers)
			var count int64
			require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&count).Error)
			require.Zero(t, count)
			t.Logf("deferred budget=%d same_job=true timeout=%s deadline=%s retention=%s group=%q payload_preserved=true headers_preserved=true retried=%d", budget, info.Timeout, info.Deadline.Format(time.RFC3339), info.Retention, info.Group, info.Retried)
		})
	}
}

func TestChatGPTPermanentFailureArchivesFirstAttemptAndRecordsDeadLetter(t *testing.T) {
	db := pgtest.Open(t)
	repo := repository.NewTaskDeadLetterRepository(db)
	client := queue.NewClient(db)
	var calls, callbacks atomic.Int32
	upstream := &types.ChatGPTError{Kind: "failed", Status: 403, Code: "subscription_sharing_user_not_eligible", Param: "model", RequestID: "req_permanent", Body: `{"error":{"code":"subscription_sharing_user_not_eligible"}}`, Message: "subscription restriction"}
	callbackErrors := make(chan error, 1)
	config := chatGPTQueueConfig()
	config.OnDeadLetter = func(_ context.Context, _ *queue.Task, err error) { callbacks.Add(1); callbackErrors <- err }
	handler := chatGPTPermanentFailureMiddleware()(queue.HandlerFunc(func(context.Context, *queue.Task) error { calls.Add(1); return fmt.Errorf("processing: %w", upstream) }))
	require.NoError(t, client.Start(context.Background(), handler, config))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	original, err := client.Enqueue(queue.NewTask(types.TypeDocumentProcess, []byte(`{"knowledge_id":"permanent"}`)), queue.MaxRetry(3))
	require.NoError(t, err)
	var info *queue.TaskInfo
	require.Eventually(t, func() bool {
		info, err = client.GetTaskInfo(context.Background(), types.QueueDefault, original.ID)
		return err == nil && info.State == queue.TaskStateArchived && callbacks.Load() == 1
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 3, info.MaxRetry)
	require.Zero(t, info.Retried)
	require.Contains(t, info.LastErr, upstream.Code)
	require.Contains(t, info.LastErr, upstream.RequestID)
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, callbacks.Load())
	var diagnostic *types.ChatGPTError
	require.True(t, errors.As(<-callbackErrors, &diagnostic))
	require.Same(t, upstream, diagnostic)
	rows, _, err := repo.ListByTaskType(context.Background(), types.TypeDocumentProcess, "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].FailCount)
	require.Equal(t, "permanent", rows[0].RelatedID)
	require.Contains(t, rows[0].LastError, upstream.Code)
	require.Contains(t, rows[0].LastError, upstream.RequestID)
	got := chatGPTPermanentFailureMiddleware()(queue.HandlerFunc(func(context.Context, *queue.Task) error { return upstream })).ProcessTask(context.Background(), queue.NewTask("fixture:permanent", nil))
	require.ErrorIs(t, got, queue.SkipRetry)
	require.ErrorAs(t, got, &diagnostic)
	require.Same(t, upstream, diagnostic)
	t.Logf("PostgreSQL archived=1 dead_letters=%d fail_count=%d retried=%d handler_calls=%d callbacks=%d", len(rows), rows[0].FailCount, info.Retried, calls.Load(), callbacks.Load())
}

func TestChatGPTOrdinaryFailureRetainsRetryBudgetWithoutDeadLetter(t *testing.T) {
	db := pgtest.Open(t)
	repo := repository.NewTaskDeadLetterRepository(db)
	client := queue.NewClient(db)
	var calls, callbacks atomic.Int32
	config := chatGPTQueueConfig()
	config.OnDeadLetter = func(context.Context, *queue.Task, error) { callbacks.Add(1) }
	handler := chatGPTPermanentFailureMiddleware()(queue.HandlerFunc(func(context.Context, *queue.Task) error {
		calls.Add(1)
		return &types.ChatGPTError{Kind: "http", Status: 503, Code: "subscription_sharing_user_unavailable", Message: "temporarily unavailable"}
	}))
	require.NoError(t, client.Start(context.Background(), handler, config))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	original, err := client.Enqueue(queue.NewTask(types.TypeDocumentProcess, []byte(`{"knowledge_id":"retry"}`)), queue.MaxRetry(3))
	require.NoError(t, err)
	var info *queue.TaskInfo
	require.Eventually(t, func() bool {
		info, err = client.GetTaskInfo(context.Background(), types.QueueDefault, original.ID)
		return err == nil && info.State == queue.TaskStateRetry
	}, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, 1, info.Retried)
	require.Equal(t, 3, info.MaxRetry)
	require.EqualValues(t, 1, calls.Load())
	require.Zero(t, callbacks.Load())
	rows, _, err := repo.ListByTaskType(context.Background(), types.TypeDocumentProcess, "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	t.Logf("PostgreSQL retry=1 dead_letters=%d retried=%d max_retry=%d handler_calls=%d callbacks=%d", len(rows), info.Retried, info.MaxRetry, calls.Load(), callbacks.Load())
}
