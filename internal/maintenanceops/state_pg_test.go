package maintenanceops

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func TestCollectionPauseWaitsForAcceptedTransaction(t *testing.T) {
	db := pgtest.Open(t)
	tx := db.Begin()
	require.NoError(t, CheckCollectionTx(context.Background(), tx))
	finished := make(chan error, 1)
	flushed := make(chan struct{}, 1)
	go func() {
		finished <- Pause(context.Background(), db, "snapshot", "backup", func(context.Context) error { flushed <- struct{}{}; return nil })
	}()
	select {
	case err := <-finished:
		t.Fatalf("pause crossed an active collection transaction: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, tx.Commit().Error)
	require.NoError(t, <-finished)
	<-flushed
	require.Error(t, db.Transaction(func(tx *gorm.DB) error { return CheckCollectionTx(context.Background(), tx) }))
	ctx := taskqueue.WithExecution(context.Background(), &taskqueue.TaskInfo{ID: "already-accepted-rss"}, "maintenance")
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckCollectionTx(ctx, tx) }))
	require.NoError(t, Resume(context.Background(), db))
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error { return CheckCollectionTx(context.Background(), tx) }))
}

func TestDrainTimeoutResumesCollectionAndRecordsAlert(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("INSERT INTO task_jobs(id,task_type,queue,state) VALUES('processing-article','document:process','default','active')").Error)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := Pause(ctx, db, "drain", "pack", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	state, err := Status(context.Background(), db)
	require.NoError(t, err)
	require.False(t, state.Paused)
	require.NoError(t, Record(context.Background(), db, "backup", Result{Status: "skipped", Error: "queue wait exceeded 1s"}))
	state, err = Status(context.Background(), db)
	require.NoError(t, err)
	require.Equal(t, "skipped", state.BackupStatus)
	require.Equal(t, "queue wait exceeded 1s", state.BackupError)
	var rows int64
	require.NoError(t, db.Table("task_jobs").Where("id='processing-article' AND state='active'").Count(&rows).Error)
	require.EqualValues(t, 1, rows)
}

func TestSnapshotDoesNotWaitForArticleOrLongTasks(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Exec("INSERT INTO task_jobs(id,task_type,queue,state) VALUES('article','document:process','default','active'),('vectors','entry:embedding_rebuild','entity','active'),('topics','topic:backfill','entity','active')").Error)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, Pause(ctx, db, "snapshot", "backup", nil))
	require.NoError(t, Resume(context.Background(), db))
	require.NoError(t, db.Exec("DELETE FROM task_jobs WHERE id='article'").Error)
	require.NoError(t, Pause(ctx, db, "drain", "pack", nil))
	var rows int64
	require.NoError(t, db.Table("task_jobs").Count(&rows).Error)
	require.EqualValues(t, 2, rows)
}

func TestWeeklyMonthlySchedulesClaimOnceAndStoreResults(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	require.NoError(t, Configure(ctx, db, ScheduleConfig{BackupSchedule: "0 3 * * 0", DrillSchedule: "0 3 1 * *", Timezone: "Asia/Shanghai", Directory: "/Volumes/backup", Enabled: true}))
	// November 1, 2026 is both the weekly Sunday and monthly first-day tick.
	now := time.Date(2026, 11, 1, 3, 0, 0, 0, time.FixedZone("Asia/Shanghai", 8*3600))
	var wg sync.WaitGroup
	results := make(chan DueResult, 2)
	failures := make(chan error, 2)
	for range 2 {
		wg.Add(1)
		go func() { defer wg.Done(); result, err := Due(ctx, db, now, false); results <- result; failures <- err }()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		require.NoError(t, err)
	}
	var jobs []string
	for result := range results {
		jobs = append(jobs, result.Jobs...)
		require.Equal(t, "/Volumes/backup", result.Directory)
	}
	require.ElementsMatch(t, []string{"backup", "drill"}, jobs)
	require.NoError(t, Record(ctx, db, "backup", Result{Status: "success"}))
	require.NoError(t, Record(ctx, db, "drill", Result{Status: "success"}))
	result, err := Due(ctx, db, now.Add(time.Minute), false)
	require.NoError(t, err)
	require.Empty(t, result.Jobs)
	// Missing Sunday's exact minute still causes one backup on wake-up.
	result, err = Due(ctx, db, now.Add(7*24*time.Hour+5*time.Minute), false)
	require.NoError(t, err)
	require.Equal(t, []string{"backup"}, result.Jobs)
	require.NoError(t, Record(ctx, db, "backup", Result{Status: "success", Path: "/Volumes/backup/20261101", SizeBytes: 2048}))
	require.NoError(t, Record(ctx, db, "drill", Result{Status: "failed", Error: "verify: damaged article"}))
	state, err := Status(ctx, db)
	require.NoError(t, err)
	require.Equal(t, "success", state.BackupStatus)
	require.EqualValues(t, 2048, state.BackupSizeBytes)
	require.Equal(t, "failed", state.DrillStatus)
	require.Contains(t, state.DrillError, "damaged article")
	require.NoError(t, Configure(ctx, db, ScheduleConfig{BackupSchedule: "* * * * *", DrillSchedule: "* * * * *", Timezone: "UTC", Directory: "/backup", Enabled: false}))
	result, err = Due(ctx, db, now.Add(time.Hour), true)
	require.NoError(t, err)
	require.Empty(t, result.Jobs)
}

