package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/redistest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestManifestAcceptsActualPausedWorkerStartupRecoveryOnlyForInterruptedTasks(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`UPDATE maintenance_state SET paused=true,pause_mode='snapshot',pause_reason='restoring backup' WHERE id=true`).Error)
	client := redistest.NewClient(t, db)
	for _, state := range []string{"active", "pending", "scheduled", "completed"} {
		_, err := client.Enqueue(queue.NewTask(types.TypeManualProcess, []byte(`{"entry_id":"preserved-article","original":"preserve entire payload"}`)), queue.TaskID(state), queue.MaxRetry(5), queue.Retention(time.Hour), queue.Deadline(time.Now().Add(24*time.Hour)))
		require.NoError(t, err)
		require.NoError(t, db.Exec(`UPDATE task_pending_ops SET state=?,process_at='2026-10-01T12:00:00Z',created_at='2026-10-01T10:00:00Z',updated_at='2026-10-01T12:00:00Z',retried=2,fail_count=1,last_failed_at='2026-10-01T11:00:00Z',last_error='preserved previous error',group_name='preserved-group' WHERE id=?`, state, state).Error)
	}
	require.NoError(t, db.Exec(`UPDATE task_pending_ops SET worker_pool='core',started_at='2026-10-01T12:00:00Z' WHERE id='active'`).Error)
	require.NoError(t, db.Exec(`UPDATE task_pending_ops SET started_at='2026-10-01T12:00:00Z',completed_at='2026-10-01T12:01:00Z' WHERE id='completed'`).Error)
	root := t.TempDir()
	flush := func() {
		snapshot, err := Capture(ctx, db)
		require.NoError(t, err)
		require.NoError(t, WriteSnapshot(root, "", snapshot))
	}
	manifest := func() *Manifest {
		flush()
		result, err := BuildManifest(ctx, db, root, "")
		require.NoError(t, err)
		return result
	}
	expected := manifest()
	var original queue.Job
	require.NoError(t, db.First(&original, "id='active'").Error)
	started := make(chan struct{}, 1)
	require.NoError(t, client.Start(ctx, queue.HandlerFunc(func(context.Context, *queue.Task) error { started <- struct{}{}; return nil }), queue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, CheckInterval: 100 * time.Millisecond}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	var recovered queue.Job
	require.NoError(t, db.First(&recovered, "id='active'").Error)
	require.Equal(t, queue.TaskStatePending, recovered.State)
	require.Equal(t, original.ProcessAt, recovered.ProcessAt, "startup retains the durable runnable time")
	require.Equal(t, original.Payload, recovered.Payload)
	require.Equal(t, original.Retried, recovered.Retried)
	require.Equal(t, original.FailCount, recovered.FailCount)
	require.Equal(t, original.Deadline, recovered.Deadline)
	actual := manifest()
	require.Empty(t, CompareManifest(expected, actual), "the raw original active row proves this precise process_at recovery exception")
	verified, err := VerifyReplica(ctx, db, root, "", false)
	require.NoError(t, err)
	require.True(t, verified.OK, "%v", verified.Differences)
	select {
	case <-started:
		t.Fatal("restored work executed while snapshot recovery remained paused")
	case <-time.After(150 * time.Millisecond):
	}
	require.NoError(t, client.Close())

	for _, state := range []string{"pending", "scheduled", "completed"} {
		t.Run("preserve_"+state+"_schedule", func(t *testing.T) {
			require.NoError(t, db.Exec(`UPDATE task_pending_ops SET process_at=process_at+interval '1 hour' WHERE id=?`, state).Error)
			changed := manifest()
			differences := CompareManifest(expected, changed)
			require.Contains(t, differences, "table task_pending_ops")
			require.Contains(t, differences, "vault file .reliquary/data/task_pending_ops.json")
			require.NoError(t, db.Exec(`UPDATE task_pending_ops SET process_at=process_at-interval '1 hour' WHERE id=?`, state).Error)
		})
	}
	for _, mutation := range []struct{ name, change, restore string }{
		{"payload", `payload='{"entry_id":"different"}'`, `payload='{"entry_id":"preserved-article","original":"preserve entire payload"}'`},
		{"retry", `retried=3`, `retried=2`},
		{"failure", `fail_count=2`, `fail_count=1`},
		{"budget", `max_retry=6`, `max_retry=5`},
		{"deadline", `deadline=deadline+interval '1 hour'`, `deadline=deadline-interval '1 hour'`},
		{"history", `last_error='changed previous error'`, `last_error='preserved previous error'`},
	} {
		t.Run("preserve_active_"+mutation.name, func(t *testing.T) {
			require.NoError(t, db.Exec(`UPDATE task_pending_ops SET `+mutation.change+` WHERE id='active'`).Error)
			require.Contains(t, CompareManifest(expected, manifest()), "table task_pending_ops")
			require.NoError(t, db.Exec(`UPDATE task_pending_ops SET `+mutation.restore+` WHERE id='active'`).Error)
		})
	}
	actual = manifest()
	require.Empty(t, CompareManifest(expected, actual))
	legacy := *expected
	legacy.Tables = make(map[string]TableManifest, len(expected.Tables))
	for table, entry := range expected.Tables {
		legacy.Tables[table] = entry
	}
	legacyJobs := legacy.Tables["task_pending_ops"]
	legacyJobs.Data = nil
	legacy.Tables["task_pending_ops"] = legacyJobs
	require.Contains(t, CompareManifest(&legacy, actual), "table task_pending_ops", "no raw provenance means no recovered schedule exception")
	path := filepath.Join(root, ".reliquary/data/task_pending_ops.json")
	var tampered []map[string]any
	require.NoError(t, json.Unmarshal(mustRead(t, path), &tampered))
	for _, row := range tampered {
		if row["id"] == "active" {
			row["payload"] = map[string]any{"changed": true}
		}
	}
	bytes, err := json.MarshalIndent(tampered, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(bytes, '\n'), 0600))
	actual, err = BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "vault file .reliquary/data/task_pending_ops.json", "valid database recovery cannot conceal altered replica bytes")
}
