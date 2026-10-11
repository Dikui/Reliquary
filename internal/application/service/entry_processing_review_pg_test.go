package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/config"
	"reliquary/internal/types"
)

const uncertainProcessingReason = "The publication mixes original research findings and a step-by-step tutorial."

func uncertainProcessingFixture(t *testing.T, contentType string) string {
	t.Helper()
	var result map[string]any
	require.NoError(t, json.Unmarshal([]byte(validProcessingFixture(contentType)), &result))
	result["content_type_uncertain"], result["content_type_reason"] = true, uncertainProcessingReason
	data, err := json.Marshal(result)
	require.NoError(t, err)
	return string(data)
}

func pendingProcessingFixture(t *testing.T, contentType string) (*articleProcessingHarness, *VaultService, *types.Entry) {
	t.Helper()
	h, vault := newVaultArticleHarness(t)
	h.processor.output = uncertainProcessingFixture(t, contentType)
	article := h.upload(t, "uncertain-type", "Original reviewbodyneedle experiments include complete measurements and deployment instructions.\n\n![figure]("+articleImageDataURI(t, 78)+")")
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Equal(t, types.ProcessingReviewPending, article.ProcessingReviewStatus)
	return h, vault, article
}

func processingReviewLogs(t *testing.T, h *articleProcessingHarness, id string) []types.OperationLog {
	t.Helper()
	var logs []types.OperationLog
	require.NoError(t, h.db.Where("object_id=? AND operation_type LIKE 'processing_review_%'", id).Order("created_at,id").Find(&logs).Error)
	return logs
}

func TestArticleProcessingRequiresUncertaintyBooleanAndUncertainReason(t *testing.T) {
	for _, modification := range []func(map[string]any){
		func(result map[string]any) { delete(result, "content_type_uncertain") },
		func(result map[string]any) { result["content_type_uncertain"] = "true" },
		func(result map[string]any) {
			result["content_type_uncertain"], result["content_type_reason"] = true, "  "
		},
	} {
		var result map[string]any
		require.NoError(t, json.Unmarshal([]byte(validProcessingFixture("tutorial")), &result))
		modification(result)
		data, err := json.Marshal(result)
		require.NoError(t, err)
		_, err = parseArticleProcessingResult(string(data))
		require.Error(t, err)
	}
	parsed, err := parseArticleProcessingResult(uncertainProcessingFixture(t, "tutorial"))
	require.NoError(t, err)
	require.True(t, *parsed.ContentTypeUncertain)
	require.Equal(t, uncertainProcessingReason, parsed.ContentTypeReason)
}

func TestProcessingReviewUncertainTutorialSavesFullSourceAndRemainsSearchable(t *testing.T) {
	h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
	require.Equal(t, types.ContentTypeTutorial, article.ContentType)
	require.True(t, article.ContentTypeUncertain)
	require.Equal(t, types.ProcessingModeFull, article.ProcessingMode)
	require.Equal(t, types.ContentTypeTutorial, article.ProcessingReviewContentType)
	require.Equal(t, uncertainProcessingReason, article.ProcessingReviewReason)
	_, _, body := readVaultArticle(t, vault, article)
	require.Contains(t, string(body), "reviewbodyneedle")
	_, err := os.Stat(filepath.Join(vault.root, "attachments", article.ID))
	require.NoError(t, err)
	chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, article.ID)
	require.Positive(t, chunks)
	require.Positive(t, bodyVectors)
	require.EqualValues(t, 1, articleVectors)
	require.Empty(t, processingReviewLogs(t, h, article.ID), "automatic classification does not emit user operation logs")
	require.Contains(t, h.processor.requests[0][0].Content, "content_type_uncertain")
	require.Contains(t, h.processor.requests[0][0].Content, "全文保存类型")
	search := NewSearchService(entrySearchFixtureBases{kb: h.kb}, nil, nil, h.db)
	results, err := search.Search(h.ctx, types.EntrySearchRequest{Query: "reviewbodyneedle", CollapseSimilar: entrySearchNoCollapse()})
	require.NoError(t, err)
	require.Contains(t, entrySearchIDs(results), article.ID)
	require.NotEmpty(t, results.Results[0].Snippets)
}

