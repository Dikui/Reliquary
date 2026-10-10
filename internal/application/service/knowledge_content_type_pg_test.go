package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func correctionIndexCounts(t *testing.T, h *articleProcessingHarness, id string) (int64, int64, int64) {
	t.Helper()
	var chunks, bodyVectors, articleVectors int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("knowledge_id=?", id).Count(&chunks).Error)
	require.NoError(t, h.db.Table("embeddings").Where("knowledge_id=?", id).Count(&bodyVectors).Error)
	require.NoError(t, h.db.Table("article_embeddings").Where("knowledge_id=?", id).Count(&articleVectors).Error)
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

func waitCorrectionHeader(t *testing.T, v *VaultService, k *types.Knowledge, contentType string) {
	t.Helper()
	require.Eventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(v.root, k.VaultPath))
		return err == nil && bytes.Contains(data, []byte("content_type: \""+contentType+"\""))
	}, 3*time.Second, 10*time.Millisecond)
}

func TestContentTypeCorrectionsWithinEachStorageModeKeepBodyAndIndexes(t *testing.T) {
	for _, fixture := range []struct{ before, after string }{{"event", "research"}, {"tutorial", "reference"}} {
		t.Run(fixture.before+"_to_"+fixture.after, func(t *testing.T) {
			h, v := newVaultArticleHarness(t)
			h.processor.output = validProcessingFixture(fixture.before)
			k := h.upload(t, "same-mode", "An original publication explains measured changes in supported deployment.\n\n![figure]("+articleImageDataURI(t, 50)+")")
			data, _, beforeBody := readVaultArticle(t, v, k)
			beforeChunks, beforeBodyVectors, beforeArticleVectors := correctionIndexCounts(t, h, k.ID)
			calls := len(h.processor.requests)
			require.Empty(t, correctionLogs(t, h, k.ID), "automatic processing must not emit correction logs")
			updated, err := h.service.CorrectContentType(h.ctx, k.ID, fixture.after, false)
			require.NoError(t, err)
			require.Equal(t, fixture.after, updated.ContentType)
			require.True(t, updated.ContentTypeCorrected)
			require.Equal(t, k.ProcessingMode, updated.ProcessingMode)
			require.Equal(t, calls, len(h.processor.requests), "a correction within one storage mode does not call a model")
			require.Equal(t, k.VaultBody, updated.VaultBody)
			require.Equal(t, k.VaultOriginalPath, updated.VaultOriginalPath)
			waitCorrectionHeader(t, v, updated, fixture.after)
			afterData, _, afterBody := readVaultArticle(t, v, updated)
			require.NotEqual(t, data, afterData)
			require.Equal(t, beforeBody, afterBody)
			chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, k.ID)
			require.Equal(t, beforeChunks, chunks)
			require.Equal(t, beforeBodyVectors, bodyVectors)
			require.Equal(t, beforeArticleVectors, articleVectors)
			logs := correctionLogs(t, h, k.ID)
			require.Len(t, logs, 1)
			require.Equal(t, fixture.before, correctionLogValue(t, logs[0].Before, "content_type"))
			require.Equal(t, fixture.after, correctionLogValue(t, logs[0].After, "content_type"))
		})
	}
}

