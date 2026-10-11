package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func TestRSSCronPersistsAndExecutesScheduledTask(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	source := &types.DataSource{TenantID: 1, Name: "Scheduled RSS", Type: "rss", Status: "active", Config: types.JSON(`{"url":"https://example.org/feed"}`), SyncSchedule: "* * * * * *"}
	require.NoError(t, db.Create(source).Error)
	client := queue.NewClient(db)
	scheduler := NewScheduler(repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db), client)
	scheduler.SetDatabase(db)
	executed := make(chan types.DataSourceSyncPayload, 1)
	handler := queue.HandlerFunc(func(ctx context.Context, task *queue.Task) error {
		var payload types.DataSourceSyncPayload
		if err := json.Unmarshal(task.Payload(), &payload); err != nil {
			return err
		}
		if payload.Trigger != "schedule" || payload.DataSourceID != source.ID {
			return errors.New("unexpected schedule payload")
		}
		if err := db.WithContext(ctx).Model(&types.SyncLog{}).Where("id = ?", payload.SyncLogID).Update("status", types.SyncLogStatusSuccess).Error; err != nil {
			return err
		}
		executed <- payload
		return nil
	})
	require.NoError(t, client.Start(ctx, handler, queue.Config{Pools: types.WorkerPoolConcurrency{Maintenance: 1}}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.NoError(t, scheduler.Start(ctx))
	t.Cleanup(scheduler.Stop)
	select {
	case payload := <-executed:
		scheduler.Stop()
		var syncLog types.SyncLog
		require.NoError(t, db.First(&syncLog, "id = ?", payload.SyncLogID).Error)
		require.Equal(t, types.SyncLogStatusSuccess, syncLog.Status)
	case <-time.After(5 * time.Second):
		t.Fatal("RSS cron did not enqueue and execute at its scheduled tick")
	}
}

type failedScheduleQueue struct{ *queue.Client }

func (q failedScheduleQueue) EnqueueTx(context.Context, *gorm.DB, *queue.Task, ...queue.Option) (*queue.TaskInfo, error) {
	return nil, errors.New("forced queue write failure")
}

func TestRSSScheduledEnqueueFailureRollsBackSyncLog(t *testing.T) {
	db := pgtest.Open(t)
	source := &types.DataSource{TenantID: 1, Name: "RSS", Type: "rss", Status: "active", Config: types.JSON(`{}`)}
	require.NoError(t, db.Create(source).Error)
	scheduler := NewScheduler(repository.NewDataSourceRepository(db), repository.NewSyncLogRepository(db), failedScheduleQueue{queue.NewClient(db)})
	scheduler.SetDatabase(db)
	require.ErrorContains(t, scheduler.triggerSyncTx(context.Background(), source.ID, source.TenantID), "forced queue write failure")
	var logs, jobs int64
	require.NoError(t, db.Model(&types.SyncLog{}).Count(&logs).Error)
	require.NoError(t, db.Model(&queue.Job{}).Count(&jobs).Error)
	require.Zero(t, logs)
	require.Zero(t, jobs)
}
