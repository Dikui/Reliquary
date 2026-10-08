package repository

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func sourceIssuePage(t *testing.T, db *gorm.DB, tenantID uint64, kbID, slug string, refs ...string) *types.WikiPage {
	t.Helper()
	page := &types.WikiPage{
		ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kbID, Slug: slug,
		Title: "Referenced page", Content: "Wiki content", PageType: types.WikiPageTypeConcept,
		Status: types.WikiPageStatusPublished, Version: 1, ChunkRefs: refs,
	}
	require.NoError(t, NewWikiPageRepository(db).Create(context.Background(), page))
	return page
}

func TestWikiSourceIssuesAppearInExistingListsAndMerge(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()
	page := sourceIssuePage(t, db, 1, "source-kb", "concept/source", "first", "second")
	other := sourceIssuePage(t, db, 1, "source-kb", "concept/other", "first")
	unrelated := sourceIssuePage(t, db, 1, "source-kb", "concept/unrelated", "unrelated")
	sourceIssuePage(t, db, 1, "other-kb", page.Slug, "first")
	sourceIssuePage(t, db, 2, "tenant-kb", page.Slug, "first")
	ordinary := &types.WikiPageIssue{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID, Slug: page.Slug,
		IssueType: "other", Description: "Existing pending issue", Status: "pending", ReportedBy: "user",
	}
	require.NoError(t, repo.CreateIssue(ctx, ordinary))
	first := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first", "first", "not-cited"}, first))
	issues, err := repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
	require.NoError(t, err)
	require.Len(t, issues, 3, "two source issues and the existing issue share one list")
	var sourceIssueID string
	for _, issue := range issues {
		if issue.IssueType == types.WikiIssueTypeSourceModified {
			require.Equal(t, 1, issue.ModifiedChunkCount)
			require.Equal(t, page.Title, issue.WikiPageTitle)
			require.Contains(t, []string{page.Slug, other.Slug}, issue.Slug)
			if issue.Slug == page.Slug {
				sourceIssueID = issue.ID
			}
		}
	}
	require.NotEmpty(t, sourceIssueID)
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, unrelated.Slug, "pending")
	require.NoError(t, err)
	require.Empty(t, issues)
	issues, err = repo.ListIssues(ctx, "tenant-kb", "", "pending")
	require.NoError(t, err)
	require.Empty(t, issues, "a different tenant's references must remain unchanged")
	issues, err = repo.ListIssues(ctx, "other-kb", "", "pending")
	require.NoError(t, err)
	require.Len(t, issues, 1, "same-tenant pages in another knowledge base retain their own issue")

	latest := first.Add(time.Minute)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first", "second"}, latest))
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, first))
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	require.Len(t, issues, 2)
	var merged *types.WikiPageIssue
	for _, issue := range issues {
		if issue.IssueType == types.WikiIssueTypeSourceModified {
			merged = issue
		}
	}
	require.NotNil(t, merged)
	require.Equal(t, sourceIssueID, merged.ID)
	require.Equal(t, 2, merged.ModifiedChunkCount)
	var storedCount int64
	require.NoError(t, db.Model(&types.WikiPageIssue{}).
		Where("knowledge_base_id = ? AND slug = ? AND issue_type = ?", page.KnowledgeBaseID, page.Slug, types.WikiIssueTypeSourceModified).
		Count(&storedCount).Error)
	require.Equal(t, int64(1), storedCount)

	// A regeneration snapshot can only acknowledge the matching changes.
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, types.WikiSourceChanges{"first": first}))
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	require.Len(t, issues, 2)
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, types.WikiSourceChanges{"second": latest}))
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	for _, issue := range issues {
		if issue.IssueType == types.WikiIssueTypeSourceModified {
			require.Equal(t, 1, issue.ModifiedChunkCount)
		}
	}
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, types.WikiSourceChanges{"first": latest}))
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "")
	require.NoError(t, err)
	require.Len(t, issues, 1, "acknowledged source issues disappear even from an unfiltered list")
	require.Equal(t, ordinary.ID, issues[0].ID)
	// Editing again creates a pending entry through the same data flow.
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, latest.Add(time.Minute)))
	issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	require.Len(t, issues, 2)
}

