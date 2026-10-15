package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func redisMessage(t *testing.T, c *Client, j *Job, suffix string) {
	t.Helper()
	transport := asynq.NewClient(c.redisOpt)
	defer transport.Close()
	payload, _ := json.Marshal(wakePayload{j.ID})
	_, err := transport.Enqueue(asynq.NewTask(wakeTaskType, payload), asynq.Queue(c.redisQueue(j.Queue)), asynq.TaskID(deliveryID(j)+suffix))
	require.NoError(t, err)
}
func TestRollbackNeverPublishesRedis(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	tx := db.Begin()
	info, err := c.EnqueueTx(context.Background(), tx, NewTask("test:rollback", nil))
	require.NoError(t, err)
	require.NoError(t, tx.Rollback().Error)
	require.NoError(t, c.DispatchPending(context.Background()))
	var count int64
	require.NoError(t, db.Model(&Job{}).Count(&count).Error)
	require.Zero(t, count)
	inspector := asynq.NewInspector(c.redisOpt)
	defer inspector.Close()
	_, err = inspector.GetTaskInfo(c.redisQueue(types.QueueDefault), info.ID+":1:1")
	require.True(t, errors.Is(err, asynq.ErrTaskNotFound) || errors.Is(err, asynq.ErrQueueNotFound))
}
func TestCommittedWhileRedisUnavailableRecovers(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	opt := c.redisOpt
	unavailable := opt
	unavailable.Addr = "127.0.0.1:1"
	unavailable.DialTimeout = 10 * time.Millisecond
	producer := NewClientWithRedis(db, unavailable)
	t.Cleanup(func() { require.NoError(t, producer.Close()) })
	info, err := producer.Enqueue(NewTask("test:outage", nil))
	require.NoError(t, err)
	require.Error(t, producer.DispatchPending(context.Background()))
	var calls atomic.Int32
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil }), testConfig()))
	jobState(t, c, info.ID, TaskStateCompleted)
	require.EqualValues(t, 1, calls.Load())
}
func TestDuplicateAndLostAcknowledgementHaveOneBusinessResult(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	require.NoError(t, db.Exec("CREATE TABLE operation_results(id text PRIMARY KEY)").Error)
	info, err := c.Enqueue(NewTask("test:once", nil))
	require.NoError(t, err)
	var calls atomic.Int32
	h := HandlerFunc(func(ctx context.Context, _ *Task) error {
		calls.Add(1)
		id, _ := GetTaskID(ctx)
		return db.Exec("INSERT INTO operation_results VALUES (?)", id).Error
	})
	require.NoError(t, c.Start(context.Background(), h, testConfig()))
	j := jobState(t, c, info.ID, TaskStateCompleted)
	// Same delivery after the transport acknowledgement, then a distinct stale
	// message. The PG terminal row is authoritative in both cases.
	transport := asynq.NewClient(c.redisOpt)
	defer transport.Close()
	payload, _ := json.Marshal(wakePayload{j.ID})
	for n := 0; n < 3; n++ {
		_, err = transport.Enqueue(asynq.NewTask(wakeTaskType, payload), asynq.Queue(c.redisQueue(j.Queue)), asynq.TaskID(deliveryID(j)))
		if errors.Is(err, asynq.ErrTaskIDConflict) {
			time.Sleep(100 * time.Millisecond)
			continue
		}
		require.NoError(t, err)
	}
	redisMessage(t, c, j, "-late")
	time.Sleep(250 * time.Millisecond)
	var count int64
	require.NoError(t, db.Table("operation_results").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.EqualValues(t, 1, calls.Load())
}
func TestRedisFlushStartupReplaysAllUnfinishedOperations(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	// DB 15 is reserved by this non-parallel destructive transport test.
	c.redisOpt.DB = 15
	r := redis.NewClient(&redis.Options{Addr: c.redisOpt.Addr, DB: 15})
	require.NoError(t, r.FlushDB(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close(); _ = r.FlushDB(context.Background()).Err(); _ = r.Close() })
	var ids []string
	for n := 0; n < 9; n++ {
		info, err := c.Enqueue(NewTask("test:replay", nil))
		require.NoError(t, err)
		ids = append(ids, info.ID)
	}
	require.NoError(t, c.DispatchPending(context.Background()))
	require.NoError(t, r.FlushDB(context.Background()).Err())
	var calls atomic.Int32
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil }), testConfig()))
	for _, id := range ids {
		jobState(t, c, id, TaskStateCompleted)
	}
	require.EqualValues(t, 9, calls.Load())
}
func TestOldDeliveryAndGenerationCannotWriteNewOutcome(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	info, err := c.Enqueue(NewTask("test:fence", nil), ProcessIn(time.Hour))
	require.NoError(t, err)
	var old Job
	require.NoError(t, db.First(&old, "id=?", info.ID).Error)
	require.NoError(t, c.RunNow(context.Background(), info.ID))
	var calls atomic.Int32
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil }), testConfig()))
	done := jobState(t, c, info.ID, TaskStateCompleted)
	redisMessage(t, c, &old, "")
	old.State = TaskStateActive
	require.NoError(t, c.finish(context.Background(), &old, NewTask(old.TaskType, old.Payload), errors.New("stale failure"), false))
	after := jobState(t, c, info.ID, TaskStateCompleted)
	require.Equal(t, done.FailCount, after.FailCount)
	require.EqualValues(t, 1, calls.Load())
}
func TestCancelledOperationAndDeletionIgnoreLateMessages(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	info, err := c.Enqueue(NewTask("test:cancel", nil), ProcessIn(time.Hour))
	require.NoError(t, err)
	var old Job
	require.NoError(t, db.First(&old, "id=?", info.ID).Error)
	require.NoError(t, c.Cancel(context.Background(), info.ID))
	jobState(t, c, info.ID, TaskStateCancelled)
	require.NoError(t, c.Delete(context.Background(), info.ID))
	redisMessage(t, c, &old, "")
	var calls atomic.Int32
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil }), testConfig()))
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, calls.Load())
}