func TestProcessingReviewConfirmFullModelTypeKeepsSourceAndSurvivesReprocessing(t *testing.T) {
	h, vault, article := pendingProcessingFixture(t, types.ContentTypeResearch)
	_, _, beforeBody := readVaultArticle(t, vault, article)
	calls := len(h.processor.requests)
	updated, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "confirm", "", false)
	require.NoError(t, err)
	require.Equal(t, types.ProcessingReviewConfirmed, updated.ProcessingReviewStatus)
	require.False(t, updated.ContentTypeCorrected)
	require.Equal(t, types.ContentTypeResearch, updated.ContentType)
	require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
	require.Len(t, h.processor.requests, calls)
	_, _, afterBody := readVaultArticle(t, vault, updated)
	require.Equal(t, beforeBody, afterBody)
	logs := processingReviewLogs(t, h, article.ID)
	require.Len(t, logs, 1)
	require.Equal(t, "processing_review_confirm", logs[0].OperationType)
	require.Equal(t, types.ProcessingReviewPending, correctionLogValue(t, logs[0].Before, "processing_review_status"))
	require.Equal(t, types.ProcessingReviewConfirmed, correctionLogValue(t, logs[0].After, "processing_review_status"))
	h.processor.output = uncertainProcessingFixture(t, types.ContentTypeTutorial)
	_, err = h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	after := h.article(t, article.ID)
	require.Equal(t, types.ContentTypeResearch, after.ContentType)
	require.Equal(t, types.ProcessingReviewConfirmed, after.ProcessingReviewStatus)
	require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
	require.Len(t, processingReviewLogs(t, h, article.ID), 1)
}

func TestProcessingReviewConfirmSummaryModelTypeRequiresConfirmationAndRemovesSource(t *testing.T) {
	h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
	_, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "confirm", "", false)
	require.ErrorContains(t, err, "confirm_remove_body")
	require.Equal(t, types.ProcessingReviewPending, h.article(t, article.ID).ProcessingReviewStatus)
	require.Empty(t, processingReviewLogs(t, h, article.ID))
	updated, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "confirm", "", true)
	require.NoError(t, err)
	require.False(t, updated.ContentTypeCorrected)
	require.Equal(t, types.ProcessingReviewConfirmed, updated.ProcessingReviewStatus)
	require.Equal(t, types.ProcessingModeSummaryOnly, updated.ProcessingMode)
	data, header, body := readVaultArticle(t, vault, updated)
	require.Equal(t, header, data)
	require.Empty(t, body)
	_, err = os.Stat(filepath.Join(vault.root, "attachments", article.ID))
	require.True(t, os.IsNotExist(err))
	chunks, bodyVectors, articleVectors := correctionIndexCounts(t, h, article.ID)
	require.Zero(t, chunks)
	require.Zero(t, bodyVectors)
	require.EqualValues(t, 1, articleVectors)
	require.Len(t, processingReviewLogs(t, h, article.ID), 1)
}

