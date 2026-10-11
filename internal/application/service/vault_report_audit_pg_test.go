package service

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

func TestVaultReportAuditAndFailureRollback(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Minute)
	report, err := h.documents.CreateReport(h.ctx, "Successful report", "# Successful report\n\nAudit fixture report.", nil)
	require.NoError(t, err)
	var logs []types.OperationLog
	require.NoError(t, h.db.Find(&logs).Error)
	require.Len(t, logs, 1)
	require.Equal(t, "write_report", logs[0].OperationType)
	require.Equal(t, report.ID, logs[0].ObjectID)
	require.Equal(t, "web_user:personal-user", logs[0].Actor)
	var chunkCount, embeddingCount int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Count(&chunkCount).Error)
	require.NoError(t, h.db.Table("embeddings").Count(&embeddingCount).Error)
	require.Positive(t, embeddingCount)
	before := vaultFileHashes(t, h.root)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_report_audit() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF NEW.operation_type = 'write_report' THEN RAISE EXCEPTION 'fixture refuses report audit'; END IF;
  RETURN NEW;
END $$`).Error)
	require.NoError(t, h.db.Exec(`CREATE TRIGGER reject_report_audit BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_report_audit()`).Error)
	_, err = h.documents.CreateReport(h.ctx, "Failed audit report", "# Failed audit report\n\nThis report must be rolled back.", nil)
	require.ErrorContains(t, err, "fixture refuses report audit")
	require.Equal(t, before, vaultFileHashes(t, h.root), "audit failure removes only the newly created report file")
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source_kind='report'").Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Model(&types.Chunk{}).Count(&count).Error)
	require.Equal(t, chunkCount, count)
	require.NoError(t, h.db.Table("embeddings").Count(&count).Error)
	require.Equal(t, embeddingCount, count)
	require.Len(t, h.documents.observations, 1)
	require.NoError(t, h.db.Model(&types.OperationLog{}).Count(&count).Error)
	require.EqualValues(t, 1, count)
}

type reportFailedEmbeddingModels struct{ vaultDocumentModels }

func (reportFailedEmbeddingModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return nil, fmt.Errorf("fixture embedding is unavailable")
}

func TestVaultReportIndexFailureRetainsFileForRetry(t *testing.T) {
	h := newVaultDocumentHarness(t, time.Minute)
	models := h.documents.models
	h.documents.models = reportFailedEmbeddingModels{}
	_, err := h.documents.CreateReport(h.ctx, "Retained report", "# Retained report\n\nSource retained for a later indexing retry.", nil)
	require.ErrorContains(t, err, "report saved at")
	require.ErrorContains(t, err, "fixture embedding is unavailable")
	files := []string{}
	require.NoError(t, filepath.WalkDir(filepath.Join(h.root, "reports"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}))
	require.Len(t, files, 1)
	before := vaultFileHashes(t, h.root)
	h.documents.models = models
	require.NoError(t, h.documents.Scan(h.ctx, true))
	require.Equal(t, before, vaultFileHashes(t, h.root))
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source_kind='report' AND parse_status='completed'").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
