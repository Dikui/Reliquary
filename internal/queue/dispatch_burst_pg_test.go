package queue

import (
	"context"
	"database/sql"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"reliquary/internal/database"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type burstTrace struct {
	logger.Interface
	pool    *sql.DB
	peak    atomic.Int64
	refused atomic.Int64
}

func (trace *burstTrace) Trace(ctx context.Context, start time.Time, sql func() (string, int64), err error) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "53300" {
		trace.refused.Add(1)
	}
	if trace.pool != nil {
		open := int64(trace.pool.Stats().OpenConnections)
		for old := trace.peak.Load(); open > old; old = trace.peak.Load() {
			if trace.peak.CompareAndSwap(old, open) {
				break
			}
		}
	}
	trace.Interface.Trace(ctx, start, sql, err)
}

// Regression for the entity embedding burst on maintenance resume. Capture
// transient connection errors too, since a later sweep can hide them.
func TestDispatchDueBurstStaysWithinPostgresPool(t *testing.T) {
	fixture := pgtest.Open(t)
	t.Setenv("DB_MAX_OPEN_CONNS", "4")
	t.Setenv("DB_MAINTENANCE_MAX_OPEN_CONNS", "4")
	trace := &burstTrace{Interface: logger.Default.LogMode(logger.Silent)}
	dsn := fixture.Dialector.(*postgres.Dialector).DSN
	db, err := database.ApplicationPool.Open(postgres.Open(dsn), &gorm.Config{Logger: trace})
	require.NoError(t, err)
	pool, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })
	trace.pool = pool

	producer := testClient(t, db)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true, pause_mode='snapshot' WHERE id=true").Error)
	const tasks = 1105 // incident size, spanning three dispatch batches
	due := time.Now().Add(-time.Second)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		for range tasks {
			_, err := producer.EnqueueTx(t.Context(), tx, NewTask(types.TypeEntityNameEmbeddingRebuild, nil),
				Queue(types.QueueEntity), ProcessAt(due), MaxRetry(0))
			if err != nil {
				return err
			}
		}
		// One writer and at most one concurrent visibility query on the root DB.
		require.LessOrEqual(t, trace.peak.Load(), int64(2))
		return nil
	}))
	require.NoError(t, producer.Close())
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false, pause_mode='' WHERE id=true").Error)

	worker := testClient(t, db)
	var calls atomic.Int64
	release := make(chan struct{})
	waitsBefore := pool.Stats().WaitCount
	cfg := testConfig()
	cfg.Pools = types.WorkerPoolConcurrency{Enrichment: 12}
	require.NoError(t, worker.Start(t.Context(), HandlerFunc(func(ctx context.Context, _ *Task) error {
		calls.Add(1)
		return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			select {
			case <-release:
				return tx.Exec("SELECT 1").Error
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}), cfg))
	// Hold the first handlers' connections until a caller actually queues behind
	// the singleton and the three available SQL sessions, even on a fast server.
	require.Eventually(t, func() bool {
		stats := pool.Stats()
		return stats.InUse == 4 && stats.WaitCount > waitsBefore
	}, 15*time.Second, 5*time.Millisecond)
	close(release)
	require.Eventually(t, func() bool {
		var completed int64
		return db.Model(&Job{}).Where("state=? AND fail_count=0 AND retried=0", TaskStateCompleted).Count(&completed).Error == nil && completed == tasks
	}, 90*time.Second, 50*time.Millisecond)
	require.NoError(t, worker.Close())
	var dead int64
	require.NoError(t, db.Model(&types.TaskDeadLetter{}).Count(&dead).Error)
	require.Zero(t, dead)
	require.EqualValues(t, tasks, calls.Load())
	require.Zero(t, trace.refused.Load(), "including connection errors recovered by later attempts")
	require.EqualValues(t, 4, pool.Stats().MaxOpenConnections)
	require.LessOrEqual(t, trace.peak.Load(), int64(4))
	t.Logf("%d completed, peak %d PostgreSQL connections, %d pool waits", tasks, trace.peak.Load(), pool.Stats().WaitCount)
}