func TestWikiSourceIssueStatusClearsMarker(t *testing.T) {
	for _, status := range []string{"ignored", "resolved"} {
		t.Run(status, func(t *testing.T) {
			db := pgtest.Open(t)
			repo := NewWikiPageRepository(db)
			ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
			page := sourceIssuePage(t, db, 1, "status-kb", "concept/status", "first")
			require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, time.Now()))
			issues, err := repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
			require.NoError(t, err)
			require.Len(t, issues, 1)
			foreign := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(2))
			require.ErrorIs(t, repo.UpdateIssueStatus(foreign, issues[0].ID, status), ErrWikiPageNotFound)
			require.NoError(t, repo.UpdateIssueStatus(ctx, issues[0].ID, status))
			stored, err := repo.GetByID(ctx, page.ID)
			require.NoError(t, err)
			require.Empty(t, stored.SourceChanges)
			require.Equal(t, page.Content, stored.Content)
			require.Equal(t, page.Version, stored.Version)
			issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, "", "")
			require.NoError(t, err)
			require.Empty(t, issues)
		})
	}
}

func TestWikiSourceIssuesExcludeDeletedAndArchivedPages(t *testing.T) {
	for _, removal := range []string{"delete_slug", "delete_id", "direct_soft_delete", "archive"} {
		t.Run(removal, func(t *testing.T) {
			db := pgtest.Open(t)
			repo := NewWikiPageRepository(db)
			ctx := context.Background()
			page := sourceIssuePage(t, db, 1, "removed-kb", "concept/removed", "first")
			require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, time.Now()))
			switch removal {
			case "delete_slug":
				require.NoError(t, repo.Delete(ctx, page.KnowledgeBaseID, page.Slug))
			case "delete_id":
				require.NoError(t, repo.DeleteByID(ctx, page.ID))
			case "direct_soft_delete":
				require.NoError(t, db.Delete(page).Error)
			case "archive":
				require.NoError(t, db.Model(page).Update("status", types.WikiPageStatusArchived).Error)
			}
			issues, err := repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
			require.NoError(t, err)
			require.Empty(t, issues)
			if removal == "archive" {
				// Changes while archived remain hidden until the page is restored.
				require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, time.Now()))
				issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
				require.NoError(t, err)
				require.Empty(t, issues)
				require.NoError(t, db.Model(page).Update("status", types.WikiPageStatusPublished).Error)
				issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
				require.NoError(t, err)
				require.Len(t, issues, 1)
			} else {
				// Reusing a deleted page's slug must not revive the previous issue.
				replacement := sourceIssuePage(t, db, 1, page.KnowledgeBaseID, page.Slug, "first")
				require.NotEqual(t, page.ID, replacement.ID)
				require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, time.Now()))
				issues, err = repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
				require.NoError(t, err)
				require.Len(t, issues, 1)
			}
		})
	}
}

func TestWikiSourceIssuesConcurrentChangesDoNotDuplicate(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()
	page := sourceIssuePage(t, db, 1, "concurrent-kb", "concept/concurrent", "first", "second")
	start := make(chan struct{})
	errors := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			<-start
			errors <- repo.MarkSourceChanged(ctx, 1, []string{id}, time.Now())
		}(id)
	}
	close(start)
	require.NoError(t, <-errors)
	require.NoError(t, <-errors)
	issues, err := repo.ListIssues(ctx, page.KnowledgeBaseID, page.Slug, "pending")
	require.NoError(t, err)
	require.Len(t, issues, 1)
	require.Equal(t, 2, issues[0].ModifiedChunkCount)
}

func TestWikiSourceIssueWriteFailureRollsBackMarker(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()
	page := sourceIssuePage(t, db, 1, "atomic-kb", "concept/atomic", "first")
	require.NoError(t, db.Exec("ALTER TABLE wiki_page_issues ADD CONSTRAINT reject_source_issue CHECK (issue_type <> 'source_modified')").Error)
	require.Error(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, time.Now()))
	stored, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, stored.SourceChanges, "a source warning without its issue must never be committed")
	issues, err := repo.ListIssues(ctx, page.KnowledgeBaseID, "", "pending")
	require.NoError(t, err)
	require.Empty(t, issues)
}
