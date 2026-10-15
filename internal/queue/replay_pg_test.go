package queue

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestIKB20OfflineReplayEmptyRedisHonorsPauseAndSchedule(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	ctx := context.Background()
	_, err := c.ReplayPaused(ctx)
	require.ErrorContains(t, err, "snapshot pause")
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	info, err := c.Enqueue(NewTask("test:restored", []byte(`{"original":"preserved"}`)), MaxRetry(7))
	require.NoError(t, err)
	require.NoError(t, db.Model(&Job{}).Where("id=?", info.ID).Updates(map[string]any{"state": TaskStateActive, "retried": 2, "fail_count": 1}).Error)
	future, err := c.Enqueue(NewTask("test:future", nil), ProcessIn(time.Hour))
	require.NoError(t, err)
	result, err := c.ReplayPaused(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.Dispatched)
	require.EqualValues(t, 1, result.Deferred)
	inspector := asynq.NewInspector(c.redisOpt)
	defer inspector.Close()
	var row Job
	require.NoError(t, db.First(&row, "id=?", info.ID).Error)
	_, err = inspector.GetTaskInfo(c.redisQueue(row.Queue), deliveryID(&row))
	require.NoError(t, err)
	var calls atomic.Int32
	require.NoError(t, c.Start(ctx, HandlerFunc(func(context.Context, *Task) error { calls.Add(1); return nil }), testConfig()))
	t.Cleanup(func() { require.NoError(t, c.Close()) })
	time.Sleep(200 * time.Millisecond)
	require.Zero(t, calls.Load())
	require.NoError(t, db.First(&row, "id=?", info.ID).Error)
	require.Equal(t, TaskStatePending, row.State)
	require.Equal(t, 2, row.Retried)
	require.Equal(t, 1, row.FailCount)
	require.Equal(t, 7, row.MaxRetry)
	require.JSONEq(t, `{"original":"preserved"}`, string(row.Payload))
	_, err = c.ReplayPaused(ctx)
	require.ErrorContains(t, err, "another task worker")
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	jobState(t, c, info.ID, TaskStateCompleted)
	require.EqualValues(t, 1, calls.Load())
	row = Job{}
	require.NoError(t, db.First(&row, "id=?", future.ID).Error)
	require.Equal(t, TaskStateScheduled, row.State)
}

func TestIKB20ReplayIncludesMoreThanOneDispatchPage(t *testing.T) {
	db := pgtest.Open(t)
	c := testClient(t, db)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	require.NoError(t, db.Exec(`INSERT INTO task_pending_ops(id,task_type,queue,payload,object_id,state) SELECT 'page-'||i,'test:page','default','{}','page-'||i,'pending' FROM generate_series(1,505) i`).Error)
	result, err := c.ReplayPaused(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 505, result.Dispatched)
}