func TestOlderEntryOperationCannotOverwriteNewGeneration(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Generation fixture','personal'); INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id) VALUES('library',1,'Library','',''); INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status) VALUES('entry',1,'library','manual','Generation fixture','','pending')`).Error)
	old, err := c.Enqueue(NewTask(types.TypeDocumentProcess, []byte(`{"entry_id":"entry","attempt":1}`)))
	require.NoError(t, err)
	newer, err := c.Enqueue(NewTask(types.TypeDocumentProcess, []byte(`{"entry_id":"entry","attempt":2}`)))
	require.NoError(t, err)
	var calls atomic.Int32
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, _ *Task) error {
		calls.Add(1)
		return db.WithContext(ctx).Exec("UPDATE entries SET parse_status='completed',title='new generation' WHERE id='entry'").Error
	}), testConfig()))
	jobState(t, c, newer.ID, TaskStateCompleted)
	jobState(t, c, old.ID, TaskStateCancelled)
	var title string
	require.NoError(t, db.Raw("SELECT title FROM entries WHERE id='entry'").Scan(&title).Error)
	require.Equal(t, "new generation", title)
	require.EqualValues(t, 1, calls.Load())
}

func TestPeriodicSweepRecoversRedisLossWithoutRestart(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	c.redisOpt.DB = 13
	r := redis.NewClient(&redis.Options{Addr: c.redisOpt.Addr, DB: 13})
	require.NoError(t, r.FlushDB(context.Background()).Err())
	t.Cleanup(func() { _ = c.Close(); _ = r.FlushDB(context.Background()).Err(); _ = r.Close() })
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	cfg := testConfig()
	cfg.Pools = types.WorkerPoolConcurrency{Core: 1}
	cfg.DeliveryTimeout = 80 * time.Millisecond
	first, err := c.Enqueue(NewTask("test:hold", nil))
	require.NoError(t, err)
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, t *Task) error {
		calls.Add(1)
		if t.Type() == "test:hold" {
			close(entered)
			select {
			case <-release:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		return nil
	}), cfg))
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("first operation did not start")
	}
	second, err := c.Enqueue(NewTask("test:recover", nil))
	require.NoError(t, err)
	require.NoError(t, c.DispatchPending(context.Background()))
	jobState(t, c, second.ID, TaskStateDispatched)
	require.NoError(t, r.FlushDB(context.Background()).Err())
	close(release)
	jobState(t, c, first.ID, TaskStateCompleted)
	recovered := jobState(t, c, second.ID, TaskStateCompleted)
	require.Greater(t, recovered.Delivery, int64(1))
	require.EqualValues(t, 2, calls.Load())
}

