package service

import (
	"bytes"
	"context"
	"encoding/json"

	"os"
	"path/filepath"

	"testing"
	"time"

	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func correctionIndexCounts(t *testing.T, h *articleProcessingHarness, id string) (int64, int64, int64) {
	t.Helper()
	var chunks, bodyVectors, articleVectors int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", id).Count(&chunks).Error)
	require.NoError(t, h.db.Table("embeddings").Where("entry_id=?", id).Count(&bodyVectors).Error)
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", id).Count(&articleVectors).Error)
	return chunks, bodyVectors, articleVectors
}

func correctionLogs(t *testing.T, h *articleProcessingHarness, id string) []types.OperationLog {
	t.Helper()
	var logs []types.OperationLog
	require.NoError(t, h.db.Where("object_id=? AND operation_type=?", id, "correct_content_type").Order("created_at,id").Find(&logs).Error)
	return logs
}

func correctionLogValue(t *testing.T, data types.JSON, key string) any {
	t.Helper()
	var value map[string]any
	require.NoError(t, json.Unmarshal(data, &value))
	return value[key]
}

func waitCorrectionHeader(t *testing.T, v *VaultService, k *types.Entry, contentType string) {
	t.Helper()
	require.Eventually(t, func() bool {
		if err := v.RefreshHeader(context.Background(), k.ID); err != nil {
			return false
		}
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && bytes.Contains(data, []byte("content_type: \""+contentType+"\""))
	}, 3*time.Second, 10*time.Millisecond)
}

func uploadedCorrectionSummary(t *testing.T, h *articleProcessingHarness, url string) *types.Entry {
	t.Helper()
	h.processor.output = validProcessingFixture("tutorial")
	k, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "summary-original.md", "A complete original publication contains setup guidance and explains supported deployment instructions."), map[string]string{"publisher": "Fixture publisher", "original_url": url, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	return h.article(t, k.ID)
}

func TestContentTypeConversionRestoresBodyAndAttachmentsWhenLogTransactionFails(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "restore-body", "An original announcement with measurements.\n\n![figure]("+articleImageDataURI(t, 60)+")")
	before, _, _ := readVaultArticle(t, v, k)
	attachmentDir := filepath.Join(v.root, "attachments", k.ID)
	attachments, err := os.ReadDir(attachmentDir)
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_fixture_conversion_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture operation log unavailable'; END; $$; CREATE TRIGGER reject_fixture_conversion_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_fixture_conversion_log()`).Error)
	_, err = h.service.CorrectContentType(context.WithValue(h.ctx, types.UserIDContextKey, "fixture-user"), k.ID, "tutorial")
	require.ErrorContains(t, err, "fixture operation log unavailable")
	after := h.article(t, k.ID)
	require.Equal(t, k.ContentType, after.ContentType)
	require.False(t, after.ContentTypeCorrected)
	data, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, data)
	afterAttachments, err := os.ReadDir(attachmentDir)
	require.NoError(t, err)
	require.Equal(t, len(attachments), len(afterAttachments))
	chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, k.ID)
	require.Positive(t, chunks)
	require.Positive(t, bodyVectors)
	require.EqualValues(t, 1, articleVectors)
	require.Empty(t, correctionLogs(t, h, k.ID))
}
