package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/datasource"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestProcessSyncCancelsWhenKnowledgeBaseDeleted(t *testing.T) {
	ds := &types.DataSource{
		ID:              "ds-1",
		TenantID:        1,
		KnowledgeBaseID: "kb-deleted",
		Type:            types.ConnectorTypeRSS,
		Status:          types.DataSourceStatusActive,
	}
	dsRepo := newKBDeleteDSRepo("kb-deleted", ds)
	syncLog := &types.SyncLog{
		ID:           "log-1",
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	syncLogRepo := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{syncLog.ID: syncLog}}

	svc := &DataSourceService{
		dsRepo:      dsRepo,
		syncLogRepo: syncLogRepo,
		kbService:   &processSyncKBService{getErr: apprepo.ErrKnowledgeBaseNotFound},
	}

	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		SyncLogID:    syncLog.ID,
	})
	require.NoError(t, err)

	err = svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))
	require.NoError(t, err)

	updated := syncLogRepo.logs[syncLog.ID]
	require.NotNil(t, updated)
	assert.Equal(t, types.SyncLogStatusCanceled, updated.Status)
	assert.Equal(t, "knowledge base has been deleted", updated.ErrorMessage)
	require.NotNil(t, updated.FinishedAt)
}

func TestProcessSyncDailySourceFailureKeepsSourceActiveForNextRun(t *testing.T) {
	for _, connectorType := range []string{types.ConnectorTypeRSS, types.ConnectorTypeGrokX} {
		t.Run(connectorType, func(t *testing.T) {
			ds := &types.DataSource{ID: connectorType + "-1", TenantID: 1, KnowledgeBaseID: "kb-1", Type: connectorType, Status: types.DataSourceStatusActive}
			logs := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{}}
			svc := &DataSourceService{
				dsRepo: newKBDeleteDSRepo(ds.KnowledgeBaseID, ds), syncLogRepo: logs,
				kbService:         &processSyncKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: ds.TenantID}},
				connectorRegistry: datasource.NewConnectorRegistry(),
			}
			for _, id := range []string{"first", "second"} {
				logs.logs[id] = &types.SyncLog{ID: id, DataSourceID: ds.ID, TenantID: ds.TenantID, Status: types.SyncLogStatusRunning}
				payload, err := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, TenantID: ds.TenantID, SyncLogID: id})
				require.NoError(t, err)
				require.Error(t, svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload)))
				assert.Equal(t, types.SyncLogStatusFailed, logs.logs[id].Status)
				assert.Equal(t, types.DataSourceStatusActive, ds.Status)
			}
		})
	}
}

type partialRSSConnector struct{ deletedItemConnector }

func (partialRSSConnector) Type() string { return types.ConnectorTypeRSS }
func (partialRSSConnector) FetchIncremental(context.Context, *types.DataSourceConfig, *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	return []types.FetchedItem{
		{ExternalID: "ok", Title: "Article", URL: "https://example.com/article", Content: []byte("Full article"), Metadata: map[string]string{"published_at": "2026-09-23", "publisher": "Publisher"}},
		{ExternalID: "failed", Title: "Missing article", Metadata: map[string]string{"error": "fetch failed"}},
	}, &types.SyncCursor{ConnectorCursor: map[string]interface{}{"revision": "new"}}, nil
}

func TestProcessSyncRSSPartialFailureKeepsCursorForRetry(t *testing.T) {
	configJSON, err := (&types.DataSourceConfig{Type: types.ConnectorTypeRSS}).ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{ID: "rss-partial", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeRSS,
		Config: configJSON, Status: types.DataSourceStatusActive, LastSyncCursor: types.JSON(`{"revision":"old"}`)}
	log := &types.SyncLog{ID: "rss-log", DataSourceID: ds.ID, TenantID: 1, Status: types.SyncLogStatusRunning}
	logs := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{log.ID: log}}
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(partialRSSConnector{}))
	svc := &DataSourceService{dsRepo: newKBDeleteDSRepo(ds.KnowledgeBaseID, ds), syncLogRepo: logs,
		kbService:         &processSyncKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: 1}},
		connectorRegistry: registry, tenantRepo: &processSyncTenantRepo{tenant: &types.Tenant{ID: 1}},
		tagService: &processSyncTagService{}, knowledgeService: &rssIntakeKnowledgeService{repo: &deletionLookupKnowledgeRepo{}},
	}
	payload, err := json.Marshal(types.DataSourceSyncPayload{DataSourceID: ds.ID, TenantID: 1, SyncLogID: log.ID})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload)))
	require.Equal(t, types.SyncLogStatusPartial, log.Status)
	require.Equal(t, types.JSON(`{"revision":"old"}`), ds.LastSyncCursor)
}

