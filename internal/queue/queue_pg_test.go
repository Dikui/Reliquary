package queue

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func testConfig() Config {
	return Config{Pools: types.WorkerPoolConcurrency{Core: 1, Enrichment: 1, Maintenance: 1, Shared: 1}, CheckInterval: 5 * time.Millisecond, RetryDelayFunc: func(int, error, *Task) time.Duration { return 5 * time.Millisecond }}
}
func jobState(t *testing.T, c *Client, id string, state TaskState) *Job {
	t.Helper()
	var j Job
	require.Eventually(t, func() bool { return c.db.First(&j, "id = ?", id).Error == nil && j.State == state }, 5*time.Second, 10*time.Millisecond)
	return &j
}

func TestEveryRegisteredTaskExecutesRetriesAndArchives(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	mux := NewServeMux()
	var completed atomic.Int32
	for _, definition := range types.QueueDefinitions() {
		for _, kind := range definition.TaskTypes {
			mux.HandleFunc(kind, func(ctx context.Context, task *Task) error {
				if string(task.Headers()["scenario"]) == "success" {
					completed.Add(1)
					return nil
				}
				return errors.New("ordinary handler failure")
			})
		}
	}
	require.NoError(t, c.Start(context.Background(), mux, testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	count := 0
	for _, definition := range types.QueueDefinitions() {
		for _, kind := range definition.TaskTypes {
			count++
			t.Run(kind, func(t *testing.T) {
				payload := []byte(`{"tenant_id":1,"entry_id":"fixture","library_id":"fixture-library"}`)
				i, err := c.Enqueue(NewTaskWithHeaders(kind, payload, map[string]string{"scenario": "success"}), Queue(definition.Name), MaxRetry(2), Retention(time.Hour))
				require.NoError(t, err)
				j := jobState(t, c, i.ID, TaskStateCompleted)
				require.Zero(t, j.Retried)
				i, err = c.Enqueue(NewTask(kind, payload), Queue(definition.Name), MaxRetry(2))
				require.NoError(t, err)
				j = jobState(t, c, i.ID, TaskStateArchived)
				require.Equal(t, 2, j.Retried)
				require.Equal(t, 3, j.FailCount)
				var rows []types.TaskDeadLetter
				require.NoError(t, db.Where("task_type = ?", kind).Find(&rows).Error)
				require.Len(t, rows, 1)
				require.Equal(t, 3, rows[0].FailCount)
				require.Equal(t, types.TaskScopeLibrary, rows[0].Scope)
				require.Equal(t, "fixture", rows[0].RelatedID)
				t.Logf("type=%s queue=%s executed=true ordinary_attempts=%d dead_letters=%d", kind, definition.Name, j.FailCount, len(rows))
			})
		}
	}
	require.EqualValues(t, count, completed.Load())
}

func TestEnqueueSharesCommitAndRollback(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	rolledBack, err := c.EnqueueTx(context.Background(), tx, NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":1}`)))
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&Job{}).Where("id = ?", rolledBack.ID).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, db.Model(&Job{}).Where("id = ?", rolledBack.ID).Count(&count).Error)
	require.Zero(t, count)
	tx = db.Begin()
	committed, err := c.EnqueueTx(context.Background(), tx, NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":1}`)))
	require.NoError(t, err)
	require.NoError(t, tx.Commit().Error)
	require.NoError(t, db.Model(&Job{}).Where("id = ?", committed.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	// A duplicate deterministic schedule ID leaves the outer transaction usable.
	tx = db.Begin()
	_, err = c.EnqueueTx(context.Background(), tx, NewTask(types.TypeEntryListReparse, nil), TaskID("schedule:minute"))
	require.NoError(t, err)
	_, err = c.EnqueueTx(context.Background(), tx, NewTask(types.TypeEntryListReparse, nil), TaskID("schedule:minute"))
	require.ErrorIs(t, err, ErrTaskIDConflict)
	require.NoError(t, tx.Exec("SELECT 1").Error)
	require.NoError(t, tx.Commit().Error)
	t.Log("uncommitted task invisible; rollback leaves no task; committed task visible; duplicate schedule does not abort transaction")
}

func TestQuotaDeferralPreservesBudgetAndOptionsAtFinalAttempt(t *testing.T) {
	for _, budget := range []int{0, 2} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			db := pgtest.Open(t)
			c := NewClient(db)
			config := testConfig()
			var callbacks atomic.Int32
			config.OnDeadLetter = func(context.Context, *Task, error) { callbacks.Add(1) }
			paused := &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(5 * time.Minute)}
			require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return fmt.Errorf("generation: %w", paused) }), config))
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			deadline := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
			payload := []byte(`{"tenant_id":1,"entry_id":"quota"}`)
			headers := map[string]string{"fixture": "preserve", "trace": "trace-fixture"}
			info, err := c.Enqueue(NewTaskWithHeaders(types.TypeDocumentProcess, payload, headers), MaxRetry(budget), Timeout(2*time.Hour), Deadline(deadline), Retention(time.Hour), Group("batch"), ProcessAt(time.Now().Add(time.Hour)))
			require.NoError(t, err)
			// Exercise the previously fragile final ordinary attempt for both budgets.
			require.NoError(t, db.Model(&Job{}).Where("id = ?", info.ID).Updates(map[string]interface{}{"retried": budget, "state": TaskStatePending, "process_at": time.Now().UTC()}).Error)
			var j Job
			require.Eventually(t, func() bool {
				return db.First(&j, "id = ?", info.ID).Error == nil && j.State == TaskStateScheduled && j.ProcessAt.Before(time.Now().Add(10*time.Minute))
			}, 5*time.Second, 10*time.Millisecond)
			after := j.Info()
			require.Equal(t, info.ID, after.ID)
			require.Equal(t, budget, after.Retried)
			require.Equal(t, budget, after.MaxRetry)
			require.Equal(t, info.Timeout, after.Timeout)
			require.Equal(t, info.Deadline, after.Deadline)
			require.Equal(t, info.Retention, after.Retention)
			require.Equal(t, info.Group, after.Group)
			require.JSONEq(t, string(payload), string(after.Payload))
			require.Equal(t, headers, after.Headers)
			require.Zero(t, j.FailCount)
			var count int64
			require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&count).Error)
			require.Zero(t, count)
			require.Zero(t, callbacks.Load())
			require.Greater(t, after.NextProcessAt.Sub(time.Now()), 4*time.Minute)
			t.Logf("quota deferred final_attempt=%d same_id=true payload_headers_timeout_deadline_retention_group_preserved=true fail_count=%d dead_letters=%d", budget, j.FailCount, count)
		})
	}
}

