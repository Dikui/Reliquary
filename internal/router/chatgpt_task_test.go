package router

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/middleware/asynqdl"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestChatGPTPauseRetriesWithoutConsumingBudgetIncludingFinalAttempt(t *testing.T) {
	for _, budget := range []int{0, 2} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			redis := miniredis.RunT(t)
			opt := asynq.RedisClientOpt{Addr: redis.Addr()}
			client := asynq.NewClient(opt)
			defer client.Close()
			inspector := asynq.NewInspector(opt)
			defer inspector.Close()
			server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, TaskCheckInterval: 10 * time.Millisecond, RetryDelayFunc: chatGPTTaskRetryDelay, IsFailure: chatGPTTaskIsFailure})
			pause := &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(5 * time.Minute)}
			handler := chatGPTFinalDeferralMiddleware(client, inspector)(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return fmt.Errorf("processing: %w", pause) }))
			require.NoError(t, server.Start(handler))
			defer server.Shutdown()
			payload := []byte(`{"knowledge_id":"fixture"}`)
			headers := map[string]string{"fixture": "preserve-13", "trace": "trace-fixture"}
			deadline := time.Now().Add(24 * time.Hour).Truncate(time.Second)
			original, err := client.Enqueue(asynq.NewTaskWithHeaders("fixture:generation", payload, headers), asynq.MaxRetry(budget), asynq.Timeout(2*time.Hour), asynq.Deadline(deadline), asynq.Retention(time.Hour))
			require.NoError(t, err)
			require.Eventually(t, func() bool {
				info, err := inspector.GetQueueInfo("default")
				return err == nil && info.Retry+info.Scheduled == 1
			}, 10*time.Second, 25*time.Millisecond)
			info, err := inspector.GetQueueInfo("default")
			require.NoError(t, err)
			require.Zero(t, info.Archived)
			var tasks []*asynq.TaskInfo
			if budget == 0 {
				tasks, err = inspector.ListScheduledTasks("default")
			} else {
				tasks, err = inspector.ListRetryTasks("default")
			}
			require.NoError(t, err)
			require.Len(t, tasks, 1)
			require.Zero(t, tasks[0].Retried)
			require.Equal(t, budget, tasks[0].MaxRetry)
			require.Greater(t, tasks[0].NextProcessAt.Sub(time.Now()), 4*time.Minute)
			require.Equal(t, original.Timeout, tasks[0].Timeout)
			require.Equal(t, original.Deadline, tasks[0].Deadline)
			require.Equal(t, original.Retention, tasks[0].Retention)
			require.Equal(t, original.Group, tasks[0].Group)
			require.Equal(t, payload, tasks[0].Payload)
			require.Equal(t, headers, tasks[0].Headers)
			if budget == 0 {
				require.NotEqual(t, original.ID, tasks[0].ID)
			} else {
				require.Equal(t, original.ID, tasks[0].ID)
			}
			t.Logf("deferred budget=%d copied=%t timeout=%s deadline=%s retention=%s group=%q payload_preserved=true headers_preserved=true retried=%d", budget, original.ID != tasks[0].ID, tasks[0].Timeout, tasks[0].Deadline.Format(time.RFC3339), tasks[0].Retention, tasks[0].Group, tasks[0].Retried)
		})
	}
}

