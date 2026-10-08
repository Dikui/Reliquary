package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type sourceIssuePendingRepo struct {
	*wikiKBGuardPendingRepo
}

func (r *sourceIssuePendingRepo) PendingCount(context.Context, string, string, string) (int64, error) {
	return int64(len(r.guardedOps)), nil
}

func TestWikiSourceIssuesCountAndRegenerateIgnoreLifecycle(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	chunk := sourceChangeChunk(t, db, "Immutable source content")
	page := sourceChangePage(t, db, "concept/issues", chunk.ID)
	sourceChangePage(t, db, "concept/unrelated", "unrelated")
	repo := repository.NewWikiPageRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	queue := &wikiGuardTaskQueue{}
	pending := &sourceIssuePendingRepo{wikiKBGuardPendingRepo: &wikiKBGuardPendingRepo{accepted: true}}
	wikiSvc := NewWikiPageService(repo, chunkRepo, nil, pending, nil)
	ordinary := &types.WikiPageIssue{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID, Slug: page.Slug,
		IssueType: "other", Description: "Existing pending issue", Status: "pending", ReportedBy: "user",
	}
	require.NoError(t, repo.CreateIssue(ctx, ordinary))
	resolved := &types.WikiPageIssue{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID, Slug: page.Slug,
		IssueType: "other", Description: "Existing resolved issue", Status: "resolved", ReportedBy: "user",
	}
	require.NoError(t, repo.CreateIssue(ctx, resolved))
	assertPending := func(want int64) {
		t.Helper()
		stats, err := wikiSvc.GetStats(ctx, page.KnowledgeBaseID)
		require.NoError(t, err)
		require.Equal(t, want, stats.PendingIssues)
		issues, err := wikiSvc.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
		require.NoError(t, err)
		require.Len(t, issues, int(want))
	}
	assertPending(1)
	content := "First edit of the editable working copy"
	_, err := chunkSourceChangeService(db).UpdateDocumentChunk(ctx, chunk.ID, &content, nil, nil)
	require.NoError(t, err)
	assertPending(2)
	issues, err := wikiSvc.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	var sourceIssue *types.WikiPageIssue
	for _, issue := range issues {
		if issue.IssueType == types.WikiIssueTypeSourceModified {
			sourceIssue = issue
		}
	}
	require.NotNil(t, sourceIssue)
	require.Equal(t, 1, sourceIssue.ModifiedChunkCount)
	require.Equal(t, page.Slug, sourceIssue.Slug, "the existing list carries the page navigation target")
	_, err = wikiSvc.RegeneratePage(ctx, page.KnowledgeBaseID, page.Slug, queue)
	require.NoError(t, err)
	assertPending(2)
	var op WikiPendingOp
	require.Len(t, pending.guardedOps, 1)
	require.NoError(t, json.Unmarshal(pending.guardedOps[0].Payload, &op))
	model := &sourceChangesChat{templateCaptureChatModel: &templateCaptureChatModel{response: "SUMMARY: refreshed\nRegenerated Wiki content"}}
	ingest := &wikiIngestService{wikiService: wikiSvc, chunkRepo: chunkRepo}
	require.NoError(t, ingest.regenerateSourcePage(ctx, model,
		WikiIngestPayload{TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID}, op,
		ingest.newWikiBatchContext(page.KnowledgeBaseID, nil)))
	assertPending(1)
	regenerated, err := wikiSvc.GetPageBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	require.NoError(t, err)
	require.Empty(t, regenerated.SourceChanges)
	issues, err = wikiSvc.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "")
	require.NoError(t, err)
	require.Len(t, issues, 2, "only the pre-existing pending and resolved issues remain")

	content = "Second edit of the editable working copy"
	_, err = chunkSourceChangeService(db).UpdateDocumentChunk(ctx, chunk.ID, &content, nil, nil)
	require.NoError(t, err)
	assertPending(2)
	ignored, err := wikiSvc.IgnoreSourceChanges(ctx, page.KnowledgeBaseID, page.Slug)
	require.NoError(t, err)
	require.Empty(t, ignored.SourceChanges)
	require.Equal(t, regenerated.Content, ignored.Content)
	require.Equal(t, regenerated.Version, ignored.Version)
	assertPending(1)
}

func TestWikiSourceIssueFailureRollsBackWorkingCopyEdit(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	chunk := sourceChangeChunk(t, db, "Immutable source content")
	page := sourceChangePage(t, db, "concept/atomic-issues", chunk.ID)
	require.NoError(t, db.Exec("ALTER TABLE wiki_page_issues ADD CONSTRAINT reject_source_issue CHECK (issue_type <> 'source_modified')").Error)
	content := "A working-copy edit that must roll back"
	_, err := chunkSourceChangeService(db).UpdateDocumentChunk(ctx, chunk.ID, &content, nil, nil)
	require.Error(t, err)
	chunkRepo := repository.NewChunkRepository(db)
	storedChunk, err := chunkRepo.GetChunkByID(ctx, chunk.TenantID, chunk.ID)
	require.NoError(t, err)
	require.Equal(t, chunk.Content, storedChunk.Content)
	require.Equal(t, 0, storedChunk.ContentRevision)
	revisions, err := chunkRepo.ListChunkRevisions(ctx, chunk.TenantID, chunk.ID)
	require.NoError(t, err)
	require.Empty(t, revisions)
	wikiRepo := repository.NewWikiPageRepository(db)
	storedPage, err := wikiRepo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, storedPage.SourceChanges)
	issues, err := wikiRepo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
	require.NoError(t, err)
	require.Empty(t, issues)
}
