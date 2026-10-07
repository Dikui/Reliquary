package service

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestDataSourceSiteAdapterFailurePersistsAndStillIngests(t *testing.T) {
	db := pgtest.Open(t)
	files := &intakeFileService{saved: map[string][]byte{}}
	task := &intakeTask{}
	kb := &types.KnowledgeBase{ID: "adapter-kb", TenantID: 1, Name: "Adapter source", EmbeddingModelID: "embedding"}
	knowledge := &knowledgeService{db: db, repo: apprepo.NewKnowledgeRepository(db), kbService: &createKnowledgeFileKBServiceStub{kb: kb}, fileSvc: files, task: task}
	svc := &DataSourceService{knowledgeService: knowledge}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.UserIDContextKey, "editor")
	ds := &types.DataSource{ID: "adapter-source", TenantID: 1, KnowledgeBaseID: kb.ID, Type: types.ConnectorTypeRSS}
	item := &types.FetchedItem{ExternalID: "article", Title: "Original fallback article", Content: []byte(strings.Repeat("The original readable source survives a failed site widget. ", 8)), URL: "https://kimi.ai/blog/kimi-k2-5", Metadata: map[string]string{"publisher": "Kimi", "published_at": "2026-09-24"}, AdapterFailures: []types.SourceAdapterFailure{{Adapter: "kimi-k25", Site: "kimi.ai", URL: "https://kimi.ai/blog/kimi-k2-5", Reason: "expected widgets or embedded data unavailable"}}}
	result := &types.SyncResult{}
	svc.applyFetchedItem(ctx, ds, item, nil, result)
	require.Zero(t, result.Failed, "%+v", result)
	require.Equal(t, 1, result.Created)
	require.Equal(t, 1, task.calls, "fallback body must enter normal intake")
	var anomaly types.KnowledgeAnomaly
	require.NoError(t, db.Where("tenant_id = ? AND knowledge_base_id = ?", 1, kb.ID).First(&anomaly).Error)
	require.Equal(t, "source_adapter_failed", anomaly.Kind)
	require.Equal(t, item.AdapterFailures[0].Site, anomaly.Site)
	require.Equal(t, item.URL, anomaly.SourceURL)
	require.Equal(t, item.AdapterFailures[0].Reason, anomaly.Reason)
	require.Equal(t, "kimi-k25", anomaly.Adapter)
	require.Equal(t, "pending", anomaly.Status)
	var stored types.Knowledge
	require.NoError(t, db.Where("tenant_id = ? AND knowledge_base_id = ?", 1, kb.ID).First(&stored).Error)
	intake, err := stored.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, item.URL, intake.SourceInfo.OriginalURL)
	require.Equal(t, item.Metadata["published_at"], intake.SourceInfo.PublishedAt)
	require.Equal(t, string(item.Content), string(files.saved[intake.OriginalPath]))
}

func TestDataSourceDiscoveryFailurePersistsWithoutMessage(t *testing.T) {
	db := pgtest.Open(t)
	svc := &DataSourceService{knowledgeService: &knowledgeService{db: db}}
	failure := types.SourceAdapterFailure{Adapter: "qwen", Site: "qwen.ai", URL: "https://qwen.ai/blog", Reason: "article response unsuccessful"}
	item := &types.FetchedItem{ExternalID: "listing", Title: "Qwen blog", AdapterFailures: []types.SourceAdapterFailure{failure}, Metadata: map[string]string{"error": "generic listing unavailable"}}
	result := &types.SyncResult{}
	svc.applyFetchedItem(context.Background(), &types.DataSource{TenantID: 1, KnowledgeBaseID: "adapter-kb"}, item, nil, result)
	require.Equal(t, 1, result.Failed)
	var anomalies []types.KnowledgeAnomaly
	require.NoError(t, db.Find(&anomalies).Error)
	require.Len(t, anomalies, 1)
	require.Equal(t, failure.URL, anomalies[0].SourceURL)
	require.Equal(t, failure.Reason, anomalies[0].Reason)
}

func TestDataSourceDiagnosticOnlyFailurePersistsWithoutIntake(t *testing.T) {
	db := pgtest.Open(t)
	svc := &DataSourceService{knowledgeService: &knowledgeService{db: db}}
	failure := types.SourceAdapterFailure{Adapter: "kimi-k25", Site: "kimi.ai", URL: "https://kimi.ai/blog/kimi-k2-5", Reason: "expected widget unavailable"}
	item := &types.FetchedItem{ExternalID: "old:adapter-diagnostic", Title: "Old original", AdapterFailures: []types.SourceAdapterFailure{failure}, Metadata: map[string]string{"adapter_failure_only": "true"}}
	result := &types.SyncResult{}
	svc.applyFetchedItem(context.Background(), &types.DataSource{TenantID: 1, KnowledgeBaseID: "adapter-kb"}, item, nil, result)
	require.Equal(t, 1, result.Skipped)
	require.Zero(t, result.Failed)
	require.Zero(t, result.Created)
	var anomaly types.KnowledgeAnomaly
	require.NoError(t, db.First(&anomaly).Error)
	require.Equal(t, failure.URL, anomaly.SourceURL)
	require.Equal(t, failure.Reason, anomaly.Reason)
	var messages int64
	require.NoError(t, db.Model(&types.Knowledge{}).Count(&messages).Error)
	require.Zero(t, messages)
}
