package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/utils"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/datasource"
	"reliquary/internal/maintenance"
	"reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestDataSourceMaintenanceProvenanceSurvivesLiveSettingsAndCredentials(t *testing.T) {
	db := pgtest.Open(t)
	ds := &types.DataSource{ID: "maintenance-source", TenantID: types.SingleTenantID, KnowledgeBaseID: "kb", Name: "Original paused source", Type: deletedItemConnectorType, Status: types.DataSourceStatusPaused,
		Config: types.JSON(`{"type":"test-sync-deletion","settings":{"url":"https://example.test/rss"},"credentials":null,"_reliquary_maintenance_original_status":"paused","_reliquary_maintenance_original_updated_at":"2026-01-02T03:04:05","_reliquary_maintenance_required_fields":"[\"credentials.auth_headers\"]","old_extension":{"retained":true}}`)}
	repo := repository.NewDataSourceRepository(db)
	require.NoError(t, repo.Create(context.Background(), ds))
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(deletedItemConnector{}))
	svc := &DataSourceService{dsRepo: repo, connectorRegistry: registry, scheduler: datasource.NewScheduler(repo, nil, nil)}
	assertPreserved := func() {
		stored, err := repo.FindByID(context.Background(), ds.ID)
		require.NoError(t, err)
		require.Equal(t, types.DataSourceStatusPaused, stored.Status, "filling credentials must not resume an originally paused source")
		var config map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(stored.Config, &config))
		require.JSONEq(t, `"paused"`, string(config["_reliquary_maintenance_original_status"]))
		require.JSONEq(t, `"2026-01-02T03:04:05"`, string(config["_reliquary_maintenance_original_updated_at"]))
		require.NotContains(t, config, "_reliquary_maintenance_required_fields", "completed checklist must not reactivate a later explicit pause")
		require.JSONEq(t, `{"retained":true}`, string(config["old_extension"]))
	}
	_, err := svc.UpdateDataSourceCredentials(context.Background(), ds.ID, map[string]interface{}{"auth_headers": `{"Authorization":"replacement"}`})
	require.NoError(t, err)
	assertPreserved()
	_, err = svc.UpdateDataSource(context.Background(), &types.DataSource{ID: ds.ID, Type: ds.Type, Name: "Edited name", Status: types.DataSourceStatusPaused,
		Config: types.JSON(`{"type":"test-sync-deletion","settings":{"url":"https://example.test/new"},"_reliquary_maintenance_original_status":"active"}`)})
	require.NoError(t, err)
	assertPreserved()
	require.NoError(t, svc.ClearDataSourceCredentials(context.Background(), ds.ID))
	assertPreserved()
	root := t.TempDir()
	expected, err := maintenance.BuildManifest(context.Background(), db, root, "")
	require.NoError(t, err)
	require.NoError(t, db.Model(&types.DataSource{}).Where("id=?", ds.ID).Update("status", types.DataSourceStatusError).Error)
	actual, err := maintenance.BuildManifest(context.Background(), db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, maintenance.CompareManifest(expected, actual), "retained credential provenance must not mask an unrelated source status change")
}