func TestContentTypeFullToSummaryRequiresConfirmationAndRemovesBodyFiles(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "remove-body", "An original product announcement with detailed measurements.\n\n![figure]("+articleImageDataURI(t, 55)+")")
	before, _, _ := readVaultArticle(t, v, k)
	attachmentDir := filepath.Join(v.root, "attachments", k.ID)
	_, err := os.Stat(attachmentDir)
	require.NoError(t, err)
	_, err = h.service.CorrectContentType(h.ctx, k.ID, "case", false)
	require.ErrorContains(t, err, "confirm_remove_body")
	unchanged := h.article(t, k.ID)
	require.Equal(t, k.ContentType, unchanged.ContentType)
	require.False(t, unchanged.ContentTypeCorrected)
	data, _, _ := readVaultArticle(t, v, k)
	require.Equal(t, before, data)
	require.Empty(t, correctionLogs(t, h, k.ID))
	ctx := types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "fixture-key"})
	updated, err := h.service.CorrectContentType(ctx, k.ID, "case", true)
	require.NoError(t, err)
	require.Equal(t, types.ProcessingModeSummaryOnly, updated.ProcessingMode)
	require.Equal(t, "case", updated.ContentType)
	require.True(t, updated.ContentTypeCorrected)
	require.Empty(t, updated.VaultBody)
	require.Empty(t, updated.VaultOriginalPath)
	require.Empty(t, updated.FilePath)
	require.Zero(t, updated.StorageSize)
	data, header, body := readVaultArticle(t, v, updated)
	require.Equal(t, header, data)
	require.Empty(t, body)
	_, err = os.Stat(attachmentDir)
	require.ErrorIs(t, err, os.ErrNotExist)
	chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, k.ID)
	require.Zero(t, chunks)
	require.Zero(t, bodyVectors)
	require.EqualValues(t, 1, articleVectors, "the summary vector remains available")
	require.Empty(t, h.service.fileSvc.(*articleProcessingFiles).saved, "the source and processing attachments must be removed")
	logs := correctionLogs(t, h, k.ID)
	require.Len(t, logs, 1)
	require.Equal(t, "api_key:fixture-key", logs[0].Actor)
	require.Equal(t, "full", correctionLogValue(t, logs[0].Before, "processing_mode"))
	require.Equal(t, "summary_only", correctionLogValue(t, logs[0].After, "processing_mode"))
}