func TestChatGPTFinalDeferralMetadataFailureStillSchedulesWithTimeoutFallback(t *testing.T) {
	redis := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: redis.Addr()}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	emptyRedis := miniredis.RunT(t)
	unavailableInspector := asynq.NewInspector(asynq.RedisClientOpt{Addr: emptyRedis.Addr()})
	defer unavailableInspector.Close()
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, TaskCheckInterval: 10 * time.Millisecond, RetryDelayFunc: chatGPTTaskRetryDelay, IsFailure: chatGPTTaskIsFailure})
	pause := &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(5 * time.Minute)}
	handler := chatGPTFinalDeferralMiddleware(client, unavailableInspector)(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return pause }))
	require.NoError(t, server.Start(handler))
	defer server.Shutdown()
	payload := []byte(`{"knowledge_id":"fallback"}`)
	headers := map[string]string{"fixture": "fallback-13"}
	original, err := client.Enqueue(asynq.NewTaskWithHeaders("fixture:metadata-fallback", payload, headers), asynq.MaxRetry(0), asynq.Timeout(2*time.Hour))
	require.NoError(t, err)
	require.True(t, original.Deadline.IsZero())
	require.Eventually(t, func() bool {
		info, err := inspector.GetQueueInfo("default")
		return err == nil && info.Scheduled == 1
	}, 10*time.Second, 25*time.Millisecond)
	info, err := inspector.GetQueueInfo("default")
	require.NoError(t, err)
	require.Zero(t, info.Archived)
	tasks, err := inspector.ListScheduledTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.NotEqual(t, original.ID, tasks[0].ID)
	require.Zero(t, tasks[0].Retried)
	require.Zero(t, tasks[0].MaxRetry)
	require.Equal(t, payload, tasks[0].Payload)
	require.Equal(t, headers, tasks[0].Headers)
	require.Greater(t, tasks[0].Timeout, time.Hour+59*time.Minute)
	require.LessOrEqual(t, tasks[0].Timeout, 2*time.Hour)
	require.True(t, tasks[0].Deadline.IsZero(), "a relative timeout does not imply an absolute deadline")
	t.Logf("metadata failure fallback: scheduled=1 archived=0 retried=0 timeout=%s absolute_deadline_unknown=true payload_preserved=true headers_preserved=true", tasks[0].Timeout)
}

func TestChatGPTPermanentFailureArchivesFirstAttemptAndRecordsDeadLetter(t *testing.T) {
	db := pgtest.Open(t)
	repo := repository.NewTaskDeadLetterRepository(db)
	redis := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: redis.Addr()}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, TaskCheckInterval: 10 * time.Millisecond, RetryDelayFunc: chatGPTTaskRetryDelay, IsFailure: chatGPTTaskIsFailure})
	var calls, callbacks atomic.Int32
	upstream := &types.ChatGPTError{Kind: "failed", Status: 403, Code: "subscription_sharing_user_not_eligible", Param: "model", RequestID: "req_permanent", Body: `{"error":{"code":"subscription_sharing_user_not_eligible"}}`, Message: "subscription restriction"}
	callbackErrors := make(chan error, 1)
	mux := asynq.NewServeMux()
	mux.Use(chatGPTFinalDeferralMiddleware(client))
	mux.Use(asynqdl.MiddlewareWithCallback(repo, func(_ context.Context, _ *asynq.Task, err error) {
		callbacks.Add(1)
		select {
		case callbackErrors <- err:
		default:
		}
	}))
	mux.HandleFunc("fixture:permanent", func(context.Context, *asynq.Task) error {
		calls.Add(1)
		return fmt.Errorf("processing: %w", upstream)
	})
	require.NoError(t, server.Start(mux))
	defer server.Shutdown()
	_, err := client.Enqueue(asynq.NewTask("fixture:permanent", []byte(`{"knowledge_id":"permanent"}`)), asynq.MaxRetry(3))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := inspector.GetQueueInfo("default")
		return err == nil && info.Archived == 1
	}, 10*time.Second, 25*time.Millisecond)
	info, err := inspector.GetQueueInfo("default")
	require.NoError(t, err)
	require.Zero(t, info.Retry)
	require.Zero(t, info.Pending)
	tasks, err := inspector.ListArchivedTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, 3, tasks[0].MaxRetry)
	require.Zero(t, tasks[0].Retried)
	require.Contains(t, tasks[0].LastErr, upstream.Code)
	require.Contains(t, tasks[0].LastErr, upstream.RequestID)
	require.EqualValues(t, 1, calls.Load())
	require.EqualValues(t, 1, callbacks.Load())
	callbackErr := <-callbackErrors
	var diagnostic *types.ChatGPTError
	require.True(t, errors.As(callbackErr, &diagnostic))
	require.Same(t, upstream, diagnostic)
	rows, _, err := repo.ListByTaskType(context.Background(), "fixture:permanent", "", 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 1, rows[0].FailCount)
	require.Equal(t, "permanent", rows[0].RelatedID)
	require.Contains(t, rows[0].LastError, upstream.Code)
	require.Contains(t, rows[0].LastError, upstream.RequestID)
	t.Logf("PostgreSQL task_dead_letters: rows=%d task_type=%s related_id=%s fail_count=%d last_error=%q; Asynq archived=%d retried=%d max_retry=%d handler_calls=%d callbacks=%d", len(rows), rows[0].TaskType, rows[0].RelatedID, rows[0].FailCount, rows[0].LastError, info.Archived, tasks[0].Retried, tasks[0].MaxRetry, calls.Load(), callbacks.Load())

	// The returned marker preserves the original typed diagnostics for callers.
	handler := chatGPTFinalDeferralMiddleware(client)(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return upstream }))
	got := handler.ProcessTask(context.Background(), asynq.NewTask("fixture:permanent", nil))
	require.ErrorIs(t, got, asynq.SkipRetry)
	require.ErrorAs(t, got, &diagnostic)
	require.Same(t, upstream, diagnostic)
}