func TestObjectMutationDrainsOldWriterBeforeNewGeneration(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	require.NoError(t, db.Exec("CREATE TABLE mutation_results(value text); INSERT INTO mutation_results VALUES('initial')").Error)
	entered := make(chan struct{})
	old, err := c.Enqueue(NewTask("test:old", []byte(`{"entry_id":"object"}`)))
	require.NoError(t, err)
	cfg := testConfig()
	cfg.Pools = types.WorkerPoolConcurrency{Core: 1}
	require.NoError(t, c.Start(context.Background(), HandlerFunc(func(ctx context.Context, t *Task) error {
		if t.Type() == "test:old" {
			close(entered)
			<-ctx.Done()
			return db.Exec("UPDATE mutation_results SET value='old finished'").Error
		}
		return nil
	}), cfg))
	select {
	case <-entered:
	case <-time.After(15 * time.Second):
		t.Fatal("old writer did not start")
	}
	err = c.WithObject(context.Background(), "object", func(ctx context.Context) error {
		if err := c.Cancel(ctx, old.ID); err != nil {
			return err
		}
		return db.Exec("UPDATE mutation_results SET value='new generation'").Error
	})
	require.NoError(t, err)
	var value string
	require.NoError(t, db.Raw("SELECT value FROM mutation_results").Scan(&value).Error)
	require.Equal(t, "new generation", value)
	state := jobState(t, c, old.ID, TaskStateCancelled)
	require.Zero(t, state.Retried)
	require.Zero(t, state.FailCount)
}

func TestObjectGenerationFollowsTransactionOrderNotRequestClock(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Generation order','personal'); INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id) VALUES('library',1,'Library','',''); INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status) VALUES('entry',1,'library','manual','Generation order','','pending')`).Error)
	blocker := db.Begin()
	require.NoError(t, blocker.Error)
	defer blocker.Rollback()
	require.NoError(t, blocker.Exec("SELECT id FROM entries WHERE id='entry' FOR UPDATE").Error)
	type result struct {
		info *TaskInfo
		err  error
	}
	later := make(chan result, 1)
	go func() {
		info, err := c.Enqueue(NewTask(types.TypeManualProcess, []byte(`{"entry_id":"entry"}`)))
		later <- result{info, err}
	}()
	require.Eventually(t, func() bool {
		var n int64
		return db.Raw("SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%entries%'").Scan(&n).Error == nil && n > 0
	}, 5*time.Second, 10*time.Millisecond)
	earlier, err := c.EnqueueTx(context.Background(), blocker, NewTask(types.TypeManualProcess, []byte(`{"entry_id":"entry"}`)))
	require.NoError(t, err)
	require.NoError(t, blocker.Commit().Error)
	var completed result
	select {
	case completed = <-later:
	case <-time.After(5 * time.Second):
		t.Fatal("waiting enqueue did not resume")
	}
	require.NoError(t, completed.err)
	var a, b Job
	require.NoError(t, db.First(&a, "id=?", earlier.ID).Error)
	require.NoError(t, db.First(&b, "id=?", completed.info.ID).Error)
	require.Less(t, a.Generation, b.Generation)
	require.True(t, b.CreatedAt.Before(a.CreatedAt))
	require.Equal(t, TaskStateCancelled, a.State)
}