func TestShutdownInterruptionReexecutesWithoutRetry(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	started := make(chan struct{})
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, _ *Task) error { close(started); <-ctx.Done(); return ctx.Err() }), testConfig()))
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0), Retention(time.Hour))
	require.NoError(t, err)
	<-started
	require.NoError(t, c.Close())
	j := jobState(t, c, i.ID, TaskStatePending)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	restarted := NewClient(db)
	require.NoError(t, restarted.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return nil }), testConfig()))
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	j = jobState(t, restarted, i.ID, TaskStateCompleted)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	t.Log("interrupted handler is requeued on shutdown and completes after restart without spending retries")
}

// This subprocess is killed while its handler owns a persisted active job.
func TestQueueCrashChild(t *testing.T) {
	dsn := os.Getenv("RELIQUARY_QUEUE_CRASH_DSN")
	if dsn == "" {
		t.Skip("subprocess fixture")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	c := NewClient(db)
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, _ *Task) error { <-ctx.Done(); return ctx.Err() }), testConfig()))
	select {}
}

func TestKilledProcessReleasesLockAndRecoversActiveJob(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":1,"entry_id":"crash"}`)), MaxRetry(0), Timeout(time.Hour), Retention(time.Hour))
	require.NoError(t, err)
	command := exec.Command(os.Args[0], "-test.run=^TestQueueCrashChild$", "-test.timeout=30s")
	command.Env = append(os.Environ(), "RELIQUARY_QUEUE_CRASH_DSN="+db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, command.Start())
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	j := jobState(t, c, i.ID, TaskStateActive)
	require.NotNil(t, j.LeaseUntil)
	require.Greater(t, time.Until(*j.LeaseUntil), 20*time.Second)
	require.NoError(t, command.Process.Kill())
	require.Error(t, command.Wait())
	restarted := NewClient(db)
	// SIGKILL closes the client socket before PostgreSQL necessarily notices it.
	// Wait for the server to release session ownership without weakening recovery.
	require.Eventually(t, func() bool {
		return restarted.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return nil }), testConfig()) == nil
	}, 5*time.Second, 20*time.Millisecond)
	t.Cleanup(func() { require.NoError(t, restarted.Close()) })
	j = jobState(t, restarted, i.ID, TaskStateCompleted)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	t.Log("SIGKILL released singleton PostgreSQL lock; restart rescued active job before 30-second lease expiry; completed without retry")
}