type processSyncKBService struct {
	getErr error
	kb     *types.KnowledgeBase
}

func (s *processSyncKBService) CreateKnowledgeBase(context.Context, *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *processSyncKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, s.getErr
}

func (s *processSyncKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, s.getErr
}

func (s *processSyncKBService) GetKnowledgeBasesByIDsOnly(context.Context, []string) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *processSyncKBService) FillKnowledgeBaseCounts(context.Context, *types.KnowledgeBase) error {
	return nil
}

func (s *processSyncKBService) ListKnowledgeBases(context.Context) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *processSyncKBService) ListKnowledgeBasesByTenantID(context.Context, uint64) ([]*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *processSyncKBService) UpdateKnowledgeBase(
	context.Context, string, string, string, *types.KnowledgeBaseConfig, ...string,
) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) DeleteKnowledgeBase(context.Context, string) error { return nil }
func (s *processSyncKBService) TogglePinKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}

func (s *processSyncKBService) HybridSearch(context.Context, string, types.SearchParams) ([]*types.SearchResult, error) {
	return nil, nil
}

func (s *processSyncKBService) GetQueryEmbedding(context.Context, string, string) ([]float32, error) {
	return nil, nil
}

func (s *processSyncKBService) ResolveEmbeddingModelKeys(context.Context, []*types.KnowledgeBase) map[string]string {
	return nil
}

func (s *processSyncKBService) CopyKnowledgeBase(context.Context, string, string) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	return nil, nil, nil
}

func (s *processSyncKBService) DuplicateKnowledgeBase(context.Context, string) (*types.KnowledgeBase, error) {
	return nil, nil
}
func (s *processSyncKBService) GetRepository() interfaces.KnowledgeBaseRepository { return nil }
func (s *processSyncKBService) ProcessKBDelete(context.Context, *asynq.Task) error {
	return nil
}

var _ interfaces.KnowledgeBaseService = (*processSyncKBService)(nil)

type processSyncSyncLogRepo struct {
	logs map[string]*types.SyncLog
}

func (r *processSyncSyncLogRepo) Create(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}

func (r *processSyncSyncLogRepo) FindByID(_ context.Context, id string) (*types.SyncLog, error) {
	log, ok := r.logs[id]
	if !ok {
		return nil, errors.New("sync log not found")
	}
	return log, nil
}

func (r *processSyncSyncLogRepo) FindByDataSource(context.Context, string, int, int) ([]*types.SyncLog, error) {
	return nil, nil
}

func (r *processSyncSyncLogRepo) FindLatest(context.Context, string) (*types.SyncLog, error) {
	return nil, nil
}

func (r *processSyncSyncLogRepo) HasRunningSync(context.Context, string) (bool, error) {
	return false, nil
}

func (r *processSyncSyncLogRepo) Update(_ context.Context, log *types.SyncLog) error {
	r.logs[log.ID] = log
	return nil
}

func (r *processSyncSyncLogRepo) UpdateResult(_ context.Context, log *types.SyncLog) error {
	return r.Update(context.Background(), log)
}

func (r *processSyncSyncLogRepo) CancelPendingByDataSource(context.Context, string) error {
	return nil
}
func (r *processSyncSyncLogRepo) CleanupOldLogs(context.Context, int) error { return nil }

func TestAllFetchedItemsFailedError(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  2,
		Failed: 2,
		Errors: []types.SyncItemError{{Message: "doc one: export failed"}},
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "all fetched items failed during sync (2/2)")
	assert.Contains(t, err.Error(), "doc one: export failed")
}

func TestAllFetchedItemsFailedErrorIgnoresPartialFailure(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Created: 1,
		Failed:  2,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorIgnoresSkippedItems(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:   3,
		Skipped: 3,
	})
	require.NoError(t, err)
}

func TestAllFetchedItemsFailedErrorTruncatesLongDetail(t *testing.T) {
	err := allFetchedItemsFailedError(&types.SyncResult{
		Total:  1,
		Failed: 1,
		Errors: []types.SyncItemError{{Message: strings.Repeat("x", 600)}},
	})
	require.Error(t, err)
	assert.LessOrEqual(t, len(err.Error()), 560)
	assert.Contains(t, err.Error(), "...")
}

