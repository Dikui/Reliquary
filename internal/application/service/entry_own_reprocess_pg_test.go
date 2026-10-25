package service

import (
	"context"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type cancelOwnSegmentChat struct {
	segmentedChat
	onResult func()
}

func (m *cancelOwnSegmentChat) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	response, err := m.segmentedChat.Chat(ctx, messages, options)
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if err == nil && stage == "entry_understanding" && m.onResult != nil {
		m.onResult()
		m.onResult = nil
	}
	return response, err
}

func TestOwnCancellationStopsBeforeTheNextSegmentAndResumes(t *testing.T) {
	h, _ := segmentedHarness(t)
	k := segmentedEntry(t, h)
	k.SourceKind, k.VaultBody = "mine", strings.Repeat("Original measurements. ", 24)
	require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved"}))
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	m := &cancelOwnSegmentChat{onResult: func() {
		_, err := h.service.CancelEntryParse(h.ctx, k.ID)
		require.NoError(t, err)
	}}
	models := h.service.modelService.(articleProcessingModels)
	models.summaryStateModelService.model = m
	_, err := h.service.callArticleProcessing(h.ctx, h.library, k, k.VaultBody, nil)
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, m.calls["entry_understanding"], 1)
	require.Equal(t, types.ParseStatusCancelled, h.article(t, k.ID).ParseStatus)
	_, err = h.service.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	require.Equal(t, 1, m.calls["entry_understanding"][1])
	require.Equal(t, types.ParseStatusCompleted, h.article(t, k.ID).ParseStatus)
}

func TestOwnReprocessAfterFailureAndCancellationReusesCompletedSegments(t *testing.T) {
	h, m := segmentedHarness(t)
	h.library.SystemRole = ""
	h.library.ID, h.library.Directory, h.library.Template, h.library.SourceEvidenceRule = "own-retry", "own-retry", types.TemplateOwn, ""
	require.NoError(t, h.db.Create(h.library).Error)
	k := segmentedEntry(t, h)
	k.SourceKind, k.VaultBody = "mine", "# Original\n\n"+strings.Repeat("Complete original measurements. ", 20)
	require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved"}))
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	m.fail = 3
	_, err := h.service.callArticleProcessing(h.ctx, h.library, k, k.VaultBody, nil)
	require.ErrorContains(t, err, "第 3/")
	k.ParseStatus = types.ParseStatusFailed
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	_, err = h.service.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	_, err = h.service.CancelEntryParse(h.ctx, k.ID)
	require.NoError(t, err)
	_, err = h.service.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	m.fail = 0
	h.drain(t)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, k.ID).ParseStatus)
	require.Equal(t, 1, m.calls["entry_understanding"][1])
	require.Equal(t, 1, m.calls["entry_understanding"][2])
	require.Equal(t, 2, m.calls["entry_understanding"][3])
}

func TestGeneratedReparseRunsProcessing(t *testing.T) {
	h, m := segmentedHarness(t)
	h.library.SystemRole = ""
	h.library.ID, h.library.Directory, h.library.Template, h.library.SourceEvidenceRule = "generated-reindex", "generated-reindex", types.TemplateGenerated, ""
	require.NoError(t, h.db.Create(h.library).Error)
	k := segmentedEntry(t, h)
	k.SourceKind, k.ParseStatus, k.EnableStatus = "report", types.ParseStatusCompleted, "enabled"
	k.VaultBody, k.Description = "# Caller report\n\nOriginal report material.", "Caller summary"
	k.StructuredSummary = &types.EntrySummary{Overview: "Caller overview"}
	k.SummaryStatus = types.SummaryStatusCompleted
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	_, err := h.service.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.drain(t)
	saved := h.article(t, k.ID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.NotEmpty(t, saved.Description)
	require.NotEqual(t, "Caller summary", saved.Description)
	require.NotNil(t, saved.StructuredSummary)

	require.NotEmpty(t, m.calls)
	require.Zero(t, m.mergeCalls)
	var chunks, runs int64
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("entry_id=?", k.ID).Count(&chunks).Error)
	require.Positive(t, chunks)
	require.NoError(t, h.db.Model(&entrySegmentRun{}).Where("entry_id=?", k.ID).Count(&runs).Error)
	require.Positive(t, runs)
}

func TestOwnMetadataIsUpdatedThroughTheNormalEntryService(t *testing.T) {
	h, _ := segmentedHarness(t)
	k := segmentedEntry(t, h)
	k.SourceKind = "mine"
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, k))
	require.NoError(t, h.service.UpdateEntry(h.ctx, &types.Entry{ID: k.ID, Title: "Updated", CustomMetadata: types.JSON(`{"owner":"me"}`)}))
	saved := h.article(t, k.ID)
	require.Equal(t, "Updated", saved.Title)
	require.JSONEq(t, `{"owner":"me"}`, string(saved.CustomMetadata))
}