func TestAcceptedDataSourceTaskWaitsForMaintenanceCredentialsWithoutLosingBudget(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	source := &types.DataSource{ID: "restored-incomplete-source", TenantID: 1, Type: "rss", Status: types.DataSourceStatusPaused,
		Config: types.JSON(`{"credentials":null,"_reliquary_maintenance_required_fields":"[\"config.credentials.auth_headers.Authorization\"]"}`)}
	require.NoError(t, db.Create(source).Error)
	syncLog := &types.SyncLog{DataSourceID: source.ID, TenantID: 1, Status: types.SyncLogStatusRunning, StartedAt: time.Now().Add(-time.Hour)}
	require.NoError(t, db.Create(syncLog).Error)
	var before string
	require.NoError(t, db.Raw(`SELECT to_jsonb(t)::text FROM sync_logs t WHERE id=?`, syncLog.ID).Scan(&before).Error)
	payload, err := json.Marshal(types.DataSourceSyncPayload{DataSourceID: source.ID, TenantID: 1, SyncLogID: syncLog.ID})
	require.NoError(t, err)
	svc := &DataSourceService{dsRepo: repository.NewDataSourceRepository(db), syncLogRepo: repository.NewSyncLogRepository(db)}
	client := queue.NewClient(db)
	job, err := client.Enqueue(queue.NewTask(types.TypeDataSourceSync, payload), queue.Queue(types.QueueSync), queue.MaxRetry(0))
	require.NoError(t, err)
	require.NoError(t, client.Start(ctx, queue.HandlerFunc(func(ctx context.Context, task *queue.Task) error {
		err := svc.ProcessSync(ctx, task)
		var deferred *types.ChatGPTDeferredError
		if !errors.As(err, &deferred) {
			return errors.New("maintenance sync did not defer before any mutation")
		}
		return err
	}), queue.Config{Pools: types.WorkerPoolConcurrency{Maintenance: 1}, CheckInterval: 20 * time.Millisecond}))
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	require.Eventually(t, func() bool {
		var stored queue.Job
		return db.First(&stored, "id=?", job.ID).Error == nil && stored.State == queue.TaskStateScheduled
	}, 8*time.Second, 50*time.Millisecond)
	var stored queue.Job
	require.NoError(t, db.First(&stored, "id=?", job.ID).Error)
	require.Zero(t, stored.Retried)
	require.Zero(t, stored.FailCount)
	require.JSONEq(t, string(payload), string(stored.Payload))
	var after string
	require.NoError(t, db.Raw(`SELECT to_jsonb(t)::text FROM sync_logs t WHERE id=?`, syncLog.ID).Scan(&after).Error)
	require.Equal(t, before, after, "unfinished synchronization metadata must remain intact while waiting")
	var deadLetters int64
	require.NoError(t, db.Table("task_dead_letters").Count(&deadLetters).Error)
	require.Zero(t, deadLetters, "a restored zero-budget task must remain resumable")
}

func TestDataSourceMaintenanceRequiresEveryFieldBeforeLiveOperations(t *testing.T) {
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	db := pgtest.Open(t)
	ctx := context.Background()
	ds := &types.DataSource{ID: "incomplete-source", TenantID: types.SingleTenantID, KnowledgeBaseID: "kb", Name: "Previously active source", Type: deletedItemConnectorType, Status: types.DataSourceStatusPaused,
		Config: types.JSON(`{"type":"test-sync-deletion","settings":{"feed_url":"https://example.test/rss"},"credentials":null,"_reliquary_maintenance_original_status":"active","_reliquary_maintenance_original_updated_at":"2026-01-02T03:04:05","_reliquary_maintenance_required_fields":"[\"config.credentials.token\",\"config.credentials.app_secret\",\"config.credentials.auth_headers.Authorization\",\"config.settings.feed_url.userinfo\",\"config.settings.feed_url.query.API_Key\"]"}`)}
	repo := repository.NewDataSourceRepository(db)
	require.NoError(t, repo.Create(ctx, ds))
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(deletedItemConnector{}))
	svc := &DataSourceService{dsRepo: repo, connectorRegistry: registry, scheduler: datasource.NewScheduler(repo, nil, nil), syncLogRepo: repository.NewSyncLogRepository(db), taskEnqueuer: queue.NewClient(db)}
	require.ErrorContains(t, svc.ResumeDataSource(ctx, ds.ID), "config.credentials.app_secret")
	_, err := svc.ManualSync(ctx, ds.ID)
	require.ErrorContains(t, err, "config.credentials.token")
	_, err = svc.UpdateDataSource(ctx, &types.DataSource{ID: ds.ID, Type: ds.Type, Name: ds.Name, Status: types.DataSourceStatusActive})
	require.ErrorContains(t, err, "config.credentials.auth_headers.Authorization")
	_, err = svc.UpdateDataSource(ctx, &types.DataSource{ID: ds.ID, Type: ds.Type, Name: ds.Name, Status: types.DataSourceStatusActive, Config: types.JSON(`{"credentials":"malformed"}`)})
	require.Error(t, err, "a malformed replacement must not bypass the stored maintenance checklist")
	_, err = svc.UpdateDataSourceCredentials(ctx, ds.ID, map[string]interface{}{"token": "replacement-token"})
	require.ErrorContains(t, err, "config.credentials.app_secret")
	require.NotContains(t, err.Error(), "replacement-token")
	stored, err := repo.FindByID(ctx, ds.ID)
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusPaused, stored.Status)
	parsed, err := stored.ParseConfig()
	require.NoError(t, err)
	require.Equal(t, "replacement-token", parsed.Credentials["token"], "partial configuration is saved safely for the next edit")
	_, err = svc.UpdateDataSource(ctx, &types.DataSource{ID: ds.ID, Type: ds.Type, Name: ds.Name, Status: types.DataSourceStatusPaused,
		Config: types.JSON(`{"type":"test-sync-deletion","settings":{"feed_url":"https://reader:replacement-password@example.test/rss?api_key=replacement-query"}}`)})
	require.ErrorContains(t, err, "config.credentials.app_secret")
	stored, err = svc.UpdateDataSourceCredentials(ctx, ds.ID, map[string]interface{}{"token": "replacement-token", "app_secret": "replacement-app-secret", "auth_headers": `{"authorization":"replacement-header"}`})
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusActive, stored.Status, "a complete checklist restores the previous active state")
	require.False(t, stored.HasMaintenanceRequirements())
	require.Equal(t, "active", stored.MaintenanceOriginalStatus())
	require.NoError(t, svc.PauseDataSource(ctx, ds.ID))
	stored, err = svc.UpdateDataSourceCredentials(ctx, ds.ID, map[string]interface{}{"token": "later-token"})
	require.NoError(t, err)
	require.Equal(t, types.DataSourceStatusPaused, stored.Status, "completed migration metadata must not override a later explicit pause")
	_, err = svc.ManualSync(ctx, ds.ID)
	require.NoError(t, err, "ordinary paused sources keep their existing manual synchronization behavior")
	var jobs int64
	require.NoError(t, db.Table("task_jobs").Count(&jobs).Error)
	require.EqualValues(t, 1, jobs, "blocked operations must not create a task or lose an accepted task")
}