func TestChatGPTOrdinaryFailureRetainsRetryBudgetWithoutDeadLetter(t *testing.T) {
	db := pgtest.Open(t)
	repo := repository.NewTaskDeadLetterRepository(db)
	redis := miniredis.RunT(t)
	opt := asynq.RedisClientOpt{Addr: redis.Addr()}
	client := asynq.NewClient(opt)
	defer client.Close()
	inspector := asynq.NewInspector(opt)
	defer inspector.Close()
	server := asynq.NewServer(opt, asynq.Config{Concurrency: 1, TaskCheckInterval: 10 * time.Millisecond, RetryDelayFunc: func(int, error, *asynq.Task) time.Duration { return time.Hour }, IsFailure: chatGPTTaskIsFailure})
	var calls, callbacks atomic.Int32
	mux := asynq.NewServeMux()
	mux.Use(chatGPTFinalDeferralMiddleware(client))
	mux.Use(asynqdl.MiddlewareWithCallback(repo, func(context.Context, *asynq.Task, error) { callbacks.Add(1) }))
	mux.HandleFunc("fixture:ordinary", func(context.Context, *asynq.Task) error {
		calls.Add(1)
		return &types.ChatGPTError{Kind: "http", Status: 503, Code: "subscription_sharing_user_unavailable", Message: "temporarily unavailable"}
	})
	require.NoError(t, server.Start(mux))
	defer server.Shutdown()
	_, err := client.Enqueue(asynq.NewTask("fixture:ordinary", []byte(`{"knowledge_id":"retry"}`)), asynq.MaxRetry(3))
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		info, err := inspector.GetQueueInfo("default")
		return err == nil && info.Retry == 1
	}, 10*time.Second, 25*time.Millisecond)
	info, err := inspector.GetQueueInfo("default")
	require.NoError(t, err)
	require.Zero(t, info.Archived)
	tasks, err := inspector.ListRetryTasks("default")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, 1, tasks[0].Retried)
	require.Equal(t, 3, tasks[0].MaxRetry)
	require.EqualValues(t, 1, calls.Load())
	require.Zero(t, callbacks.Load())
	rows, _, err := repo.ListByTaskType(context.Background(), "fixture:ordinary", "", 10)
	require.NoError(t, err)
	require.Empty(t, rows)
	t.Logf("PostgreSQL task_dead_letters: rows=%d; Asynq archived=%d retry=%d retried=%d max_retry=%d handler_calls=%d callbacks=%d", len(rows), info.Archived, info.Retry, tasks[0].Retried, tasks[0].MaxRetry, calls.Load(), callbacks.Load())
}