func TestAutomaticMaintenanceRetriesInterruptedOrFailedRunAfterFifteenMinutes(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 2, 2, 0, 0, 0, time.UTC)
	jobs, err := Due(ctx, db, now, false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"backup", "drill"}, jobs.Jobs)
	require.NoError(t, Record(ctx, db, "backup", Result{Status: "running"}))
	require.NoError(t, Record(ctx, db, "drill", Result{Status: "failed", Error: "damaged archive"}))
	jobs, err = Due(ctx, db, now.Add(14*time.Minute), false)
	require.NoError(t, err)
	require.Empty(t, jobs.Jobs)
	jobs, err = Due(ctx, db, now.Add(15*time.Minute), false)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"backup", "drill"}, jobs.Jobs)
	jobs, err = Due(ctx, db, now.Add(16*time.Minute), false)
	require.NoError(t, err)
	require.Empty(t, jobs.Jobs)
	require.NoError(t, Pause(ctx, db, "snapshot", "manual migration", nil))
	jobs, err = Due(ctx, db, now.Add(time.Hour), true)
	require.NoError(t, err)
	require.Empty(t, jobs.Jobs, "automatic runner must not resume a manual migration")
}

func TestProcessingArticleSnapshotRestoresAndQueueResumes(t *testing.T) {
	source, restored := pgtest.Open(t), pgtest.Open(t)
	ctx := context.Background()
	const original = "Exact original content admitted before the power interruption."
	require.NoError(t, source.Exec(`INSERT INTO entries
		(id,space_id,knowledge_base_id,type,title,source,parse_status,vault_body)
		VALUES ('backup-processing',1,'backup-kb','manual','Interrupted article','','processing',?)`, original).Error)
	payload, err := json.Marshal(map[string]any{"entry_id": "backup-processing", "content": original})
	require.NoError(t, err)
	require.NoError(t, source.Exec(`INSERT INTO task_jobs
		(id,task_type,queue,state,payload,max_retry,retried,fail_count,claim_token,lease_until,retention_ns)
		VALUES ('backup-job','manual:process','default','active',?::jsonb,3,1,1,'interrupted-process',now()+interval '1 hour',?)`, string(payload), int64(time.Hour)).Error)
	// Backup does not wait for this interrupted active job. Full PostgreSQL
	// records, including payload and retry budget, are exported and read back.
	require.NoError(t, Pause(ctx, source, "snapshot", "automatic backup", nil))
	var articleJSON, taskJSON string
	require.NoError(t, source.Raw("SELECT jsonb_agg(to_jsonb(t))::text FROM entries t WHERE id='backup-processing'").Scan(&articleJSON).Error)
	require.NoError(t, source.Raw("SELECT jsonb_agg(to_jsonb(t))::text FROM task_jobs t WHERE id='backup-job'").Scan(&taskJSON).Error)
	archive := struct {
		Articles json.RawMessage `json:"articles"`
		Tasks    json.RawMessage `json:"tasks"`
	}{json.RawMessage(articleJSON), json.RawMessage(taskJSON)}
	encoded, err := json.Marshal(archive)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "processing-snapshot.json")
	require.NoError(t, os.WriteFile(path, encoded, 0600))
	readback, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(readback, &archive))
	require.NoError(t, restored.Exec("INSERT INTO entries SELECT * FROM jsonb_populate_recordset(NULL::entries,?::jsonb)", string(archive.Articles)).Error)
	require.NoError(t, restored.Exec("INSERT INTO task_jobs SELECT * FROM jsonb_populate_recordset(NULL::task_jobs,?::jsonb)", string(archive.Tasks)).Error)
	var restoredJob taskqueue.Job
	require.NoError(t, restored.First(&restoredJob, "id='backup-job'").Error)
	require.Equal(t, taskqueue.TaskStateActive, restoredJob.State)
	require.Equal(t, 1, restoredJob.Retried)
	require.Equal(t, 1, restoredJob.FailCount)
	require.JSONEq(t, string(payload), string(restoredJob.Payload))

	client := taskqueue.NewClient(restored)
	completed := make(chan error, 1)
	handler := taskqueue.HandlerFunc(func(ctx context.Context, task *taskqueue.Task) error {
		var input struct {
			EntryID string `json:"entry_id"`
			Content string `json:"content"`
		}
		if err := json.Unmarshal(task.Payload(), &input); err != nil {
			return err
		}
		if input.Content != original {
			return fmt.Errorf("original payload changed during backup")
		}
		var article types.Entry
		if err := restored.WithContext(ctx).First(&article, "id=?", input.EntryID).Error; err != nil {
			return err
		}
		if article.VaultBody != original {
			return fmt.Errorf("original article changed during restore")
		}
		err := restored.WithContext(ctx).Model(&types.Entry{}).Where("id=?", input.EntryID).Update("parse_status", "completed").Error
		completed <- err
		return err
	})
	require.NoError(t, client.Start(ctx, handler, taskqueue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, CheckInterval: 5 * time.Millisecond}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	select {
	case err := <-completed:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("restored processing article stayed stuck")
	}
	require.Eventually(t, func() bool {
		return restored.First(&restoredJob, "id='backup-job'").Error == nil && restoredJob.State == taskqueue.TaskStateCompleted
	}, time.Second, 10*time.Millisecond)
	var article types.Entry
	require.NoError(t, restored.First(&article, "id='backup-processing'").Error)
	require.Equal(t, "completed", article.ParseStatus)
	require.Equal(t, original, article.VaultBody)
	require.Equal(t, 1, restoredJob.Retried, "crash recovery must not consume a retry")
	require.Equal(t, 1, restoredJob.FailCount, "crash recovery must not count as failure")
	require.NoError(t, Resume(ctx, source))
}