func TestRestoredRSSHeadersCanBeFilledThenSettingsSaved(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer replacement" {
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		w.Write([]byte(`<rss version="2.0"><channel><title>Private feed</title><link>https://example.test</link><description>Fixture</description></channel></rss>`))
	}))
	defer server.Close()
	for _, original := range []string{"active", "paused"} {
		t.Run(original, func(t *testing.T) {
			db := pgtest.Open(t)
			repo := repository.NewDataSourceRepository(db)
			registry := datasource.NewConnectorRegistry()
			require.NoError(t, registry.Register(rss.NewConnector()))
			ctx := context.Background()
			config := map[string]any{"type": "rss", "settings": map[string]any{"feed_urls": server.URL}, "credentials": nil, "_reliquary_maintenance_original_status": original, "_reliquary_maintenance_required_fields": `["config.credentials.auth_headers"]`}
			blob, err := json.Marshal(config)
			require.NoError(t, err)
			source := &types.DataSource{ID: "restored-rss", TenantID: 1, KnowledgeBaseID: "kb", Name: "Private feed", Type: "rss", Status: "paused", Config: blob}
			require.NoError(t, repo.Create(ctx, source))
			svc := &DataSourceService{dsRepo: repo, connectorRegistry: registry, scheduler: datasource.NewScheduler(repo, nil, nil)}
			filled, err := svc.UpdateDataSourceCredentials(ctx, source.ID, map[string]interface{}{"auth_headers": "Authorization: Bearer replacement"})
			require.NoError(t, err)
			require.Equal(t, original, filled.Status)
			require.False(t, filled.HasMaintenanceRequirements())
			public, _ := json.Marshal(map[string]any{"type": "rss", "settings": map[string]any{"feed_urls": server.URL}, "credentials": map[string]any{}})
			saved, err := svc.UpdateDataSource(ctx, &types.DataSource{ID: source.ID, Type: "rss", Name: source.Name, Config: public})
			require.NoError(t, err)
			require.Equal(t, original, saved.Status)
			stored, err := repo.FindByID(ctx, source.ID)
			require.NoError(t, err)
			parsed, err := stored.ParseConfig()
			require.NoError(t, err)
			require.Equal(t, "Authorization: Bearer replacement", parsed.Credentials["auth_headers"])
		})
	}
}
