package container

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestReplicaVaultRebuildPausesBeforeWorkerStartupWithoutLosingInterruptedTasks(t *testing.T) {
	ctx := context.Background()
	source, target := pgtest.Open(t), pgtest.Open(t)
	root, uploads := t.TempDir(), t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", uploads)
	_, err := repository.EnsureSpaceKnowledgeBase(ctx, source, types.SingleSpaceID, "")
	require.NoError(t, err)
	require.NoError(t, source.Create(&types.Entity{ID: "preserved-profile", SpaceID: 1, CanonicalName: "Preserved owner profile", NormalizedName: "preserved owner profile", EntityType: "product", Description: "Keep user information while configuring recovery"}).Error)
	jobs := queue.NewClient(source)
	for _, state := range []string{"active", "pending"} {
		_, err := jobs.Enqueue(queue.NewTask(types.TypeManualProcess, []byte(`{"knowledge_id":"preserved-article","original":"accepted original"}`)), queue.TaskID(state), queue.MaxRetry(7))
		require.NoError(t, err)
		require.NoError(t, source.Exec(`UPDATE task_jobs SET state=?,retried=2,fail_count=1,last_error='preserved previous failure' WHERE id=?`, state, state).Error)
	}
	snapshot, err := maintenance.Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, maintenance.WriteSnapshot(root, uploads, snapshot))
	expected, err := maintenance.BuildManifest(ctx, source, root, uploads)
	require.NoError(t, err)

	result, err := rebuildVaultWithDatabase(ctx, target, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	require.NotNil(t, result)
	state, err := maintenanceops.Status(ctx, target)
	require.NoError(t, err)
	require.True(t, state.Paused)
	require.Equal(t, "snapshot", state.PauseMode)
	require.Equal(t, "vault rebuild awaiting configuration", state.PauseReason)
	verified, err := maintenance.VerifyReplica(ctx, target, root, uploads, false)
	require.NoError(t, err)
	require.True(t, verified.OK, verified.Differences)
	actual, err := maintenance.BuildManifest(ctx, target, root, uploads)
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(expected, actual), "pause changes only regenerated maintenance fields")

	started := make(chan struct{}, 1)
	client := queue.NewClient(target)
	require.NoError(t, client.Start(ctx, queue.HandlerFunc(func(context.Context, *queue.Task) error {
		started <- struct{}{}
		return nil
	}), queue.Config{Pools: types.WorkerPoolConcurrency{Core: 1}, CheckInterval: 20 * time.Millisecond}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	select {
	case <-started:
		t.Fatal("a restored task executed before credentials and vectors were ready")
	case <-time.After(250 * time.Millisecond):
	}
	require.NoError(t, client.Close())
	var retained []queue.Job
	require.NoError(t, target.Order("id").Find(&retained).Error)
	require.Len(t, retained, 2)
	for _, job := range retained {
		require.Equal(t, queue.TaskStatePending, job.State)
		require.Equal(t, 2, job.Retried)
		require.Equal(t, 1, job.FailCount)
		require.Equal(t, 7, job.MaxRetry)
		require.Equal(t, "preserved previous failure", job.LastError)
		require.JSONEq(t, `{"knowledge_id":"preserved-article","original":"accepted original"}`, string(job.Payload))
	}
	// A running application's writer records legitimate startup lease recovery.
	// The paired manifest comparison still preserves every unrelated table.
	require.NoError(t, maintenance.NewWriter(target, root, uploads, time.Hour).Flush(ctx))
	actual, err = maintenance.BuildManifest(ctx, target, root, uploads)
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(expected, actual))
	verified, err = maintenance.VerifyReplica(ctx, target, root, uploads, false)
	require.NoError(t, err)
	require.True(t, verified.OK, verified.Differences)
}

func TestReplicaVaultRebuildRejectsExistingArticlesWithoutPausingTheExistingDatabase(t *testing.T) {
	ctx := context.Background()
	source, target := pgtest.Open(t), pgtest.Open(t)
	root, uploads := t.TempDir(), t.TempDir()
	t.Setenv("LOCAL_STORAGE_BASE_DIR", uploads)
	kb, err := repository.EnsureSpaceKnowledgeBase(ctx, source, 1, "")
	require.NoError(t, err)
	snapshot, err := maintenance.Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, maintenance.WriteSnapshot(root, uploads, snapshot))
	require.NoError(t, target.Create(&types.Knowledge{ID: "existing-original", SpaceID: 1, KnowledgeBaseID: kb.ID, Title: "Preserve this existing article", Type: "manual", SourceKind: "article", ParseStatus: types.ParseStatusPending}).Error)
	_, err = rebuildVaultWithDatabase(ctx, target, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.ErrorContains(t, err, "requires an empty knowledge database")
	state, err := maintenanceops.Status(ctx, target)
	require.NoError(t, err)
	require.False(t, state.Paused)
	var original types.Knowledge
	require.NoError(t, target.First(&original, "id='existing-original'").Error)
	require.Equal(t, "Preserve this existing article", original.Title)
}
