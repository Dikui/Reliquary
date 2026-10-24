package service

import (
	"context"
	"os"
	"path/filepath"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"testing"

	"github.com/stretchr/testify/require"
)

type mineReviewCapture struct {
	retainedProcessingChatInterface
	reviews [][]chat.Message
}

func (m *mineReviewCapture) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "intake_review" {
		m.reviews = append(m.reviews, messages)
	}
	return m.retainedProcessingChatInterface.Chat(ctx, messages, options)
}

func captureMineReview(h *articleProcessingHarness) *mineReviewCapture {
	capture := &mineReviewCapture{retainedProcessingChatInterface: h.processor}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService = &summaryStateModelService{model: capture, metadata: models.metadata}
	h.service.modelService = models
	return capture
}

func TestIngestMineProcessesWithoutReviewAndExportsPortableHeader(t *testing.T) {
	h, vault := newIngestHarness(t)
	capture := captureMineReview(h)
	h.processor.output = articleProcessingOutput(
		t,

		[]legacyEntityCandidate{{Name: "Spectrometer", EntityType: "organization"}},
	)
	content := "# Calibration notes\n\nSpectrometer observations record verified values. Follow the measured calibration procedure and compare the original readings.\n\n来源: https://example.org/reference-not-my-document"
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "verbatim", Title: "校准笔记", Content: content})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Empty(t, capture.reviews)
	h.drain(t)
	k := h.article(t, result.EntryID)
	require.Equal(t, "mine", k.SourceKind)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus, k.ErrorMessage)

	require.False(t, k.SourceUnverified)
	require.Empty(t, k.NormalizedURL)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, intake.SourceInfo.OriginalURL)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, "personal/校准笔记-"+k.ID[:8]+".md", k.VaultPath)
	require.NoFileExists(t, filepath.Join(vault.root, k.VaultPath))
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	data, err := os.ReadFile(filepath.Join(vault.root, k.VaultPath))
	require.NoError(t, err)
	header, body, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Empty(t, header.SourceKind)
	require.Equal(t, k.LibraryID, header.LibraryID)
	require.Equal(t, k.VaultBody, string(body))
	require.Contains(t, string(body), content)
	require.NotContains(t, string(data), "[[Spectrometer]]")
	require.Empty(t, k.Entities)
	require.NotContains(t, string(data), "url: null")
	require.NotContains(t, string(data), "publisher: null")
	require.NotContains(t, string(data), "published_at: null")
	var count int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", k.ID).Count(&count).Error)
	require.Positive(t, count)
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", k.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Len(t, h.processor.requests, 1)
	require.Equal(t, "article_processing", h.processor.stages[0])
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, k.VaultPath), []byte("damaged user document"), 0o644))
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	restored, err := vault.ReadBody(k)
	require.NoError(t, err)
	require.Equal(t, []byte(k.VaultBody), restored)
}

func TestIngestMineDoesNotEnterRoutinePendingReview(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	h.processor.reject = true
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Content: "My unfinished calibration notes."})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.NotEmpty(t, h.article(t, result.EntryID).VaultBody)
	pending, err := h.service.ListPending(h.ctx, 1, 20, "intake")
	require.NoError(t, err)
	require.Empty(t, pending.Data.([]*types.Entry))
	h.drain(t)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, result.EntryID).ParseStatus)
	require.Empty(t, capture.reviews)
}

func TestIngestMineDuplicateIsSameKindAndNeverCreatesRepost(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	content := "The documented experiment records original verified spectrometer output, configuration values and independent measurements."
	article, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", Acquisition: "verbatim", URL: "https://example.org/same-content", Publisher: "Journal", Content: content})
	require.NoError(t, err)
	h.drain(t)
	mine, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "verbatim", URL: "https://example.org/same-content", Title: "本人实验", Content: content})
	require.NoError(t, err)
	require.Equal(t, "accepted", mine.Status)
	require.NotEqual(t, article.EntryID, mine.EntryID)
	h.drain(t)
	duplicate, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "verbatim", Title: "本人实验", Content: content})
	require.NoError(t, err)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, mine.EntryID, duplicate.EntryID)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, h.db.Model(&types.EntryRepostSource{}).Count(&count).Error)
	require.Zero(t, count)
	require.Empty(t, capture.reviews)
	require.Len(t, h.processor.requests, 2)
}