func TestPoolConcurrencyAndWeightedSharedClaims(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	config := testConfig()
	config.Pools = types.WorkerPoolConcurrency{Core: 2, Enrichment: 3, Maintenance: 2, Shared: 2}
	gate := make(chan struct{})
	var active [4]atomic.Int32
	var peaks [4]atomic.Int32
	index := map[string]int{types.WorkerPoolCore: 0, types.WorkerPoolEnrichment: 1, types.WorkerPoolMaintenance: 2, types.WorkerPoolShared: 3}
	h := HandlerFunc(func(ctx context.Context, task *Task) error {
		pool, _ := GetWorkerPool(ctx)
		idx := index[pool]
		n := active[idx].Add(1)
		for old := peaks[idx].Load(); n > old; old = peaks[idx].Load() {
			if peaks[idx].CompareAndSwap(old, n) {
				break
			}
		}
		queue, _ := GetQueueName(ctx)
		if pool == types.WorkerPoolShared && queue != types.QueueDefault && queue != types.QueueEntity {
			return errors.New("maintenance borrowed shared capacity")
		}
		select {
		case <-gate:
		case <-ctx.Done():
		}
		active[idx].Add(-1)
		return nil
	})
	for _, definition := range types.QueueDefinitions() {
		for n := 0; n < 12; n++ {
			_, err := c.Enqueue(NewTask(definition.TaskTypes[0], nil), Queue(definition.Name), Retention(time.Hour))
			require.NoError(t, err)
		}
	}
	require.NoError(t, c.Start(context.Background(), h, config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	require.Eventually(t, func() bool {
		return active[0].Load() == 2 && active[1].Load() == 3 && active[2].Load() == 2 && active[3].Load() == 2
	}, 5*time.Second, 10*time.Millisecond)
	close(gate)
	for idx, limit := range []int32{2, 3, 2, 2} {
		require.LessOrEqual(t, peaks[idx].Load(), limit)
	}
	require.NoError(t, c.Close())
	// With both logical queues ready, shared dispatch selects three default
	// jobs for each entity job. Dedicated workers retain their own limits.
	require.NoError(t, db.Exec("DELETE FROM task_jobs").Error)
	for _, name := range []string{types.QueueDefault, types.QueueEntity} {
		for n := 0; n < 8; n++ {
			_, err := c.Enqueue(NewTask("fixture:weights", nil), Queue(name))
			require.NoError(t, err)
		}
	}
	c.config = config.withDefaults()
	shared := c.config.workerPools()[3]
	counts := map[string]int{}
	for n := 0; n < 8; n++ {
		j, err := c.claim(context.Background(), shared)
		require.NoError(t, err)
		require.NotNil(t, j)
		counts[j.Queue]++
	}
	require.Equal(t, 6, counts[types.QueueDefault])
	require.Equal(t, 2, counts[types.QueueEntity])
	t.Logf("pool peaks core=%d enrichment=%d maintenance=%d shared=%d; shared claims default=%d entity=%d", peaks[0].Load(), peaks[1].Load(), peaks[2].Load(), peaks[3].Load(), counts[types.QueueDefault], counts[types.QueueEntity])
}

func TestPermanentAndElapsedDeadlineArchiveOnce(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	var calls, callbacks atomic.Int32
	config := testConfig()
	config.OnDeadLetter = func(context.Context, *Task, error) { callbacks.Add(1) }
	h := HandlerFunc(func(context.Context, *Task) error {
		calls.Add(1)
		return &types.ChatGPTError{Kind: "failed", Status: 403, Code: "subscription_sharing_user_not_eligible", Message: "subscription restriction"}
	})
	require.NoError(t, c.Start(context.Background(), h, config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(3))
	require.NoError(t, err)
	j := jobState(t, c, i.ID, TaskStateArchived)
	require.Zero(t, j.Retried)
	require.Equal(t, 1, j.FailCount)
	i, err = c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(3), Deadline(time.Now().Add(-time.Minute)))
	require.NoError(t, err)
	j = jobState(t, c, i.ID, TaskStateArchived)
	require.Zero(t, j.Retried)
	require.Equal(t, 1, j.FailCount)
	require.EqualValues(t, 1, calls.Load())
	require.Eventually(t, func() bool { return callbacks.Load() == 2 }, time.Second, 10*time.Millisecond)
	var rows []types.TaskDeadLetter
	require.NoError(t, db.Find(&rows).Error)
	require.Len(t, rows, 2)
	t.Log("permanent ChatGPT failure and elapsed deadline archive on first attempt; expired deadline never reenters rescue loop")
}

func TestArchiveAndDeadLetterRollbackTogether(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	config := testConfig()
	var callbacks atomic.Int32
	config.OnDeadLetter = func(context.Context, *Task, error) { callbacks.Add(1) }
	require.NoError(t, db.Exec(`CREATE FUNCTION reject_dead_letter() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'dead letter unavailable'; END $$; CREATE TRIGGER reject_dead_letter BEFORE INSERT ON task_dead_letters FOR EACH ROW EXECUTE FUNCTION reject_dead_letter()`).Error)
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return errors.New("archive fixture") }), config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0))
	require.NoError(t, err)
	j := jobState(t, c, i.ID, TaskStateActive)
	require.Zero(t, j.FailCount)
	require.Zero(t, callbacks.Load())
	require.NoError(t, db.Exec("DROP TRIGGER reject_dead_letter ON task_dead_letters").Error)
	j = jobState(t, c, i.ID, TaskStateArchived)
	require.Equal(t, 1, j.FailCount)
	require.Eventually(t, func() bool { return callbacks.Load() == 1 }, time.Second, 10*time.Millisecond)
	var count int64
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
	t.Log("dead-letter insert outage rolls back archive and counters; retrying persistence writes one archive and one callback")
}

