package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"

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

func TestIngestMineReviewsProcessesTutorialFullTextAndPortableHeader(t *testing.T) {
	h, vault := newIngestHarness(t)
	capture := captureMineReview(h)
	topic, err := NewTopicService(h.db).CreateTopic(h.ctx, &types.Topic{Name: "Personal measurements", Description: "Measurements and calibration notes"})
	require.NoError(t, err)
	h.processor.output = articleProcessingOutput(t, "笔记", []types.EntityCandidate{{Name: "Spectrometer", EntityType: "product"}}, []string{topic.ID})
	content := "# Calibration notes\n\nSpectrometer observations record verified values. Follow the measured calibration procedure and compare the original readings.\n\n来源: https://example.org/reference-not-my-document"
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "verbatim", Title: "校准笔记", Content: content})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Len(t, capture.reviews, 1)
	require.Contains(t, capture.reviews[0][0].Content, "Library review criteria:")
	require.Contains(t, capture.reviews[0][0].Content, types.PersonalReviewCriteria)
	h.drain(t)
	k := h.article(t, result.EntryID)
	require.Equal(t, "mine", k.SourceKind)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus, k.ErrorMessage)
	require.Equal(t, "笔记", k.ContentType)
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
	require.Contains(t, string(data), topic.Name)
	require.NotContains(t, string(data), "url: null")
	require.NotContains(t, string(data), "publisher: null")
	require.NotContains(t, string(data), "published_at: null")
	var count int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", k.ID).Count(&count).Error)
	require.Positive(t, count)
	require.NoError(t, h.db.Table("article_embeddings").Where("entry_id=?", k.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.Len(t, h.processor.requests, 1)
	require.Equal(t, []string{"intake_review", "article_processing"}, h.processor.stages[:2])
	corrected, err := h.service.CorrectContentType(h.ctx, k.ID, "经验")
	require.NoError(t, err)
	require.Equal(t, k.VaultBody, corrected.VaultBody)
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, k.VaultPath), []byte("damaged user document"), 0644))
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	restored, err := vault.ReadBody(corrected)
	require.NoError(t, err)
	require.Equal(t, []byte(k.VaultBody), restored)
}

func TestIngestMineRejectedIsPersonalPendingAndApprovalProcessesWithoutReview(t *testing.T) {
	h, vault := newIngestHarness(t)
	capture := captureMineReview(h)
	h.processor.reject = true
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "model_written", Title: "待审本人文档", Content: "My original measurements record calibrations and complete supporting observations."})
	require.NoError(t, err)
	require.Equal(t, "pending_review", result.Status)
	k := h.article(t, result.EntryID)
	require.Equal(t, "mine", k.SourceKind)
	require.Equal(t, types.ParseStatusRejected, k.ParseStatus)
	require.Empty(t, k.VaultPath)
	pending, err := h.service.ListPending(h.ctx, 1, 20, "intake")
	require.NoError(t, err)
	entries := pending.Data.([]*types.Entry)
	require.Len(t, entries, 1)
	require.Equal(t, "mine", entries[0].SourceKind)
	require.Empty(t, h.queue.tasks)
	_, err = h.service.ReviewEntryIntake(h.ctx, k.ID, "approve", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	k = h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus, k.ErrorMessage)
	require.Len(t, capture.reviews, 1)
	require.Len(t, h.processor.requests, 1)
	require.True(t, strings.HasPrefix(k.VaultPath, "personal/"))
	require.NoError(t, vault.RefreshHeader(h.ctx, k.ID))
	data, err := os.ReadFile(filepath.Join(vault.root, k.VaultPath))
	require.NoError(t, err)
	header, _, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Empty(t, header.SourceKind)
	require.Equal(t, k.LibraryID, header.LibraryID)
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
	duplicate, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "personal", SourceRef: "fixture-mine", Acquisition: "verbatim", Title: "重复本人实验", Content: content})
	require.NoError(t, err)
	require.Equal(t, "exists", duplicate.Status)
	require.Equal(t, mine.EntryID, duplicate.EntryID)
	var count int64
	require.NoError(t, h.db.Model(&types.Entry{}).Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, h.db.Model(&types.EntryRepostSource{}).Count(&count).Error)
	require.Zero(t, count)
	require.Len(t, capture.reviews, 2)
	require.Len(t, h.processor.requests, 2)
}
