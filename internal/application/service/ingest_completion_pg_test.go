package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"

	"github.com/stretchr/testify/require"
)

func ingestFixtureRequest(url, body string) *types.IngestRequest {
	return &types.IngestRequest{Library: "institutional", URL: url, Content: body, Acquisition: "verbatim", Publisher: "Acme", Title: "Original observations"}
}
func ingestMissingImageRequest(url, source string) *types.IngestRequest {
	return ingestFixtureRequest(url, "# Original observations\n\nAcme reports the exact measured pressure, the calibration method and instrument configuration.\n\n![measurement]("+source+")")
}

type ingestSelectiveFailureModel struct{ *articleProcessingChat }

func (m *ingestSelectiveFailureModel) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	for _, message := range messages {
		if strings.Contains(message.Content, "deliberately encounters") {
			return nil, fmt.Errorf("fixture model internal failure")
		}
	}
	return m.articleProcessingChat.Chat(ctx, messages, options)
}

func TestIngestImageSupplementWithBodyPreservesTextReprocessesAndUpdatesHeader(t *testing.T) {
	h, v := newIngestHarness(t)
	transport := forbidIngestNetwork(t, h)
	source := "https://example.org/figure.png"
	request := ingestMissingImageRequest("https://example.org/completion", source)
	accepted, err := h.service.Ingest(h.ctx, request)
	require.NoError(t, err)
	require.Equal(t, "accepted", accepted.Status)
	h.drain(t)
	before := h.article(t, accepted.EntryID)
	_, _, oldBody := readVaultArticle(t, v, before)
	require.True(t, before.Incomplete)
	require.Len(t, h.processor.requests, 1)
	h.processor.output = strings.Replace(validProcessingFixture("research"), "Acme released", "Acme measured", 1)
	supplement, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "institutional", EntryID: accepted.EntryID, URL: request.URL, Content: request.Content, Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 71)}}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplement.Status)
	require.Empty(t, supplement.Completeness)
	h.drain(t)
	after := h.article(t, before.ID)
	require.False(t, after.Incomplete)
	require.Len(t, h.processor.requests, 2)
	require.Equal(t, "research", after.ContentType)
	waitCorrectionHeader(t, v, after, "research")
	_, header, newBody := readVaultArticle(t, v, after)
	require.Contains(t, string(header), "content_type: \"research\"")
	refs := articleImagesInOrder(string(newBody), nil)
	require.Len(t, refs, 1)
	require.Contains(t, refs[0], "../../attachments/")
	require.Equal(t, replaceIngestImageTargets(string(oldBody), map[string]string{source: refs[0]}), string(newBody), "all text outside the image destination is byte-identical")
	require.Zero(t, transport.calls.Load())
	require.EqualValues(t, 1, after.IngestAPIKeyID)
	require.Equal(t, "Fixture collector", after.LastIngestAPIKeyName)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest_supplement", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
	require.Equal(t, "Fixture collector", logs.Items[0].APIKeyName)
}

func TestIngestPartialCompletionListPaginationAcceptAndKeyRevocation(t *testing.T) {
	h, _ := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	sources := []string{"https://example.org/a.png", "https://example.org/b.png"}
	req := ingestMissingImageRequest("https://example.org/partial", sources[0])
	req.Content += "\n\n![second](" + sources[1] + ")"
	first, err := h.service.Ingest(h.ctx, req)
	require.NoError(t, err)
	h.drain(t)
	second, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/other", "https://example.org/other.png"))
	require.NoError(t, err)
	// Distinct text avoids content deduplication when source URLs are compared.
	if second.Status == "exists" {
		t.Fatal("fixture requires an independent second article")
	}
	h.drain(t)
	page, err := h.service.ListIncomplete(h.ctx, 1, 1, false)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	require.Len(t, page.Items, 1)
	require.NotEmpty(t, page.Items[0].URL)
	require.Len(t, page.Items[0].Missing, 2)
	next, err := h.service.ListIncomplete(h.ctx, 2, 1, false)
	require.NoError(t, err)
	require.Len(t, next.Items, 1)
	supplement, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "institutional", EntryID: first.EntryID, URL: req.URL, Content: req.Content, Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: sources[0], Data: articleImageDataURI(t, 72)}}})
	require.NoError(t, err)
	require.Equal(t, "supplemented", supplement.Status)
	require.Len(t, supplement.Completeness, 1)
	article := h.article(t, first.EntryID)
	require.True(t, article.Incomplete)
	require.True(t, article.ExternalCompletionAttempted)
	page, err = h.service.ListIncomplete(h.ctx, 0, 0, false)
	require.NoError(t, err)
	require.EqualValues(t, 1, page.Total)
	page, err = h.service.ListIncomplete(h.ctx, 1, 100, true)
	require.NoError(t, err)
	require.EqualValues(t, 2, page.Total)
	accepted, err := h.service.AcceptIncomplete(h.ctx, first.EntryID)
	require.NoError(t, err)
	require.Empty(t, accepted.Completeness)
	require.False(t, h.article(t, first.EntryID).Incomplete)
	now := time.Now()
	require.NoError(t, h.db.Model(&types.APIKey{}).Where("id=1").Update("revoked_at", now).Error)
	_, err = h.service.CheckURLs(h.ctx, []string{req.URL}, "institutional")
	require.Error(t, err)
	_, err = h.service.Ingest(h.ctx, req)
	require.Error(t, err)
	_, err = h.service.AcceptIncomplete(h.ctx, second.EntryID)
	require.Error(t, err)
}