func TestActiveCancellationAcknowledgedWithoutRetry(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	started := make(chan struct{})
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, _ *Task) error { close(started); <-ctx.Done(); return ctx.Err() }), testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(3))
	require.NoError(t, err)
	<-started
	require.NoError(t, c.Cancel(context.Background(), i.ID))
	require.Eventually(t, func() bool {
		var n int64
		return db.Model(&Job{}).Where("id = ?", i.ID).Count(&n).Error == nil && n == 0
	}, time.Second, 10*time.Millisecond)
	var n int64
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&n).Error)
	require.Zero(t, n)
}

func TestDeferredErrorNeverCreatesDeadLetterAfterDeadline(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	started, release := make(chan struct{}), make(chan struct{})
	deadline := time.Now().UTC().Add(time.Hour)
	h := HandlerFunc(func(ctx context.Context, _ *Task) error {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return ctx.Err()
		}
		return &types.ChatGPTDeferredError{Reason: "daily_limit", RetryAt: time.Now().Add(time.Hour)}
	})
	require.NoError(t, c.Start(context.Background(), h, testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0), Deadline(deadline))
	require.NoError(t, err)
	<-started
	// Move the persisted absolute deadline into the past after execution has
	// started, so database load cannot expire it before the first claim.
	expired := time.Now().UTC().Add(-time.Minute)
	require.NoError(t, db.Model(&Job{}).Where("id = ?", i.ID).Update("deadline", expired).Error)
	close(release)
	j := jobState(t, c, i.ID, TaskStateScheduled)
	require.WithinDuration(t, expired, *j.Deadline, time.Microsecond)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	var n int64
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&n).Error)
	require.Zero(t, n)
	// A quota error itself always snoozes. A subsequent execution whose
	// absolute deadline has elapsed terminates with a deadline error instead.
	require.NoError(t, c.RunNow(context.Background(), i.ID))
	j = jobState(t, c, i.ID, TaskStateArchived)
	require.Contains(t, j.LastError, "absolute deadline")
	require.NotContains(t, j.LastError, "daily_limit")
}