func TestProcessingReviewCorrectTypeUsesUserCorrectionAndExistingBodyConversion(t *testing.T) {
	for _, contentType := range []string{types.ContentTypeResearch, types.ContentTypeReference} {
		t.Run(contentType, func(t *testing.T) {
			h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
			_, _, originalBody := readVaultArticle(t, vault, article)
			if contentType == types.ContentTypeReference {
				_, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "correct", contentType, false)
				require.ErrorContains(t, err, "confirm_remove_body")
				require.Empty(t, processingReviewLogs(t, h, article.ID))
			}
			updated, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "correct", contentType, true)
			require.NoError(t, err)
			require.True(t, updated.ContentTypeCorrected)
			require.Equal(t, contentType, updated.ContentType)
			require.Equal(t, types.ProcessingReviewCorrected, updated.ProcessingReviewStatus)
			require.Equal(t, types.ContentTypeTutorial, updated.ProcessingReviewContentType)
			_, _, body := readVaultArticle(t, vault, updated)
			if contentType == types.ContentTypeReference {
				require.Empty(t, body)
				chunks, vectors, _ := correctionIndexCounts(t, h, article.ID)
				require.Zero(t, chunks)
				require.Zero(t, vectors)
			} else {
				require.Equal(t, originalBody, body)
			}
			logs := processingReviewLogs(t, h, article.ID)
			require.Len(t, logs, 1)
			require.Equal(t, "processing_review_correct", logs[0].OperationType)
			require.Equal(t, true, correctionLogValue(t, logs[0].After, "content_type_corrected"))
		})
	}
}

func TestProcessingReviewIgnoreKeepsCurrentSourceAndDoesNotReopenOnReprocessing(t *testing.T) {
	h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
	data, _, originalBody := readVaultArticle(t, vault, article)
	updated, err := h.service.ResolveProcessingReview(h.ctx, article.ID, "ignore", "", false)
	require.NoError(t, err)
	require.Equal(t, types.ProcessingReviewIgnored, updated.ProcessingReviewStatus)
	require.Equal(t, types.ContentTypeTutorial, updated.ContentType)
	require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
	require.False(t, updated.ContentTypeCorrected)
	afterData, _, afterBody := readVaultArticle(t, vault, updated)
	require.Equal(t, data, afterData)
	require.Equal(t, originalBody, afterBody)
	_, err = h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	after := h.article(t, article.ID)
	require.Equal(t, types.ProcessingReviewIgnored, after.ProcessingReviewStatus)
	require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
	logs := processingReviewLogs(t, h, article.ID)
	require.Len(t, logs, 1)
	require.Equal(t, "processing_review_ignore", logs[0].OperationType)
	h.processor.output = validProcessingFixture(types.ContentTypeReference)
	_, err = h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	after = h.article(t, article.ID)
	require.False(t, after.ContentTypeUncertain)
	require.Equal(t, types.ContentTypeTutorial, after.ContentType)
	require.Equal(t, types.ProcessingReviewIgnored, after.ProcessingReviewStatus)
	require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
	_, _, afterBody = readVaultArticle(t, vault, after)
	require.Equal(t, originalBody, afterBody)
	chunks, vectors, _ := correctionIndexCounts(t, h, article.ID)
	require.Positive(t, chunks)
	require.Positive(t, vectors)
	require.Len(t, processingReviewLogs(t, h, article.ID), 1)
	_, err = h.service.ResolveProcessingReview(h.ctx, article.ID, "ignore", "", false)
	require.ErrorContains(t, err, "not pending")
}

func TestProcessingReviewOperationLogFailureRollsBackResolutionAndBodyRemoval(t *testing.T) {
	for _, action := range []string{"ignore", "correct"} {
		t.Run(action, func(t *testing.T) {
			h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
			before, _, _ := readVaultArticle(t, vault, article)
			require.NoError(t, h.db.Exec(`CREATE FUNCTION reject_fixture_processing_review_log() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'fixture processing log unavailable'; END; $$; CREATE TRIGGER reject_fixture_processing_review_log BEFORE INSERT ON operation_logs FOR EACH ROW EXECUTE FUNCTION reject_fixture_processing_review_log()`).Error)
			_, err := h.service.ResolveProcessingReview(h.ctx, article.ID, action, types.ContentTypeReference, true)
			require.ErrorContains(t, err, "fixture processing log unavailable")
			after := h.article(t, article.ID)
			require.Equal(t, types.ProcessingReviewPending, after.ProcessingReviewStatus)
			require.False(t, after.ContentTypeCorrected)
			require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
			data, _, _ := readVaultArticle(t, vault, article)
			require.Equal(t, before, data)
			require.Empty(t, processingReviewLogs(t, h, article.ID))
		})
	}
}