func TestIngestConflictPreservesOriginalAndCanBeDiscardedOrSaved(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	req := ingestMissingImageRequest("https://example.org/conflict", "https://example.org/conflict.png")
	first, err := h.service.Ingest(h.ctx, req)
	require.NoError(t, err)
	h.drain(t)
	original := h.article(t, first.EntryID)
	_, _, body := readVaultArticle(t, v, original)
	different := ingestFixtureRequest(req.URL, "An unrelated novel discusses a fictional seafaring kingdom, characters and narrative choices.")
	different.EntryID = first.EntryID
	different.Images = []types.IngestImage{{SourceURL: "https://example.org/conflict.png", Data: articleImageDataURI(t, 90)}}
	different.CollectReason = "保留新材料供用户判断"
	different.AgentSummary = "外部说明"
	different.AgentTags = []string{"小说"}
	different.SourceName = "Fixture new source"
	result, err := h.service.Ingest(h.ctx, different)
	require.NoError(t, err)
	require.Equal(t, "conflict", result.Status)
	require.NotEqual(t, first.EntryID, result.EntryID)
	conflict := h.article(t, result.EntryID)
	require.Equal(t, first.EntryID, conflict.RelatedArticleID)
	require.Equal(t, different.CollectReason, conflict.CollectReason)
	require.Equal(t, different.AgentSummary, conflict.AgentSummary)
	_, _, still := readVaultArticle(t, v, h.article(t, first.EntryID))
	require.Equal(t, body, still)
	require.NoError(t, h.service.DiscardIngest(h.ctx, result.EntryID, "用户不保留冲突材料"))
	checks, err := h.service.CheckURLs(h.ctx, []string{req.URL}, "institutional")
	require.NoError(t, err)
	require.Equal(t, "in_library", checks[0].Status)
	result, err = h.service.Ingest(h.ctx, different)
	require.NoError(t, err)
	require.Equal(t, "conflict", result.Status)
	_, err = h.service.ReviewEntryIntake(h.ctx, result.EntryID, "approve", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	saved := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.NotEmpty(t, saved.VaultPath)
	require.Equal(t, first.EntryID, saved.RelatedArticleID)
}

func TestIngestConcurrentSameSupplementMutatesOnce(t *testing.T) {
	h, _ := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	source := "https://example.org/concurrent.png"
	first, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/concurrent", source))
	require.NoError(t, err)
	h.drain(t)
	request := &types.IngestRequest{Library: "institutional", EntryID: first.EntryID, URL: "https://example.org/concurrent", Content: ingestMissingImageRequest("https://example.org/concurrent", source).Content, Acquisition: "verbatim", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 73)}}}
	var wg sync.WaitGroup
	results := make([]*types.IngestResult, 2)
	errs := make([]error, 2)
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = h.service.Ingest(h.ctx, request) }(i)
	}
	wg.Wait()
	success := 0
	for i, err := range errs {
		if err != nil {
			require.ErrorContains(t, err, "not awaiting image completion")
		} else {
			success++
			require.Equal(t, "supplemented", results[i].Status)
		}
	}
	require.Equal(t, 1, success)
	logs, err := NewOperationLogService(h.db).List(h.ctx, "ingest_supplement", 1, 100)
	require.NoError(t, err)
	require.EqualValues(t, 1, logs.Total)
	require.False(t, h.article(t, first.EntryID).Incomplete)
}

func TestIngestSupplementRollbackRestoresVaultBody(t *testing.T) {
	h, v := newIngestHarness(t)
	forbidIngestNetwork(t, h)
	source := "https://example.org/rollback.png"
	first, err := h.service.Ingest(h.ctx, ingestMissingImageRequest("https://example.org/rollback", source))
	require.NoError(t, err)
	h.drain(t)
	article := h.article(t, first.EntryID)
	before, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
	require.NoError(t, err)
	require.NoError(t, h.db.Exec("DROP TABLE operation_logs").Error)
	_, err = h.service.Ingest(h.ctx, &types.IngestRequest{Library: "institutional", EntryID: first.EntryID, URL: "https://example.org/rollback", Content: ingestMissingImageRequest("https://example.org/rollback", source).Content, Acquisition: "verbatim", Images: []types.IngestImage{{SourceURL: source, Data: articleImageDataURI(t, 74)}}})
	require.Error(t, err)
	after, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
	require.NoError(t, err)
	require.Equal(t, before, after)
	require.True(t, h.article(t, article.ID).Incomplete)
}