func TestTimeoutRetriesAndArchives(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	var calls atomic.Int32
	h := HandlerFunc(func(ctx context.Context, _ *Task) error { calls.Add(1); <-ctx.Done(); return ctx.Err() })
	require.NoError(t, c.Start(context.Background(), h, testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(1), Timeout(20*time.Millisecond))
	require.NoError(t, err)
	j := jobState(t, c, i.ID, TaskStateArchived)
	require.EqualValues(t, 2, calls.Load())
	require.Equal(t, 1, j.Retried)
	require.Equal(t, 2, j.FailCount)
	require.Equal(t, int64(20*time.Millisecond), j.TimeoutNS)
	require.Contains(t, j.LastError, "context deadline exceeded")
}

func TestLeaseHeartbeatsAndExpiredLeaseRescue(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	config := testConfig()
	config.LeaseDuration = 120 * time.Millisecond
	config.HeartbeatInterval = 20 * time.Millisecond
	var calls atomic.Int32
	h := HandlerFunc(func(ctx context.Context, task *Task) error {
		calls.Add(1)
		if task.Type() == "fixture:long" {
			<-ctx.Done()
			return ctx.Err()
		}
		return nil
	})
	require.NoError(t, c.Start(context.Background(), h, config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	other := NewClient(db)
	require.ErrorContains(t, other.Start(context.Background(), h, config), "another task worker process")
	i, err := c.Enqueue(NewTask("fixture:long", nil), MaxRetry(0))
	require.NoError(t, err)
	j := jobState(t, c, i.ID, TaskStateActive)
	firstLease := *j.LeaseUntil
	require.Eventually(t, func() bool {
		return db.First(&j, "id = ?", i.ID).Error == nil && j.State == TaskStateActive && j.LeaseUntil.After(firstLease.Add(1100*time.Millisecond))
	}, 3*time.Second, 10*time.Millisecond)
	require.EqualValues(t, 1, calls.Load(), "live heartbeat prevents duplicate recovery")
	require.NoError(t, c.Cancel(context.Background(), i.ID))
	// Simulate a lost execution owner after the workers are already running.
	expired := time.Now().UTC().Add(-time.Minute)
	j = &Job{ID: "fixture:expired-lease", TaskType: types.TypeDocumentProcess, Queue: types.QueueDefault, Payload: []byte("{}"), Headers: []byte("{}"), State: TaskStateActive, ClaimToken: "lost-owner", LeaseUntil: &expired, ProcessAt: expired, CreatedAt: expired, UpdatedAt: expired, RetentionNS: int64(time.Hour)}
	require.NoError(t, db.Create(j).Error)
	j = jobState(t, c, j.ID, TaskStateCompleted)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	require.EqualValues(t, 2, calls.Load())
	t.Log("singleton rejects a second worker; live leases renew across rescue cycles; expired owner is rescued without spending retries")
}

func TestDailyCountersCountSuccessfulAndFailedExecutions(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	h := HandlerFunc(func(_ context.Context, task *Task) error {
		if task.Type() == "fixture:success" {
			return nil
		}
		return errors.New("ordinary failure")
	})
	require.NoError(t, c.Start(context.Background(), h, testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	success, err := c.Enqueue(NewTask("fixture:success", nil), Retention(time.Hour))
	require.NoError(t, err)
	jobState(t, c, success.ID, TaskStateCompleted)
	failure, err := c.Enqueue(NewTask("fixture:failure", nil), MaxRetry(2))
	require.NoError(t, err)
	jobState(t, c, failure.ID, TaskStateArchived)
	var counters struct{ Processed, Failed int }
	require.NoError(t, db.Raw("SELECT processed,failed FROM task_queue_counters WHERE queue=? AND day=?::date", types.QueueDefault, time.Now().UTC().Format("2006-01-02")).Scan(&counters).Error)
	require.Equal(t, 4, counters.Processed)
	require.Equal(t, 3, counters.Failed)
	t.Log("daily counters: one success + three failed attempts yields processed=4 failed=3")
}

func TestPostgresErrorDiagnosticsAllowMultibyteNulAndInvalidUTF8(t *testing.T) {
	for _, message := range []string{"x" + strings.Repeat("错", 3000), "bad\x00text", "invalid\xff\xfeUTF8"} {
		t.Run(fmt.Sprint(len(message)), func(t *testing.T) {
			db := pgtest.Open(t)
			c := NewClient(db)
			require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { return errors.New(message) }), testConfig()))
			t.Cleanup(func() { require.NoError(t, c.Close()) })
			i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0))
			require.NoError(t, err)
			j := jobState(t, c, i.ID, TaskStateArchived)
			var rows []types.TaskDeadLetter
			require.NoError(t, db.Find(&rows).Error)
			require.Len(t, rows, 1)
			for _, text := range []string{j.LastError, rows[0].LastError} {
				require.True(t, utf8.ValidString(text))
				require.NotContains(t, text, "\x00")
				require.LessOrEqual(t, len(text), 8192)
				require.Equal(t, NormalizeError(message), text)
			}
			if len(message) > 8192 {
				require.True(t, strings.HasSuffix(j.LastError, "...(truncated)"))
			}
		})
	}
}

