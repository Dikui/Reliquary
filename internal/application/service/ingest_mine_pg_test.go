package service

import (
	"context"
	"encoding/base64"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
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
	h.processor.output = articleProcessingOutput(t, "tutorial", []types.EntityCandidate{{Name: "Spectrometer", EntityType: "product"}}, []string{topic.ID})
	content := "# Calibration notes\n\nSpectrometer observations record verified values. Follow the measured calibration procedure and compare the original readings.\n\n来源: https://example.org/reference-not-my-document"
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", Title: "校准笔记", Content: content})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Len(t, capture.reviews, 1)
	require.Contains(t, capture.reviews[0][0].Content, "No original URL is normal")
	require.Contains(t, capture.reviews[0][0].Content, "without relaxing or tightening")
	h.drain(t)
	k := h.article(t, result.EntryID)
	require.Equal(t, "mine", k.SourceKind)
	require.Equal(t, types.ParseStatusCompleted, k.ParseStatus, k.ErrorMessage)
	require.Equal(t, "tutorial", k.ContentType)
	require.Equal(t, types.ProcessingModeFull, k.ProcessingMode)
	require.False(t, k.Incomplete)
	require.False(t, k.SourceUnverified)
	require.Empty(t, k.NormalizedURL)
	intake, err := k.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, intake.SourceInfo.OriginalURL)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, "mine/校准笔记-"+k.ID[:8]+".md", k.VaultPath)
	data, err := os.ReadFile(filepath.Join(vault.root, k.VaultPath))
	require.NoError(t, err)
	header, body, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Equal(t, "mine", header.SourceKind)
	require.Equal(t, k.VaultBody, string(body))
	require.Contains(t, string(body), content)
	require.Contains(t, string(data), "[[Spectrometer]]")
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
	corrected, err := h.service.CorrectContentType(h.ctx, k.ID, "reference", false)
	require.NoError(t, err)
	require.Equal(t, types.ProcessingModeFull, corrected.ProcessingMode)
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
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "model_written", Title: "待审本人文档", Content: "My original measurements record calibrations and complete supporting observations."})
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
	require.True(t, strings.HasPrefix(k.VaultPath, "mine/"))
	data, err := os.ReadFile(filepath.Join(vault.root, k.VaultPath))
	require.NoError(t, err)
	header, _, err := ParseVaultArticle(data)
	require.NoError(t, err)
	require.Equal(t, "mine", header.SourceKind)
}

func TestIngestMineDuplicateIsSameKindAndNeverCreatesRepost(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	content := "The documented experiment records original verified spectrometer output, configuration values and independent measurements."
	article, err := h.service.Ingest(h.ctx, &types.IngestRequest{Acquisition: "verbatim", URL: "https://example.org/same-content", Publisher: "Journal", Content: content})
	require.NoError(t, err)
	h.drain(t)
	mine, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", URL: "https://example.org/same-content", Title: "本人实验", Content: content})
	require.NoError(t, err)
	require.Equal(t, "accepted", mine.Status)
	require.NotEqual(t, article.EntryID, mine.EntryID)
	h.drain(t)
	duplicate, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", Title: "重复本人实验", Content: content})
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

func TestIngestMineAttachmentsMissingAndIDSupplementFollowFinalization(t *testing.T) {
	h, vault := newIngestHarness(t)
	h.processor.output = validProcessingFixture("tutorial")
	imageURL := "https://example.org/missing-personal.png"
	original := "# Personal apparatus notes\n\nThe complete original observations record apparatus dimensions, calibration settings and verified measurements.\n\n![measurement](" + imageURL + ")"
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", Title: "本人材料补全", Content: original, Missing: []types.IngestMissing{{Type: "image", SourceURL: imageURL, Reason: "图片暂时无法导出"}, {Type: "file", Reason: "附件暂时无法导出"}}})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Len(t, result.Completeness, 2)
	h.drain(t)
	k := h.article(t, result.EntryID)
	require.True(t, k.Incomplete)
	incomplete, err := h.service.ListIncomplete(h.ctx, 1, 20, false)
	require.NoError(t, err)
	require.Len(t, incomplete.Items, 1)
	require.Equal(t, "mine", incomplete.Items[0].Kind)
	require.Equal(t, k.ID, incomplete.Items[0].EntryID)
	require.Empty(t, incomplete.Items[0].URL)
	image := strings.SplitN(articleImageDataURI(t, 52), ",", 2)[1]
	supplemented, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", DocumentID: k.ID, Acquisition: "verbatim", Images: []types.IngestImage{{SourceURL: imageURL, Data: image}}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplemented.Status)
	require.Len(t, supplemented.Completeness, 1)
	k = h.article(t, k.ID)
	require.True(t, k.Incomplete)
	require.True(t, k.ExternalCompletionAttempted)
	incomplete, err = h.service.ListIncomplete(h.ctx, 1, 20, false)
	require.NoError(t, err)
	require.Empty(t, incomplete.Items)
	text := "Original instrument calibration worksheet."
	supplemented, err = h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", DocumentID: k.ID, Acquisition: "verbatim", File: &types.IngestFile{Name: "worksheet.txt", Data: base64.StdEncoding.EncodeToString([]byte(text))}})
	require.NoError(t, err)
	require.Empty(t, supplemented.Completeness)
	h.drain(t)
	final := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, final.ParseStatus, final.ErrorMessage)
	require.False(t, final.Incomplete)
	require.Equal(t, types.ProcessingModeFull, final.ProcessingMode)
	require.Contains(t, final.VaultBody, "The complete original observations")
	require.NotContains(t, final.VaultBody, imageURL)
	require.Contains(t, final.VaultBody, "../attachments/"+k.ID+"/")
	require.Len(t, final.IngestAttachmentRecords(), 1)
	file, err := vault.OpenAttachment(h.ctx, k.ID, final.IngestAttachmentRecords()[0].Path)
	require.NoError(t, err)
	bytes, err := io.ReadAll(file)
	require.NoError(t, err)
	require.NoError(t, file.Close())
	require.Equal(t, text, string(bytes))
	require.Len(t, h.processor.requests, 2)
	_, err = h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", DocumentID: k.ID, Acquisition: "verbatim", Content: "edited"})
	require.ErrorContains(t, err, "incomplete user document")
	require.Equal(t, final.VaultBody, h.article(t, k.ID).VaultBody)
}