const deletedItemConnectorType = "test-sync-deletion"

type deletedItemConnector struct{}

func (deletedItemConnector) Type() string { return deletedItemConnectorType }
func (deletedItemConnector) Validate(context.Context, *types.DataSourceConfig) error {
	return nil
}

func (deletedItemConnector) ListResources(context.Context, *types.DataSourceConfig, string) ([]types.Resource, error) {
	return nil, nil
}

func (deletedItemConnector) ResolveResourceAncestors(
	context.Context, *types.DataSourceConfig, []string,
) ([]string, error) {
	return nil, nil
}

func (deletedItemConnector) FetchAll(context.Context, *types.DataSourceConfig, []string) ([]types.FetchedItem, error) {
	return []types.FetchedItem{{
		ExternalID:       "file:gone",
		SourceResourceID: "folder:1",
		IsDeleted:        true,
	}}, nil
}

func (deletedItemConnector) FetchIncremental(
	context.Context, *types.DataSourceConfig, *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	items, err := (deletedItemConnector{}).FetchAll(context.Background(), nil, nil)
	return items, nil, err
}

type processSyncTenantRepo struct {
	interfaces.TenantRepository
	tenant *types.Tenant
}

func (r *processSyncTenantRepo) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return r.tenant, nil
}

type processSyncTagService struct {
	interfaces.KnowledgeTagService
}

func (*processSyncTagService) FindOrCreateTagByName(context.Context, string, string) (*types.KnowledgeTag, error) {
	return nil, nil
}

type deletionLookupKnowledgeRepo struct {
	interfaces.KnowledgeRepository
	knowledge         *types.Knowledge
	lookupErr         error
	metadataUpdates   []map[string]string // metadata persisted via UpdateKnowledge
	metadataUpdateErr error
	hardDeleted       []string
	hardDeleteErr     error
	tenantID          uint64
	knowledgeBaseID   string
	dataSourceID      string
	externalID        string
}

func (r *deletionLookupKnowledgeRepo) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	if r.metadataUpdateErr != nil {
		return r.metadataUpdateErr
	}
	metadata := map[string]string{}
	if len(knowledge.Metadata) > 0 {
		if err := json.Unmarshal(knowledge.Metadata, &metadata); err != nil {
			return err
		}
	}
	r.metadataUpdates = append(r.metadataUpdates, metadata)
	return nil
}

func (r *deletionLookupKnowledgeRepo) FindByDataSourceExternalID(
	_ context.Context, tenantID uint64, knowledgeBaseID, dataSourceID, externalID string,
) (*types.Knowledge, error) {
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	r.tenantID = tenantID
	r.knowledgeBaseID = knowledgeBaseID
	r.dataSourceID = dataSourceID
	r.externalID = externalID
	return r.knowledge, nil
}

func (r *deletionLookupKnowledgeRepo) HardDeleteKnowledge(_ context.Context, _ uint64, id string) error {
	if r.hardDeleteErr != nil {
		return r.hardDeleteErr
	}
	r.hardDeleted = append(r.hardDeleted, id)
	return nil
}

func (r *deletionLookupKnowledgeRepo) HardDeleteKnowledgeList(_ context.Context, _ uint64, ids []string) error {
	for _, id := range ids {
		if err := r.HardDeleteKnowledge(context.Background(), 0, id); err != nil {
			return err
		}
	}
	return nil
}

// scopedDeletionRepo models two data sources sharing the same external_id.
type scopedDeletionRepo struct {
	interfaces.KnowledgeRepository
	live        map[string]*types.Knowledge
	hardDeleted []string
}

func (r *scopedDeletionRepo) FindByDataSourceExternalID(
	_ context.Context, _ uint64, _, dataSourceID, externalID string,
) (*types.Knowledge, error) {
	if r.live == nil {
		return nil, nil
	}
	return r.live[dataSourceID+"|"+externalID], nil
}

func (r *scopedDeletionRepo) HardDeleteKnowledge(_ context.Context, _ uint64, id string) error {
	r.hardDeleted = append(r.hardDeleted, id)
	return nil
}

func (r *scopedDeletionRepo) HardDeleteKnowledgeList(_ context.Context, _ uint64, ids []string) error {
	r.hardDeleted = append(r.hardDeleted, ids...)
	return nil
}