func TestRescueDoesNotReclaimAStillLiveHandler(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	config := testConfig()
	config.Pools = types.WorkerPoolConcurrency{Core: 2}
	config.LeaseDuration = 2 * time.Minute
	config.HeartbeatInterval = time.Minute
	started := make(chan struct{}, 2)
	var calls atomic.Int32
	h := HandlerFunc(func(ctx context.Context, _ *Task) error {
		calls.Add(1)
		started <- struct{}{}
		<-ctx.Done()
		return ctx.Err()
	})
	require.NoError(t, c.Start(context.Background(), h, config))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0))
	require.NoError(t, err)
	<-started
	// Simulate a heartbeat delayed by a database outage while the owning
	// handler remains alive. Rescue must wait for that handler to exit.
	require.NoError(t, db.Model(&Job{}).Where("id = ?", i.ID).Update("lease_until", time.Now().Add(-time.Minute)).Error)
	require.NoError(t, c.recoverJobs(context.Background(), false))
	var j Job
	require.NoError(t, db.First(&j, "id = ?", i.ID).Error)
	require.Equal(t, TaskStateActive, j.State)
	require.EqualValues(t, 1, calls.Load())
	require.NoError(t, c.Cancel(context.Background(), i.ID))
	require.Eventually(t, func() bool {
		var n int64
		return db.Model(&Job{}).Where("id = ?", i.ID).Count(&n).Error == nil && n == 0
	}, time.Second, 10*time.Millisecond)
	t.Log("expired lease owned by a live local handler remains active and executes once; cancellation ownership remains intact")
}

func TestDisconnectedSingletonPausesWorkerStatsAndPreservesTask(t *testing.T) {
	db := pgtest.Open(t)
	c := NewClient(db)
	started := make(chan struct{})
	h := HandlerFunc(func(ctx context.Context, _ *Task) error { close(started); <-ctx.Done(); return ctx.Err() })
	require.NoError(t, c.Start(context.Background(), h, testConfig()))
	// The lock session is deliberately terminated, so its close may report
	// the expected broken connection after the worker has already stopped.
	t.Cleanup(func() { _ = c.Close() })
	i, err := c.Enqueue(NewTask(types.TypeDocumentProcess, nil), MaxRetry(0))
	require.NoError(t, err)
	<-started
	stats, err := c.WorkerStats(context.Background())
	require.NoError(t, err)
	require.Len(t, stats, 4)
	for _, pool := range stats {
		require.Equal(t, "active", pool.Status)
	}
	var pid int
	c.lockMu.Lock()
	pidErr := c.lock.QueryRowContext(context.Background(), "SELECT pg_backend_pid()").Scan(&pid)
	c.lockMu.Unlock()
	require.NoError(t, pidErr)
	var terminated bool
	require.NoError(t, db.Raw("SELECT pg_terminate_backend(?)", pid).Scan(&terminated).Error)
	require.True(t, terminated)
	require.Eventually(t, func() bool {
		stats, err = c.WorkerStats(context.Background())
		if err != nil {
			return false
		}
		for _, pool := range stats {
			if pool.Status != "stopped" || pool.Active != 0 {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	j := jobState(t, c, i.ID, TaskStatePending)
	require.Zero(t, j.Retried)
	require.Zero(t, j.FailCount)
	t.Log("terminated PostgreSQL singleton session cancels workers; all pool stats show stopped; interrupted job remains pending without spending retries")
}