func localCorrectionSource(t *testing.T, status *int) *httptest.Server {
	t.Helper()
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if status != nil && *status != http.StatusOK {
			w.WriteHeader(*status)
			fmt.Fprint(w, "fixture original unavailable")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Fresh original</title></head><body><article><h1>Fresh original</h1><p>`+strings.Repeat("refetchedfullbody An original article describes measured product improvements and experimental deployment results. ", 15)+`</p></article></body></html>`)
	}))
	t.Cleanup(server.Close)
	return server
}

func uploadedCorrectionSummary(t *testing.T, h *articleProcessingHarness, url string) *types.Knowledge {
	t.Helper()
	h.processor.output = validProcessingFixture("tutorial")
	k, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID, newMultipartFileHeader(t, "summary-original.md", "A complete original publication contains setup guidance and explains supported deployment instructions."), map[string]string{"publisher": "Fixture publisher", "original_url": url, "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	return h.article(t, k.ID)
}

func TestContentTypeSummaryToFullRefetchesAndPreservesUserTypeOnReprocess(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	server := localCorrectionSource(t, nil)
	k := uploadedCorrectionSummary(t, h, server.URL)
	require.Empty(t, k.FilePath)
	h.processor.output = validProcessingFixture("case")
	updated, err := h.service.CorrectContentType(h.ctx, k.ID, "research", false)
	require.NoError(t, err)
	require.Equal(t, "research", updated.ContentType, "the model's case classification cannot overwrite the user correction")
	require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
	require.True(t, updated.ContentTypeCorrected)
	require.Equal(t, types.ParseStatusCompleted, updated.ParseStatus)
	require.Equal(t, k.VaultPath, updated.VaultPath)
	_, _, body := readVaultArticle(t, v, updated)
	require.Contains(t, string(body), "refetchedfullbody")
	require.Equal(t, updated.VaultBody, string(body))
	chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, k.ID)
	require.Positive(t, chunks)
	require.Positive(t, bodyVectors)
	require.EqualValues(t, 1, articleVectors)
	require.Len(t, correctionLogs(t, h, k.ID), 1)
	h.processor.output = validProcessingFixture("reference")
	_, err = h.service.ReparseKnowledge(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	reprocessed := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, reprocessed.ParseStatus)
	require.Equal(t, "research", reprocessed.ContentType)
	require.True(t, reprocessed.ContentTypeCorrected)
	require.Equal(t, types.ProcessingModeFull, reprocessed.ProcessingMode)
	_, _, afterBody := readVaultArticle(t, v, reprocessed)
	require.Equal(t, body, afterBody)
	require.Len(t, correctionLogs(t, h, k.ID), 1, "automatic processing must not create operation logs")
}

func TestContentTypeSummaryCorrectionSurvivesLaterRefetch(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	server := localCorrectionSource(t, nil)
	k := uploadedCorrectionSummary(t, h, server.URL)
	_, err := h.service.CorrectContentType(h.ctx, k.ID, "reference", false)
	require.NoError(t, err)
	h.processor.output = validProcessingFixture("event")
	_, err = h.service.ReparseKnowledge(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	reprocessed := h.article(t, k.ID)
	require.Equal(t, "reference", reprocessed.ContentType)
	require.Equal(t, types.ProcessingModeSummaryOnly, reprocessed.ProcessingMode)
	require.True(t, reprocessed.ContentTypeCorrected)
	waitCorrectionHeader(t, v, reprocessed, "reference")
	data, header, body := readVaultArticle(t, v, reprocessed)
	require.Equal(t, header, data)
	require.Empty(t, body)
	require.Len(t, correctionLogs(t, h, k.ID), 1)
}

func TestContentTypeConversionFetchModelAndIndexFailureKeepOriginalState(t *testing.T) {
	for _, failure := range []string{"fetch", "model", "index"} {
		t.Run(failure, func(t *testing.T) {
			h, v := newVaultArticleHarness(t)
			status := http.StatusOK
			server := localCorrectionSource(t, &status)
			k := uploadedCorrectionSummary(t, h, server.URL)
			before, _, _ := readVaultArticle(t, v, k)
			wantError := ""
			switch failure {
			case "fetch":
				status, wantError = http.StatusServiceUnavailable, "503"
			case "model":
				h.processor.output, wantError = "invalid JSON", "valid JSON"
			case "index":
				h.processor.output = validProcessingFixture("case")
				models := h.service.modelService.(articleProcessingModels)
				models.embeddingErr = errors.New("fixture index unavailable")
				h.service.modelService, wantError = models, "fixture index unavailable"
			}
			_, err := h.service.CorrectContentType(h.ctx, k.ID, "research", false)
			require.ErrorContains(t, err, wantError)
			after := h.article(t, k.ID)
			require.Equal(t, k.ContentType, after.ContentType)
			require.False(t, after.ContentTypeCorrected)
			require.Equal(t, k.ProcessingMode, after.ProcessingMode)
			require.Equal(t, k.ParseStatus, after.ParseStatus)
			require.Equal(t, k.ErrorMessage, after.ErrorMessage)
			require.Equal(t, k.StructuredSummary, after.StructuredSummary)
			require.Equal(t, k.Metadata, after.Metadata)
			require.Equal(t, k.UpdatedAt, after.UpdatedAt)
			data, _, _ := readVaultArticle(t, v, after)
			require.Equal(t, before, data)
			chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, k.ID)
			require.Zero(t, chunks)
			require.Zero(t, bodyVectors)
			require.EqualValues(t, 1, articleVectors)
			require.Empty(t, correctionLogs(t, h, k.ID))
		})
	}
}

func TestContentTypeConversionRestoresBodyAndAttachmentsWhenLogTransactionFails(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	k := h.upload(t, "restore-body", "An original announcement with measurements.\n\n![figure]("+articleImageDataURI(t, 60)+")")
	before, _, _ := readVaultArticle(t, v, k)
	attachmentDir := filepath.Join(v.root, "attachments", k.ID)
	attachments, err := os.ReadDir(attachmentDir)
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_fixture_conversion_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture operation log unavailable'; END; $$; CREATE TRIGGER reject_fixture_conversion_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_fixture_conversion_log()`).Error)
	_, err = h.service.CorrectContentType(context.WithValue(h.ctx, types.UserIDContextKey, "fixture-user"), k.ID, "tutorial", true)
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