// keyedDeletionRepo maps external_id to live knowledge for multi-item sync tests.
type keyedDeletionRepo struct {
	interfaces.KnowledgeRepository
	items         map[string]*types.Knowledge
	hardDeleted   []string
	hardDeleteErr error
}

func (r *keyedDeletionRepo) FindByDataSourceExternalID(
	_ context.Context, _ uint64, _, _, externalID string,
) (*types.Knowledge, error) {
	if r.items == nil {
		return nil, nil
	}
	return r.items[externalID], nil
}

func (r *keyedDeletionRepo) HardDeleteKnowledge(_ context.Context, _ uint64, id string) error {
	if r.hardDeleteErr != nil {
		return r.hardDeleteErr
	}
	r.hardDeleted = append(r.hardDeleted, id)
	return nil
}

func (r *keyedDeletionRepo) HardDeleteKnowledgeList(_ context.Context, _ uint64, ids []string) error {
	for _, id := range ids {
		if err := r.HardDeleteKnowledge(context.Background(), 0, id); err != nil {
			return err
		}
	}
	return nil
}

// syncDeletionHarness wires a DataSourceService around a connector that always
// reports one deleted item, with overridable lookup/delete fakes.
type syncDeletionHarness struct {
	ds            *types.DataSource
	syncLogID     string
	syncLogRepo   *processSyncSyncLogRepo
	knowledgeRepo *deletionLookupKnowledgeRepo
	knowledgeSvc  *sweepFakeKS
	svc           *DataSourceService
}

// newSyncDeletionHarness builds a full-sync ProcessSync fixture. Passing nil
// for repo/ks selects the happy-path defaults: an existing knowledge item and
// no lookup/delete errors.
func newSyncDeletionHarness(
	t *testing.T, syncDeletions bool, dsID, logID string,
	repo *deletionLookupKnowledgeRepo, ks *sweepFakeKS,
) *syncDeletionHarness {
	t.Helper()
	configJSON, err := (&types.DataSourceConfig{Type: deletedItemConnectorType}).ToJSON()
	require.NoError(t, err)

	ds := &types.DataSource{
		ID:              dsID,
		TenantID:        1,
		KnowledgeBaseID: "kb-1",
		Name:            "Sync Deletion",
		Type:            deletedItemConnectorType,
		Config:          configJSON,
		SyncMode:        types.SyncModeFull,
		Status:          types.DataSourceStatusActive,
		SyncDeletions:   syncDeletions,
	}
	syncLog := &types.SyncLog{
		ID:           logID,
		DataSourceID: ds.ID,
		TenantID:     ds.TenantID,
		Status:       types.SyncLogStatusRunning,
		StartedAt:    time.Now().UTC(),
	}
	if repo == nil {
		repo = &deletionLookupKnowledgeRepo{knowledge: &types.Knowledge{ID: "knowledge-gone"}}
	}
	if ks == nil {
		ks = &sweepFakeKS{repo: repo}
	}
	syncLogRepo := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{syncLog.ID: syncLog}}
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(deletedItemConnector{}))

	return &syncDeletionHarness{
		ds:            ds,
		syncLogID:     syncLog.ID,
		syncLogRepo:   syncLogRepo,
		knowledgeRepo: repo,
		knowledgeSvc:  ks,
		svc: &DataSourceService{
			dsRepo:            newKBDeleteDSRepo(ds.KnowledgeBaseID, ds),
			syncLogRepo:       syncLogRepo,
			knowledgeService:  ks,
			kbService:         &processSyncKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: ds.TenantID}},
			connectorRegistry: registry,
			tenantRepo:        &processSyncTenantRepo{tenant: &types.Tenant{ID: ds.TenantID}},
			tagService:        &processSyncTagService{},
		},
	}
}

// run executes a full sync and returns the persisted sync log plus the error
// ProcessSync returned (non-nil when every fetched item failed).
func (h *syncDeletionHarness) run(t *testing.T) (*types.SyncLog, error) {
	t.Helper()
	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: h.ds.ID,
		TenantID:     h.ds.TenantID,
		SyncLogID:    h.syncLogID,
		ForceFull:    true,
	})
	require.NoError(t, err)
	err = h.svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))

	updated := h.syncLogRepo.logs[h.syncLogID]
	require.NotNil(t, updated)
	return updated, err
}