func TestIngestMineMissingBodyRequiresReviewAfterIDSupplement(t *testing.T) {
	h, _ := newIngestHarness(t)
	capture := captureMineReview(h)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", Missing: []types.IngestMissing{{Type: "content", Reason: "正文尚未导出"}}})
	require.NoError(t, err)
	require.Equal(t, "pending_review", result.Status)
	require.Empty(t, capture.reviews)
	supplemented, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", DocumentID: result.EntryID, Acquisition: "verbatim", Content: "Full original personal observations supply measured values and describe their instruments."})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplemented.Status)
	require.Len(t, capture.reviews, 1)
	h.drain(t)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, result.EntryID).ParseStatus)
}

func TestIngestMineAcceptMissingAndConflictingSupplement(t *testing.T) {
	for _, action := range []string{"accept", "conflict"} {
		t.Run(action, func(t *testing.T) {
			h, vault := newIngestHarness(t)
			capture := captureMineReview(h)
			original := "# Telescope observations\n\nThe complete observing log records calibrated telescope settings, stellar positions, exposure durations and recorded brightness measurements."
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", Acquisition: "verbatim", Content: original, Title: "本人观测记录", Missing: []types.IngestMissing{{Type: "file", Reason: "原始观测表暂时无法导出"}}})
			require.NoError(t, err)
			h.drain(t)
			k := h.article(t, result.EntryID)
			before := k.VaultBody
			if action == "accept" {
				accepted, err := h.service.AcceptIncomplete(h.ctx, k.ID)
				require.NoError(t, err)
				require.Empty(t, accepted.Completeness)
				final := h.article(t, k.ID)
				require.False(t, final.Incomplete)
				require.Equal(t, before, final.VaultBody)
				_, err = h.service.Ingest(h.ctx, &types.IngestRequest{Kind: "mine", DocumentID: k.ID, Acquisition: "verbatim", File: &types.IngestFile{Name: "changed.txt", Data: base64.StdEncoding.EncodeToString([]byte("Changed attachment"))}})
				require.ErrorContains(t, err, "incomplete user document")
				logs, err := NewOperationLogService(h.db).List(h.ctx, "accept_incomplete", 1, 20)
				require.NoError(t, err)
				require.EqualValues(t, 1, logs.Total)
				return
			}
			request := &types.IngestRequest{Kind: "mine", DocumentID: k.ID, Acquisition: "verbatim", Title: "不同本人记录", Content: "# Baking experiment\n\nThe bread experiment describes flour hydration, yeast fermentation temperatures, kneading duration and the complete resulting loaf weights."}
			conflict, err := h.service.Ingest(h.ctx, request)
			require.NoError(t, err)
			require.Equal(t, "conflict", conflict.Status)
			require.NotEqual(t, k.ID, conflict.EntryID)
			pending := h.article(t, conflict.EntryID)
			require.Equal(t, "mine", pending.SourceKind)
			require.Equal(t, k.ID, pending.RelatedArticleID)
			require.Equal(t, types.ParseStatusRejected, pending.ParseStatus)
			require.Equal(t, before, h.article(t, k.ID).VaultBody)
			repeated, err := h.service.Ingest(h.ctx, request)
			require.NoError(t, err)
			require.Equal(t, conflict.EntryID, repeated.EntryID)
			_, err = h.service.ReviewEntryIntake(h.ctx, pending.ID, "approve", types.SourceInfo{})
			require.NoError(t, err)
			h.drain(t)
			saved := h.article(t, pending.ID)
			require.Equal(t, "mine", saved.SourceKind)
			require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus, saved.ErrorMessage)
			require.Contains(t, saved.VaultBody, "Baking experiment")
			require.Len(t, capture.reviews, 1)
			body, err := vault.ReadBody(h.article(t, k.ID))
			require.NoError(t, err)
			require.Equal(t, []byte(before), body)
		})
	}
}
