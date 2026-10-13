package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestSnapshotPauseRestoresInterruptedQueueWithoutClaimingUntilResume(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot',pause_reason='waiting_credentials' WHERE id=true").Error)
	client := testClient(t, db)
	job, err := client.Enqueue(NewTask(types.TypeManualProcess, []byte(`{"entry_id":"waiting-credentials","content":"Retain original while waiting"}`)), MaxRetry(3), Retention(time.Hour))
	require.NoError(t, err)
	require.NoError(t, db.Model(&Job{}).Where("id=?", job.ID).Updates(map[string]any{"state": TaskStateActive, "retried": 1, "fail_count": 1, "delivery": 1}).Error)
	executed := make(chan *Task, 1)
	require.NoError(t, client.Start(context.Background(), HandlerFunc(func(_ context.Context, task *Task) error { executed <- task; return nil }), testConfig()))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	select {
	case <-executed:
		t.Fatal("waiting credential queue executed before maintenance resume")
	case <-time.After(75 * time.Millisecond):
	}
	var retained Job
	require.NoError(t, db.First(&retained, "id=?", job.ID).Error)
	require.Equal(t, TaskStatePending, retained.State, "startup should recover interrupted task but leave it waiting")
	require.Equal(t, 1, retained.Retried)
	require.Equal(t, 1, retained.FailCount)
	require.JSONEq(t, string(job.Payload), string(retained.Payload))
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	select {
	case task := <-executed:
		require.JSONEq(t, string(job.Payload), string(task.Payload()))
	case <-time.After(5 * time.Second):
		t.Fatal("queue did not resume after credentials were ready")
	}
	retained = *jobState(t, client, job.ID, TaskStateCompleted)
	require.Equal(t, 1, retained.Retried)
	require.Equal(t, 1, retained.FailCount)
}

func TestDrainPauseAllowsPreviouslyAcceptedTaskToFinish(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='drain' WHERE id=true").Error)
	client := testClient(t, db)
	executed := make(chan struct{}, 1)
	require.NoError(t, client.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { executed <- struct{}{}; return nil }), testConfig()))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	_, err := client.Enqueue(NewTask(types.TypeDocumentProcess, []byte(`{"entry_id":"accepted-before-pack"}`)))
	require.NoError(t, err)
	select {
	case <-executed:
	case <-time.After(5 * time.Second):
		t.Fatal("pack drain blocked an accepted article")
	}
}

func TestSnapshotPausePreservesExpiredQueueHistoryUntilResume(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	client := testClient(t, db)
	completed, err := client.Enqueue(NewTask(types.TypeManualProcess, []byte(`{"original":"completed history"}`)), Queue("unregistered-history"), Retention(time.Nanosecond))
	require.NoError(t, err)
	require.NoError(t, db.Model(&Job{}).Where("id=?", completed.ID).Updates(map[string]any{"state": TaskStateCompleted, "completed_at": time.Now().Add(-time.Hour)}).Error)
	config := testConfig()
	// This test exercises maintenance, with no claimable work. Slow the empty
	// worker polls so package-wide concurrency does not flood PostgreSQL.
	config.CheckInterval = 100 * time.Millisecond
	config.DeliveryTimeout = time.Millisecond
	require.NoError(t, client.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return nil }), config))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	// Startup normalization is allowed. A timed-out delivery introduced after
	// startup must remain unchanged throughout the paused maintenance ticks.
	expired, err := client.Enqueue(NewTask(types.TypeManualProcess, []byte(`{"original":"timed-out delivery"}`)), Queue("unregistered-history"))
	require.NoError(t, err)
	require.NoError(t, db.Model(&Job{}).Where("id=?", expired.ID).Updates(map[string]any{"state": TaskStateDispatched, "dispatched_at": time.Now().Add(-time.Hour)}).Error)
	var original string
	require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM task_pending_ops j").Scan(&original).Error)
	time.Sleep(1250 * time.Millisecond)
	cycleCtx, cancelCycle := context.WithTimeout(context.Background(), 10*time.Second)
	err = client.maintenanceCycle(cycleCtx)
	cancelCycle()
	require.NoError(t, err, "paused maintenance cycle must finish without changing jobs")
	var retained string
	require.NoError(t, db.Raw("SELECT jsonb_agg(to_jsonb(j) ORDER BY id)::text FROM task_pending_ops j").Scan(&retained).Error)
	require.JSONEq(t, original, retained, "snapshot ticks must not prune history or recover timed-out deliverys")
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	var lastJobs []Job
	var lastError error
	var observationMu sync.Mutex
	if !assert.Eventually(t, func() bool {
		queryCtx, cancelQuery := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelQuery()
		var jobs []Job
		err := db.WithContext(queryCtx).Where("id IN ?", []string{completed.ID, expired.ID}).Find(&jobs).Error
		observationMu.Lock()
		lastJobs, lastError = jobs, err
		observationMu.Unlock()
		return err == nil && len(jobs) == 1 && jobs[0].ID == expired.ID && (jobs[0].State == TaskStatePending || jobs[0].State == TaskStateDispatched)
	}, 20*time.Second, 50*time.Millisecond, "background maintenance must prune completed history and recover the timed-out delivery after resume") {
		diagnosticCtx, cancelDiagnostic := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancelDiagnostic()
		var state string
		stateError := db.WithContext(diagnosticCtx).Raw("SELECT to_jsonb(s)::text FROM maintenance_state s WHERE id=true").Scan(&state).Error
		observationMu.Lock()
		t.Logf("last queue error=%v; jobs=%+v; maintenance=%s; maintenance query error=%v", lastError, lastJobs, state, stateError)
		observationMu.Unlock()
		t.FailNow()
	}
}