// deletionFailedCount extracts the SyncResult.deletion_failed counter without
// referencing the SyncResult type, so these tests stay independent of the
// field's commit.
func deletionFailedCount(t *testing.T, log *types.SyncLog) int {
	t.Helper()
	var counters struct {
		DeletionFailed int `json:"deletion_failed"`
	}
	require.NoError(t, json.Unmarshal(log.Result, &counters))
	return counters.DeletionFailed
}

// TestProcessSync_SyncDeletionsDeletesMatchingKnowledge verifies that a
// deleted source item removes the matching KB knowledge (counted as Deleted),
// and that the lookup is scoped to tenant, KB, data source and external ID.
// TestIngestItem_URLCreationAttachesDataSourceMetadata verifies that a
// URL-only item gets its datasource scoping keys attached right after
// creation, so a later source-side deletion can find it via
// FindByDataSourceExternalID (CreateKnowledgeFromURL itself persists no
// metadata).
func TestIngestItem_URLCreationAttachesDataSourceMetadata(t *testing.T) {
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1"}
	repo := &deletionLookupKnowledgeRepo{}
	ks := &sweepFakeKS{repo: repo, createURLKnowledge: &types.Knowledge{ID: "url-knowledge-1"}}
	svc := &DataSourceService{knowledgeService: ks}

	isUpdate, err := svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "url:1",
		URL:        "https://example.com/doc",
	}, nil)
	require.NoError(t, err)
	assert.False(t, isUpdate)
	require.Len(t, repo.metadataUpdates, 1)
	assert.Equal(t, ds.ID, repo.metadataUpdates[0]["datasource_id"])
	assert.Equal(t, "url:1", repo.metadataUpdates[0]["external_id"])
}

type rssIntakeKnowledgeService struct {
	interfaces.KnowledgeService
	repo      interfaces.KnowledgeRepository
	payload   *types.ManualKnowledgePayload
	channel   string
	updatedID string
	rejected  bool
	updateErr error
}

func (s *rssIntakeKnowledgeService) GetRepository() interfaces.KnowledgeRepository { return s.repo }
func (s *rssIntakeKnowledgeService) CreateKnowledgeFromManual(_ context.Context, _ string, payload *types.ManualKnowledgePayload, channel string) (*types.Knowledge, error) {
	s.payload, s.channel = payload, channel
	return &types.Knowledge{ID: "received"}, nil
}
func (s *rssIntakeKnowledgeService) ReviewRSSUpdate(_ context.Context, oldID string, payload *types.ManualKnowledgePayload) (bool, error) {
	s.updatedID, s.payload = oldID, payload
	return s.rejected, s.updateErr
}

func TestIngestItem_RSSUsesUnifiedIntakeWithSourceEvidence(t *testing.T) {
	repo := &deletionLookupKnowledgeRepo{}
	ks := &rssIntakeKnowledgeService{repo: repo}
	svc := &DataSourceService{knowledgeService: ks}
	ds := &types.DataSource{ID: "rss-source", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeRSS}
	updated, err := svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Full original text"), URL: "https://example.com/article",
		Metadata: map[string]string{"author": "Original Author", "publisher": "Publishing Institution", "published_at": "2026-09-23T08:30:00Z", "channel": types.ChannelRSS},
	}, []string{"source-tag"})
	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, types.ManualKnowledgeStatusPublish, ks.payload.Status)
	require.Equal(t, "Full original text", ks.payload.Content)
	require.Equal(t, types.SourceInfo{Author: "Original Author", Publisher: "Publishing Institution", OriginalURL: "https://example.com/article", PublishedAt: "2026-09-23T08:30:00Z"}, ks.payload.SourceInfo)
	require.Equal(t, "rss-source", ks.payload.SourceMetadata["datasource_id"])
	require.NotEmpty(t, ks.payload.SourceMetadata["content_sha256"])
	require.Equal(t, []string{"source-tag"}, ks.payload.TagIDs)
	oldMetadata, err := json.Marshal(ks.payload.SourceMetadata)
	require.NoError(t, err)
	var stored map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(oldMetadata, &stored))
	stored["intake"] = json.RawMessage(`{"decision":"approved"}`)
	oldMetadata, err = json.Marshal(stored)
	require.NoError(t, err)
	repo.knowledge = &types.Knowledge{ID: "old", Metadata: types.JSON(oldMetadata)}
	_, err = svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Full original text"), URL: "https://example.com/article",
	}, nil)
	var duplicate *types.DuplicateKnowledgeError
	require.ErrorAs(t, err, &duplicate)
	updated, err = svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Changed original text"), URL: "https://example.com/article",
	}, nil)
	require.NoError(t, err)
	require.True(t, updated)
	require.Equal(t, "old", ks.updatedID)
	require.Equal(t, "Changed original text", ks.payload.Content)
	ks.rejected = true
	_, err = svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Changed original text"), URL: "https://example.com/article",
	}, nil)
	require.ErrorIs(t, err, errRSSUpdateRejected)
	result := &types.SyncResult{}
	svc.applyFetchedItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Changed original text"), URL: "https://example.com/article",
	}, nil, result)
	require.Equal(t, 1, result.Deleted)
	require.Len(t, result.Errors, 1)
	require.Equal(t, "rss_update_rejected", result.Errors[0].Code)
	ks.rejected, ks.updateErr = false, errors.New("model unavailable")
	_, err = svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "feed-item", Title: "Article", Content: []byte("Changed original text"), URL: "https://example.com/article",
	}, nil)
	require.ErrorContains(t, err, "model unavailable")
	require.Equal(t, types.ChannelRSS, ks.channel)
	repo.knowledge = nil
	_, err = svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "qwen:undated", Title: "Undated", Content: []byte("Original body"),
		Metadata: map[string]string{"publisher": "Qwen", "intake_reject_reason": "Qwen article missing publication date"},
	}, nil)
	require.NoError(t, err)
	require.Equal(t, "Qwen article missing publication date", ks.payload.SourceMetadata["intake_reject_reason"])
	require.Empty(t, ks.payload.SourceInfo.OriginalURL)
}

