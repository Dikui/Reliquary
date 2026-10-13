package service

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	"reliquary/internal/models/chat"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ingestRollbackModel struct {
	*articleProcessingChat
	failures int
}

func (m *ingestRollbackModel) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	for _, message := range messages {
		if strings.Contains(message.Content, "rollbackmodelmarker") {
			m.failures++
			return nil, errors.New("fixture ingest model unavailable")
		}
	}
	return m.articleProcessingChat.Chat(ctx, messages, options)
}

func ingestRollbackCounts(t *testing.T, db *gorm.DB) map[string]int64 {
	t.Helper()
	counts := make(map[string]int64)
	for _, table := range []string{"entries", "resources", "resource_bindings", "entry_repost_sources", "operation_logs", "task_jobs", "ingest_assets"} {
		var count int64
		require.NoError(t, db.Table(table).Count(&count).Error)
		counts[table] = count
	}
	return counts
}

func ingestRollbackBytes(t *testing.T, roots ...string) map[string][]byte {
	t.Helper()
	files := make(map[string][]byte)
	for _, root := range roots {
		require.NoError(t, filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil || entry.IsDir() {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			files[path] = body
			return nil
		}))
	}
	return files
}

func TestIngestInternalErrorRollsBackCatalogFilesAssetsAndJobs(t *testing.T) {
	for _, failure := range []string{"audit", "model"} {
		t.Run(failure, func(t *testing.T) {
			h, vault := newIngestHarness(t)
			h.db.Logger = h.db.Logger.LogMode(logger.Info)
			t.Cleanup(func() { h.db.Logger = h.db.Logger.LogMode(logger.Silent) })
			root := t.TempDir()
			catalog := NewResourceCatalog(repository.NewResourceRepository(h.db))
			h.service.resourceCatalog = catalog
			h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(root, ""), catalog)
			h.service.task = taskqueue.NewClient(h.db)
			asset, err := h.service.UploadIngestAsset(h.ctx, "retained-original.txt", []byte("Original source attachment retained across an unsuccessful delivery."))
			require.NoError(t, err)
			var uploaded types.IngestAsset
			require.NoError(t, h.db.First(&uploaded, "id=?", asset.AssetID).Error)
			require.Nil(t, uploaded.ReferencedAt)
			require.Empty(t, uploaded.EntryID)
			baselineCounts := ingestRollbackCounts(t, h.db)
			baselineFiles := ingestRollbackBytes(t, root, vault.root)
			require.EqualValues(t, 1, baselineCounts["resources"])
			require.EqualValues(t, 1, baselineCounts["task_jobs"])
			require.Len(t, baselineFiles, 1)

			request := types.IngestRequest{URL: "https://example.org/rollback-internal-" + failure, Acquisition: "model_written", Publisher: "Acme", Title: "失败后材料与任务回滚",
				Content: "Acme reports independent spectrometer observations, calibration settings and verified physical measurements. rollbackmodelmarker",
				File:    &types.IngestFile{AssetID: asset.AssetID},
				Images:  []types.IngestImage{{Data: strings.SplitN(articleImageDataURI(t, 82), ",", 2)[1], Caption: "Supplied measurement"}},
				Missing: []types.IngestMissing{{Type: "image", SourceURL: "https://example.org/unavailable-chart.png", Reason: "外部工具未取得原始图片文件"}}}
			var model *ingestRollbackModel
			if failure == "audit" {
				require.NoError(t, h.db.Exec(`CREATE SEQUENCE ingest_rollback_audit_reached;
CREATE FUNCTION fail_ingest_rollback_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.operation_type = 'ingest' AND NEW.after_value->>'url' = 'https://example.org/rollback-internal-audit' THEN
    IF NOT EXISTS (SELECT 1 FROM entries WHERE id = NEW.object_id) OR NOT EXISTS (SELECT 1 FROM ingest_assets WHERE entry_id = NEW.object_id AND referenced_at IS NOT NULL) THEN
      RAISE EXCEPTION 'fixture did not reach article and asset writes';
    END IF;
    PERFORM nextval('ingest_rollback_audit_reached');
    RAISE EXCEPTION 'fixture ingest audit unavailable';
  END IF;
  RETURN NEW;
END $$;
CREATE TRIGGER fail_ingest_rollback_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION fail_ingest_rollback_audit()`).Error)
			} else {
				model = &ingestRollbackModel{articleProcessingChat: h.processor}
				models := h.service.modelService.(articleProcessingModels)
				models.summaryStateModelService = &summaryStateModelService{model: model, metadata: models.metadata}
				h.service.modelService = models
			}

			result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{request}})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			require.Equal(t, "invalid", result.Items[0].Status)
			require.Equal(t, "内部处理失败", result.Items[0].Reason)
			require.Empty(t, result.Items[0].EntryID, "a failed item cannot expose an article that did not commit")
			t.Logf("failed result: %+v", result.Items[0])
			if model != nil {
				require.Positive(t, model.failures, "failure must come from the injected review model")
			} else {
				var reached bool
				require.NoError(t, h.db.Raw("SELECT is_called FROM ingest_rollback_audit_reached").Scan(&reached).Error)
				require.True(t, reached, "the audit failure must happen after the article and asset writes")
			}
			require.Equal(t, baselineCounts, ingestRollbackCounts(t, h.db), "all durable collection writes roll back")
			require.Equal(t, baselineFiles, ingestRollbackBytes(t, root, vault.root), "failed collection keeps previous bytes and removes every newly stored file")
			var retained types.IngestAsset
			require.NoError(t, h.db.First(&retained, "id=?", asset.AssetID).Error)
			require.Equal(t, uploaded, retained, "an uploaded asset remains unreferenced and can be retried")

			valid := request
			valid.URL = "https://example.org/rollback-recovery-" + failure
			valid.Content = "The housing committee publishes regional statistics, occupancy totals and its original survey methodology with reviewed demographic estimates."
			result, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{valid}})
			require.NoError(t, err)
			require.Len(t, result.Items, 1)
			require.Equal(t, "accepted", result.Items[0].Status, "an internal failure does not poison subsequent ingestion")
			require.NotEmpty(t, result.Items[0].EntryID)
			require.NoError(t, h.db.First(&retained, "id=?", asset.AssetID).Error)
			require.NotNil(t, retained.ReferencedAt)
			require.Equal(t, result.Items[0].EntryID, retained.EntryID)
			counts := ingestRollbackCounts(t, h.db)
			require.EqualValues(t, 1, counts["entries"])
			require.EqualValues(t, 1, counts["operation_logs"])
			require.EqualValues(t, 2, counts["task_jobs"])
			t.Logf("recovered result: %+v; committed counts: %+v", result.Items[0], counts)
		})
	}
}

