package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestWikiSourceChangesMergeAndPreserveConcurrentEdits(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()
	page := &types.WikiPage{
		ID: "source-page", TenantID: 1, KnowledgeBaseID: "source-kb", Slug: "concept/source",
		Title: "Source", Content: "Original Wiki page", PageType: types.WikiPageTypeConcept,
		Status: types.WikiPageStatusPublished, Version: 1, ChunkRefs: types.StringArray{"first", "second"},
	}
	require.NoError(t, repo.Create(ctx, page))
	first := time.Date(2026, 10, 8, 1, 0, 0, 0, time.UTC)
	latest := first.Add(time.Minute)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first", "first", "not-cited"}, first))
	snapshot, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, types.WikiSourceChanges{"first": first}, snapshot.SourceChanges)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first", "second"}, latest))
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{"first"}, first))
	require.NoError(t, repo.MarkSourceChanged(ctx, 2, []string{"first"}, latest.Add(time.Hour)))
	// An old metadata snapshot cannot overwrite a newly recorded warning.
	require.NoError(t, repo.UpdateMeta(ctx, snapshot))
	snapshot.Content = "Manual page edit"
	require.NoError(t, repo.Update(ctx, snapshot))
	marked, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, types.WikiSourceChanges{"first": latest, "second": latest}, marked.SourceChanges)
	// A regeneration of the old first-chunk snapshot leaves both later edits.
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, types.WikiSourceChanges{"first": first}))
	marked, err = repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Len(t, marked.SourceChanges, 2)
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, types.WikiSourceChanges{"second": latest}))
	marked, err = repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Equal(t, types.WikiSourceChanges{"first": latest}, marked.SourceChanges)
	require.NoError(t, repo.ClearSourceChanges(ctx, page.KnowledgeBaseID, page.Slug, nil))
	marked, err = repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, marked.SourceChanges)
	require.Equal(t, "Manual page edit", marked.Content)
}