func TestIngestItem_GrokXUsesUnifiedIntakeWithSourceEvidence(t *testing.T) {
	ks := &rssIntakeKnowledgeService{repo: &deletionLookupKnowledgeRepo{}}
	svc := &DataSourceService{knowledgeService: ks}
	ds := &types.DataSource{ID: "grok-source", TenantID: 1, KnowledgeBaseID: "kb", Type: types.ConnectorTypeGrokX}
	updated, err := svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "123", Title: "@bcherny: Exact source text", Content: []byte("Exact source text\n\n![X image](https://pbs.twimg.com/media/example.jpg)"),
		URL: "https://x.com/bcherny/status/123", Metadata: map[string]string{"author": "@bcherny", "published_at": "2026-09-24T08:30:00Z", "channel": types.ConnectorTypeGrokX},
	}, nil)
	require.NoError(t, err)
	require.False(t, updated)
	require.Equal(t, types.ConnectorTypeGrokX, ks.channel)
	require.Equal(t, "@bcherny", ks.payload.SourceInfo.Author)
	require.Equal(t, "https://x.com/bcherny/status/123", ks.payload.SourceInfo.OriginalURL)
	require.Equal(t, "2026-09-24T08:30:00Z", ks.payload.SourceInfo.PublishedAt)
	require.Contains(t, ks.payload.Content, "![X image]")
}

func TestProcessSync_SyncDeletionsDeletesMatchingKnowledge(t *testing.T) {
	h := newSyncDeletionHarness(t, true, "ds-delete-characterization", "log-delete-characterization", nil, nil)
	updated, err := h.run(t)
	require.NoError(t, err)

	assert.Equal(t, 1, updated.ItemsDeleted)
	assert.Equal(t, []string{"knowledge-gone"}, h.knowledgeSvc.deleted)
	assert.Equal(t, h.ds.TenantID, h.knowledgeRepo.tenantID)
	assert.Equal(t, h.ds.KnowledgeBaseID, h.knowledgeRepo.knowledgeBaseID)
	assert.Equal(t, h.ds.ID, h.knowledgeRepo.dataSourceID)
	assert.Equal(t, "file:gone", h.knowledgeRepo.externalID)
}

// TestProcessSync_SyncDeletionsDisabledSkipsDeletion verifies that with
// SyncDeletions off the item is neither deleted nor counted (Deleted=0,
// Skipped=0, no DeleteKnowledge call).
func TestProcessSync_SyncDeletionsDisabledSkipsDeletion(t *testing.T) {
	h := newSyncDeletionHarness(t, false, "ds-delete-disabled", "log-delete-disabled", nil, nil)
	updated, err := h.run(t)
	require.NoError(t, err)

	assert.Empty(t, h.knowledgeSvc.deleted)
	assert.Equal(t, 0, updated.ItemsDeleted)
	assert.Equal(t, 0, updated.ItemsSkipped)
}

