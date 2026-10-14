package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestSnapshotPauseSkipsAutomaticHousekeepingUntilResume(t *testing.T) {
	db := setupHousekeepingDB(t)
	svc := newHousekeepingSvcForTest(db)
	insertEntry(t, db, "waiting-credentials-orphan", types.ParseStatusProcessing, time.Now().Add(-4*time.Hour))
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	var original, retained string
	require.NoError(t, db.Raw("SELECT to_jsonb(k)::text FROM entries k WHERE id='waiting-credentials-orphan'").Scan(&original).Error)
	svc.runSweep(context.Background())
	require.NoError(t, db.Raw("SELECT to_jsonb(k)::text FROM entries k WHERE id='waiting-credentials-orphan'").Scan(&retained).Error)
	require.JSONEq(t, original, retained)
	require.NoError(t, db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	svc.runSweep(context.Background())
	var status string
	require.NoError(t, db.Raw("SELECT parse_status FROM entries WHERE id='waiting-credentials-orphan'").Scan(&status).Error)
	require.Equal(t, types.ParseStatusFailed, status)
}

func TestSnapshotPauseSkipsAutomaticReportScanUntilResume(t *testing.T) {
	h := newVaultDocumentHarness(t, 25*time.Millisecond)
	models := h.documents.models
	h.documents.models = reportFailedEmbeddingModels{}
	report, err := h.documents.CreateReport(h.ctx, "Waiting for credentials", "# Waiting for credentials\n\nKeep this exact original document.", nil)
	require.NoError(t, err)
	h.documents.models = models
	// The fallback scan only owns reports without a durable index operation.
	require.NoError(t, h.db.Exec("DELETE FROM task_pending_ops WHERE task_type=?", types.TypeReportIndex).Error)
	require.NoError(t, h.db.Exec("UPDATE maintenance_state SET paused=true,pause_mode='snapshot' WHERE id=true").Error)
	require.NoError(t, h.documents.Start(h.ctx))
	time.Sleep(125 * time.Millisecond)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Where("source_kind='report' AND parse_status='completed'").Count(&count).Error)
	require.Zero(t, count)
	require.Zero(t, h.embedder.inputCount())
	require.NoError(t, h.db.Exec("UPDATE maintenance_state SET paused=false,pause_mode='' WHERE id=true").Error)
	require.Eventually(t, func() bool {
		return h.db.Model(&types.Entry{}).Where("source_kind='report' AND parse_status='completed' AND vault_path=?", report.VaultPath).Count(&count).Error == nil && count == 1 && h.embedder.inputCount() > 0
	}, 2*time.Second, 25*time.Millisecond)
}