type ingestRollbackDeleteFile struct {
	interfaces.FileService
	failNext bool
	failures int
}

func (s *ingestRollbackDeleteFile) DeleteFile(ctx context.Context, path string) error {
	if s.failNext {
		s.failNext = false
		s.failures++
		return errors.New("fixture physical file deletion unavailable")
	}
	return s.FileService.DeleteFile(ctx, path)
}

func TestIngestDuplicateCleanupErrorRollsBackAndRetrySucceeds(t *testing.T) {
	h, vault := newIngestHarness(t)
	h.db.Logger = h.db.Logger.LogMode(logger.Info)
	t.Cleanup(func() { h.db.Logger = h.db.Logger.LogMode(logger.Silent) })
	root := t.TempDir()
	physical := &ingestRollbackDeleteFile{FileService: file.NewLocalFileService(root, "")}
	catalog := NewResourceCatalog(repository.NewResourceRepository(h.db))
	h.service.resourceCatalog = catalog
	h.service.fileSvc = file.NewResourceCatalogFileService(physical, catalog)
	h.service.task = taskqueue.NewClient(h.db)
	original := types.IngestRequest{URL: "https://example.org/rollback-canonical", Acquisition: "verbatim", Publisher: "Acme",
		Content: "The observatory publishes original stellar measurements, instrument configuration, exposure durations and the calibration process applied to its independently recorded observations."}
	accepted, err := h.service.Ingest(h.ctx, &original)
	require.NoError(t, err)
	require.Equal(t, "accepted", accepted.Status)
	asset, err := h.service.UploadIngestAsset(h.ctx, "duplicate-original.txt", []byte("Original attachment provided for a duplicate delivery."))
	require.NoError(t, err)
	var uploaded types.IngestAsset
	require.NoError(t, h.db.First(&uploaded, "id=?", asset.AssetID).Error)
	require.Nil(t, uploaded.ReferencedAt)
	baselineCounts := ingestRollbackCounts(t, h.db)
	baselineFiles := ingestRollbackBytes(t, root, vault.root)
	require.EqualValues(t, 1, baselineCounts["entries"])
	require.EqualValues(t, 2, baselineCounts["resources"])
	require.EqualValues(t, 2, baselineCounts["task_jobs"])
	require.Len(t, baselineFiles, 2)
	beforeArticle := h.article(t, accepted.EntryID)

	duplicate := original
	duplicate.URL = "https://example.org/rollback-repost"
	duplicate.Acquisition = "model_written"
	duplicate.CollectReason = "收藏理由更新也必须随失败回滚"
	duplicate.File = &types.IngestFile{AssetID: asset.AssetID}
	duplicate.Missing = []types.IngestMissing{{Type: "image", SourceURL: "https://example.org/duplicate-chart.png", Reason: "图片文件无法获取"}}
	physical.failNext = true
	result, err := h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{duplicate}})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "invalid", result.Items[0].Status)
	require.Equal(t, "内部处理失败", result.Items[0].Reason)
	require.Empty(t, result.Items[0].EntryID)
	t.Logf("duplicate cleanup failed result: %+v", result.Items[0])
	require.Equal(t, baselineCounts, ingestRollbackCounts(t, h.db), "a cleanup failure rolls back the repost, operation log and new resource identities")
	require.Equal(t, baselineFiles, ingestRollbackBytes(t, root, vault.root), "rollback removes only new duplicate delivery files")
	require.Equal(t, beforeArticle, h.article(t, accepted.EntryID), "the failed duplicate cannot modify the previously collected article")
	require.Equal(t, 1, physical.failures, "the deliberate failure must occur in the physical file driver")
	var retained types.IngestAsset
	require.NoError(t, h.db.First(&retained, "id=?", asset.AssetID).Error)
	require.Equal(t, uploaded, retained)

	result, err = h.service.IngestBatch(h.ctx, &types.IngestBatchRequest{Items: []types.IngestRequest{duplicate}})
	require.NoError(t, err)
	require.Len(t, result.Items, 1)
	require.Equal(t, "exists", result.Items[0].Status)
	require.Equal(t, accepted.EntryID, result.Items[0].EntryID)
	require.Equal(t, duplicate.CollectReason, h.article(t, accepted.EntryID).CollectReason)
	require.Equal(t, baselineFiles, ingestRollbackBytes(t, root, vault.root))
	require.NoError(t, h.db.First(&retained, "id=?", asset.AssetID).Error)
	require.Equal(t, uploaded, retained, "a duplicate must not claim the uploaded source asset")
	counts := ingestRollbackCounts(t, h.db)
	require.Equal(t, baselineCounts["entries"], counts["entries"])
	require.Equal(t, baselineCounts["task_jobs"], counts["task_jobs"])
	require.Equal(t, baselineCounts["entry_repost_sources"]+1, counts["entry_repost_sources"])
	require.Equal(t, baselineCounts["operation_logs"]+1, counts["operation_logs"])
	t.Logf("duplicate cleanup recovery result: %+v; committed counts: %+v", result.Items[0], counts)
}