// TestProcessSync_SyncDeletionsAlreadyGoneCountsSkipped verifies the
// idempotent path: the source item reports deletion but no KB knowledge
// matches, so the item counts as Skipped and nothing is deleted.
func TestProcessSync_SyncDeletionsAlreadyGoneCountsSkipped(t *testing.T) {
	h := newSyncDeletionHarness(t, true, "ds-delete-gone", "log-delete-gone", &deletionLookupKnowledgeRepo{}, nil)
	updated, err := h.run(t)
	require.NoError(t, err)

	assert.Empty(t, h.knowledgeSvc.deleted)
	assert.Equal(t, 0, updated.ItemsDeleted)
	assert.Equal(t, 1, updated.ItemsSkipped)
}

// TestProcessSync_SyncDeletionsLookupFailureCountsFailed verifies that a
// failing scoped lookup surfaces as a Failed item with the
// deletion_lookup_failed code, increments DeletionFailed, and never calls
// DeleteKnowledge.
func TestProcessSync_SyncDeletionsLookupFailureCountsFailed(t *testing.T) {
	repo := &deletionLookupKnowledgeRepo{lookupErr: errors.New("lookup failed")}
	h := newSyncDeletionHarness(t, true, "ds-delete-lookup-fail", "log-delete-lookup-fail", repo, nil)
	updated, err := h.run(t)
	require.Error(t, err)

	assert.Empty(t, h.knowledgeSvc.deleted)
	assert.Equal(t, 0, updated.ItemsDeleted)
	assert.Equal(t, 1, updated.ItemsFailed)
	result, err := updated.ParseResult()
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "deletion_lookup_failed", result.Errors[0].Code)
	assert.Equal(t, 1, deletionFailedCount(t, updated))
}

// TestProcessSync_SyncDeletionsDeleteFailureCountsFailed verifies that a
// failing DeleteKnowledge call surfaces as a Failed item with the
// deletion_failed code and increments DeletionFailed without counting
// the item as Deleted.
func TestProcessSync_SyncDeletionsDeleteFailureCountsFailed(t *testing.T) {
	h := newSyncDeletionHarness(t, true, "ds-delete-fail", "log-delete-fail", nil, nil)
	h.knowledgeSvc.deleteErr = errors.New("delete failed")
	updated, err := h.run(t)
	require.Error(t, err)

	assert.Equal(t, []string{"knowledge-gone"}, h.knowledgeSvc.deleted)
	assert.Equal(t, 0, updated.ItemsDeleted)
	assert.Equal(t, 1, updated.ItemsFailed)
	result, err := updated.ParseResult()
	require.NoError(t, err)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "deletion_failed", result.Errors[0].Code)
	assert.Equal(t, 1, deletionFailedCount(t, updated))
}

func TestProcessSync_SyncDeletionsHardDeletesRow(t *testing.T) {
	h := newSyncDeletionHarness(t, true, "ds-delete-hard", "log-delete-hard", nil, nil)
	updated, err := h.run(t)
	require.NoError(t, err)

	assert.Equal(t, 1, updated.ItemsDeleted)
	assert.Equal(t, []string{"knowledge-gone"}, h.knowledgeSvc.deleted)
	assert.Equal(t, []string{"knowledge-gone"}, h.knowledgeRepo.hardDeleted)
}

func TestApplyFetchedItem_SyncDeletionScopedPerDataSource(t *testing.T) {
	repo := &scopedDeletionRepo{live: map[string]*types.Knowledge{
		"ds-a|file:shared": {ID: "knowledge-a"},
		"ds-b|file:shared": {ID: "knowledge-b"},
	}}
	ks := &sweepFakeKS{repo: repo}
	svc := &DataSourceService{knowledgeService: ks}

	result := &types.SyncResult{}
	dsA := &types.DataSource{
		ID: "ds-a", TenantID: 1, KnowledgeBaseID: "kb-1", SyncDeletions: true,
	}
	svc.applyFetchedItem(context.Background(), dsA, &types.FetchedItem{
		ExternalID: "file:shared",
		IsDeleted:  true,
	}, nil, result)

	assert.Equal(t, 1, result.Deleted)
	assert.Equal(t, []string{"knowledge-a"}, ks.deleted)
	assert.Equal(t, []string{"knowledge-a"}, repo.hardDeleted)
	assert.NotContains(t, ks.deleted, "knowledge-b")
}

type mixedSyncConnector struct{}