func TestSourceUnverifiedArticleContentTypeCorrectionAlwaysKeepsFullSource(t *testing.T) {
	h, vault := newVaultArticleHarness(t)
	article := h.upload(t, "unverified-preserves-body", "Original reviewbodyneedle body and measurements.\n\n![figure]("+articleImageDataURI(t, 79)+")")
	require.NoError(t, h.service.repo.UpdateEntryColumn(h.ctx, article.ID, "source_unverified", true))
	_, _, beforeBody := readVaultArticle(t, vault, article)
	updated, err := h.service.CorrectContentType(h.ctx, article.ID, types.ContentTypeReference, false)
	require.NoError(t, err)
	require.True(t, updated.SourceUnverified)
	require.True(t, updated.ContentTypeCorrected)
	require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
	_, _, afterBody := readVaultArticle(t, vault, updated)
	require.Equal(t, beforeBody, afterBody)
	chunks, vectors, _ := correctionIndexCounts(t, h, article.ID)
	require.Positive(t, chunks)
	require.Positive(t, vectors)
	h.processor.output = validProcessingFixture(types.ContentTypeTutorial)
	_, err = h.service.ReparseEntry(h.ctx, article.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	after := h.article(t, article.ID)
	require.True(t, after.SourceUnverified)
	require.Equal(t, types.ProcessingModeFull, after.ProcessingMode)
	require.Equal(t, types.ContentTypeReference, after.ContentType)
	_, _, afterBody = readVaultArticle(t, vault, after)
	require.Equal(t, beforeBody, afterBody)
}

func TestSourceUnverifiedProcessingReviewResolutionKeepsFullSource(t *testing.T) {
	for _, action := range []string{"confirm", "correct"} {
		t.Run(action, func(t *testing.T) {
			h, vault, article := pendingProcessingFixture(t, types.ContentTypeTutorial)
			require.NoError(t, h.service.repo.UpdateEntryColumn(h.ctx, article.ID, "source_unverified", true))
			_, _, beforeBody := readVaultArticle(t, vault, article)
			updated, err := h.service.ResolveProcessingReview(h.ctx, article.ID, action, types.ContentTypeReference, false)
			require.NoError(t, err)
			require.True(t, updated.SourceUnverified)
			require.Equal(t, types.ProcessingModeFull, updated.ProcessingMode)
			require.Equal(t, action == "correct", updated.ContentTypeCorrected)
			_, _, afterBody := readVaultArticle(t, vault, updated)
			require.Equal(t, beforeBody, afterBody)
			chunks, vectors, _ := correctionIndexCounts(t, h, article.ID)
			require.Positive(t, chunks)
			require.Positive(t, vectors)
		})
	}
}

func TestProcessingReviewAllContentTypesSaveFullSourceWhenUncertain(t *testing.T) {
	for _, contentType := range types.ContentTypes {
		t.Run(contentType, func(t *testing.T) {
			h, vault, article := pendingProcessingFixture(t, contentType)
			require.Equal(t, types.ProcessingModeFull, article.ProcessingMode)
			require.Equal(t, contentType, article.ContentType)
			_, _, body := readVaultArticle(t, vault, article)
			require.Contains(t, strings.ToLower(string(body)), "reviewbodyneedle")
			require.Empty(t, processingReviewLogs(t, h, article.ID))
		})
	}
}

func TestVaultRebuildPreservesFullTutorialForUncertainAndUnverifiedSources(t *testing.T) {
	for _, source := range []string{"uncertain", "unverified"} {
		t.Run(source, func(t *testing.T) {
			h, vault := newVaultArticleHarness(t)
			body := "Original reviewbodyneedle measurements retained for vault recovery.\n\n![figure](" + articleImageDataURI(t, 81) + ")"
			var article *types.Entry
			if source == "uncertain" {
				h.processor.output = uncertainProcessingFixture(t, types.ContentTypeTutorial)
				article = h.upload(t, "rebuild-uncertain", body)
			} else {
				h.processor.output = validProcessingFixture(types.ContentTypeTutorial)
				created, err := h.service.CreateEntryFromFile(h.ctx, h.kb.ID,
					newMultipartFileHeader(t, "rebuild-unverified.md", body), map[string]string{"publisher": "Acme"}, nil, "", types.ChannelWeb, nil)
				require.NoError(t, err)
				h.drain(t)
				require.Equal(t, types.ParseStatusRejected, h.article(t, created.ID).ParseStatus)
				_, err = h.service.ReviewEntryIntake(h.ctx, created.ID, "admit_unverified", types.SourceInfo{Confirmed: true})
				require.NoError(t, err)
				h.drain(t)
				article = h.article(t, created.ID)
			}
			require.Equal(t, types.ContentTypeTutorial, article.ContentType)
			require.Equal(t, types.ProcessingModeFull, article.ProcessingMode)
			before, _, originalBody := readVaultArticle(t, vault, article)
			header, _, err := ParseVaultArticle(before)
			require.NoError(t, err)
			require.Equal(t, types.ProcessingModeFull, header.ProcessingMode)
			require.Equal(t, source == "unverified", header.SourceUnverified)
			if source == "unverified" {
				require.Empty(t, header.URL)
			}
			hashes := vaultFileHashes(t, vault.root)
			fresh := newRetainedIntakeHarness(t)
			fresh.kb.ModelTiers, fresh.kb.EmbeddingModelID, fresh.kb.IndexingStrategy = h.kb.ModelTiers, h.kb.EmbeddingModelID, h.kb.IndexingStrategy
			require.NoError(t, fresh.db.Save(fresh.kb).Error)
			retrieval := retriever.NewPostgresRetrievalService(pgretriever.NewPostgresRetrieveEngineRepository(fresh.db))
			rebuild, err := NewVaultDocumentService(fresh.db, h.service.modelService, retrieval, vault.root, time.Millisecond, time.Minute)
			require.NoError(t, err)
			result, err := rebuild.Rebuild(fresh.ctx)
			require.NoError(t, err)
			require.Equal(t, 1, result.Articles)
			require.Empty(t, result.Skipped)
			restored := fresh.article(t, article.ID)
			require.Equal(t, types.ProcessingModeFull, restored.ProcessingMode)
			require.Equal(t, article.SourceUnverified, restored.SourceUnverified)
			require.Equal(t, string(originalBody), restored.VaultBody)
			var chunks, vectors int64
			require.NoError(t, fresh.db.Model(&types.Chunk{}).Where("entry_id=?", article.ID).Count(&chunks).Error)
			require.NoError(t, fresh.db.Table("embeddings").Where("entry_id=?", article.ID).Count(&vectors).Error)
			require.Positive(t, chunks)
			require.Positive(t, vectors)
			freshVault, err := NewVaultService(fresh.db, &config.Config{Vault: &config.VaultConfig{Root: vault.root, HeaderDebounce: time.Millisecond}})
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, freshVault.Close()) })
			require.NoError(t, freshVault.RefreshHeader(fresh.ctx, article.ID))
			require.Equal(t, hashes, vaultFileHashes(t, vault.root), "recovery and header refresh must preserve body and attachment bytes")
			search := NewSearchService(entrySearchFixtureBases{kb: fresh.kb}, nil, nil, fresh.db)
			results, err := search.Search(fresh.ctx, types.EntrySearchRequest{Query: "reviewbodyneedle", CollapseSimilar: entrySearchNoCollapse()})
			require.NoError(t, err)
			require.Contains(t, entrySearchIDs(results), article.ID)
		})
	}
}
