package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestReconnectWaitsForLockDrainsOldWorkersAndResumes(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	config := testConfig()
	config.Pools = types.WorkerPoolConcurrency{Core: 1}
	config.ReconnectInitialDelay = 20 * time.Millisecond
	config.ReconnectMaxDelay = 100 * time.Millisecond
	config.ReconnectAlertAfter = 50 * time.Millisecond
	var calls, active, peak atomic.Int32
	started := make(chan struct{})
	handler := HandlerFunc(func(ctx context.Context, _ *Task) error {
		n := active.Add(1)
		if n > peak.Load() {
			peak.Store(n)
		}
		defer active.Add(-1)
		if calls.Add(1) == 1 {
			close(started)
			<-ctx.Done()
			time.Sleep(100 * time.Millisecond)
			return ctx.Err()
		}
		return nil
	})
	require.NoError(t, c.Start(context.Background(), handler, config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	first, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0), Retention(time.Hour))
	require.NoError(t, err)
	<-started
	pool, err := db.DB()
	require.NoError(t, err)
	blocker, err := pool.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { releaseLock(blocker) })
	c.mu.Lock()
	lock := c.lock
	c.mu.Unlock()
	var pid int
	c.mu.Lock()
	pidErr := lock.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid)
	c.mu.Unlock()
	require.NoError(t, pidErr)
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_terminate_backend($1)", pid)
	require.NoError(t, err)
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_lock(714140014)")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return c.ConnectionStatus().State == "reconnecting" }, 5*time.Second, 10*time.Millisecond)
	status := c.ConnectionStatus()
	require.NotEmpty(t, status.LastError)
	require.NotNil(t, status.Since)
	require.NotNil(t, status.NextRetryAt)
	second, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), Retention(time.Hour))
	require.NoError(t, err)
	require.Eventually(t, func() bool { return c.ConnectionStatus().Alert }, time.Second, 10*time.Millisecond)
	var interrupted Job
	// A producer may publish the recovered operation while consumption is locked.
	require.Eventually(t, func() bool {
		return db.First(&interrupted, "id=?", first.ID).Error == nil &&
			(interrupted.State == TaskStatePending || interrupted.State == TaskStateDispatched)
	}, 15*time.Second, 10*time.Millisecond)
	require.Zero(t, interrupted.Retried)
	require.Zero(t, interrupted.FailCount)
	time.Sleep(250 * time.Millisecond)
	require.EqualValues(t, 1, calls.Load(), "no task is claimed while another session owns the lock")
	var waiting Job
	require.NoError(t, db.First(&waiting, "id=?", second.ID).Error)
	require.Contains(t, []TaskState{TaskStatePending, TaskStateDispatched}, waiting.State)
	require.Contains(t, c.ConnectionStatus().LastError, "another task worker")
	_, err = blocker.ExecContext(context.Background(), "SELECT pg_advisory_unlock(714140014)")
	require.NoError(t, err)
	firstJob := jobState(t, c, first.ID, TaskStateCompleted)
	jobState(t, c, second.ID, TaskStateCompleted)
	require.Zero(t, firstJob.Retried)
	require.Zero(t, firstJob.FailCount)
	require.EqualValues(t, 1, peak.Load(), "old and new generation handlers cannot overlap")
	require.Equal(t, "active", c.ConnectionStatus().State)
	require.Empty(t, c.ConnectionStatus().LastError)
	require.False(t, c.ConnectionStatus().Alert)
	t.Log("terminated singleton session; reconnect status and alert visible; other lock owner prevents claims; release resumes old and new jobs without retry or overlap")
}