func (mixedSyncConnector) Type() string { return "test-sync-mixed" }
func (mixedSyncConnector) Validate(context.Context, *types.DataSourceConfig) error {
	return nil
}

func (mixedSyncConnector) ListResources(context.Context, *types.DataSourceConfig, string) ([]types.Resource, error) {
	return nil, nil
}

func (mixedSyncConnector) ResolveResourceAncestors(
	context.Context, *types.DataSourceConfig, []string,
) ([]string, error) {
	return nil, nil
}

func (mixedSyncConnector) FetchAll(context.Context, *types.DataSourceConfig, []string) ([]types.FetchedItem, error) {
	return []types.FetchedItem{
		{ExternalID: "file:gone", IsDeleted: true},
		{ExternalID: "file:new", Content: []byte("hello"), FileName: "new.txt"},
	}, nil
}

func (mixedSyncConnector) FetchIncremental(
	context.Context, *types.DataSourceConfig, *types.SyncCursor,
) ([]types.FetchedItem, *types.SyncCursor, error) {
	items, err := (mixedSyncConnector{}).FetchAll(context.Background(), nil, nil)
	return items, nil, err
}

func TestProcessSync_SyncDeletionsPartialWhenMixedResults(t *testing.T) {
	configJSON, err := (&types.DataSourceConfig{Type: "test-sync-mixed"}).ToJSON()
	require.NoError(t, err)

	ds := &types.DataSource{
		ID: "ds-mixed", TenantID: 1, KnowledgeBaseID: "kb-1",
		Type: "test-sync-mixed", Config: configJSON,
		SyncMode: types.SyncModeFull, Status: types.DataSourceStatusActive,
		SyncDeletions: true,
	}
	syncLog := &types.SyncLog{
		ID: "log-mixed", DataSourceID: ds.ID, TenantID: ds.TenantID,
		Status: types.SyncLogStatusRunning, StartedAt: time.Now().UTC(),
	}
	repo := &keyedDeletionRepo{
		items:         map[string]*types.Knowledge{"file:gone": {ID: "knowledge-gone"}},
		hardDeleteErr: errors.New("hard delete failed"),
	}
	ks := &sweepFakeKS{repo: repo}
	syncLogRepo := &processSyncSyncLogRepo{logs: map[string]*types.SyncLog{syncLog.ID: syncLog}}
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(mixedSyncConnector{}))

	svc := &DataSourceService{
		dsRepo:            newKBDeleteDSRepo(ds.KnowledgeBaseID, ds),
		syncLogRepo:       syncLogRepo,
		knowledgeService:  ks,
		kbService:         &processSyncKBService{kb: &types.KnowledgeBase{ID: ds.KnowledgeBaseID, TenantID: ds.TenantID}},
		connectorRegistry: registry,
		tenantRepo:        &processSyncTenantRepo{tenant: &types.Tenant{ID: ds.TenantID}},
		tagService:        &processSyncTagService{},
	}

	payload, err := json.Marshal(types.DataSourceSyncPayload{
		DataSourceID: ds.ID, TenantID: ds.TenantID, SyncLogID: syncLog.ID, ForceFull: true,
	})
	require.NoError(t, err)
	err = svc.ProcessSync(context.Background(), asynq.NewTask(types.TypeDataSourceSync, payload))
	require.NoError(t, err, "partial failure must not fail the whole sync")

	updated := syncLogRepo.logs[syncLog.ID]
	require.NotNil(t, updated)
	assert.Equal(t, types.SyncLogStatusPartial, updated.Status)
	assert.Equal(t, 1, updated.ItemsFailed)
	assert.Equal(t, 1, updated.ItemsCreated)
	assert.Contains(t, updated.ErrorMessage, "deletion failure(s) will only retry on the next full sync")
}

func TestIngestItem_URLCreationMetadataAttachFailure(t *testing.T) {
	ds := &types.DataSource{ID: "ds-1", TenantID: 1, KnowledgeBaseID: "kb-1"}
	repo := &deletionLookupKnowledgeRepo{metadataUpdateErr: errors.New("db unavailable")}
	ks := &sweepFakeKS{repo: repo, createURLKnowledge: &types.Knowledge{ID: "url-knowledge-1"}}
	svc := &DataSourceService{knowledgeService: ks}

	_, err := svc.ingestItem(context.Background(), ds, &types.FetchedItem{
		ExternalID: "url:1",
		URL:        "https://example.com/doc",
	}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "attach datasource metadata")
}
