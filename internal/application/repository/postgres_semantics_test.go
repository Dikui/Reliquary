package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func TestWikiListPostgresMatchesExactCategoryPath(t *testing.T) {
	db := setupWikiPagesTestDB(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()

	for _, page := range []*types.WikiPage{
		makeCategorizedWikiPage("kb-path", "entity/exact", types.WikiPageTypeEntity, types.WikiPageStatusPublished, "AI", "RAG"),
		makeCategorizedWikiPage("kb-path", "entity/nested", types.WikiPageTypeEntity, types.WikiPageStatusPublished, "AI", "RAG", "Advanced"),
		makeCategorizedWikiPage("kb-path", "entity/reversed", types.WikiPageTypeEntity, types.WikiPageStatusPublished, "RAG", "AI"),
		makeCategorizedWikiPage("kb-other", "entity/foreign", types.WikiPageTypeEntity, types.WikiPageStatusPublished, "AI", "RAG"),
	} {
		require.NoError(t, repo.Create(ctx, page))
	}

	pages, total, err := repo.List(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: "kb-path",
		CategoryPath:    types.StringArray{" AI ", "RAG"},
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, pages, 1)
	assert.Equal(t, "entity/exact", pages[0].Slug)
}

func TestWikiListPostgresCategoryRankIgnoresStaleDepth(t *testing.T) {
	db := setupWikiPagesTestDB(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()

	root := makeWikiPage("kb-rank", "entity/000-root", types.WikiPageTypeEntity, types.WikiPageStatusPublished)
	root.CategoryPath = types.StringArray{}
	root.Depth = 99
	child := makeCategorizedWikiPage("kb-rank", "entity/999-child", types.WikiPageTypeEntity, types.WikiPageStatusPublished, "Folder")
	child.Depth = 0
	for _, page := range []*types.WikiPage{root, child} {
		require.NoError(t, repo.Create(ctx, page))
	}

	pages, total, err := repo.List(ctx, &types.WikiPageListRequest{
		KnowledgeBaseID: "kb-rank", SortBy: "wiki_path", SortOrder: "asc",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, pages, 2)
	assert.Equal(t, child.ID, pages[0].ID)
	assert.Equal(t, root.ID, pages[1].ID)
}

func TestWikiOrphansPostgresDistinguishesJSONBEmptyArray(t *testing.T) {
	db := setupWikiPagesTestDB(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()

	for _, row := range []struct {
		slug    string
		inLinks interface{}
	}{
		{"entity/empty", `[]`},
		{"entity/absent", nil},
		{"entity/linked", `["entity/source"]`},
		{"entity/json-null", `null`},
		{"entity/string", `"[]"`},
	} {
		page := makeWikiPage("kb-links", row.slug, types.WikiPageTypeEntity, types.WikiPageStatusPublished)
		require.NoError(t, repo.Create(ctx, page))
		require.NoError(t, db.Exec("UPDATE wiki_pages SET in_links = ?::jsonb WHERE id = ?", row.inLinks, page.ID).Error)
	}

	count, err := repo.CountOrphans(ctx, "kb-links")
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)
}

func TestWikiNormalizedTitlePostgresRemovesWhitespace(t *testing.T) {
	db := setupWikiPagesTestDB(t)
	repo := NewWikiPageRepository(db)
	ctx := context.Background()

	for _, row := range []struct {
		kb, slug, title, pageType, status string
	}{
		{"kb-title", "entity/spaced", " A C\tM\nE ", types.WikiPageTypeEntity, types.WikiPageStatusPublished},
		{"kb-title", "entity/plain", "Acme", types.WikiPageTypeEntity, types.WikiPageStatusPublished},
		{"kb-title", "entity/longer", "Acme Corp", types.WikiPageTypeEntity, types.WikiPageStatusPublished},
		{"kb-title", "entity/archived", "Acme", types.WikiPageTypeEntity, types.WikiPageStatusArchived},
		{"kb-title", "concept/other-type", "Acme", types.WikiPageTypeConcept, types.WikiPageStatusPublished},
		{"kb-other", "entity/foreign", "Acme", types.WikiPageTypeEntity, types.WikiPageStatusPublished},
	} {
		page := makeWikiPage(row.kb, row.slug, row.pageType, row.status)
		page.Title = row.title
		require.NoError(t, repo.Create(ctx, page))
	}

	pages, err := repo.FindPagesByNormalizedTitle(ctx, "kb-title", types.WikiPageTypeEntity, "acme")
	require.NoError(t, err)
	require.Len(t, pages, 2)
	assert.Equal(t, "entity/plain", pages[0].Slug)
	assert.Equal(t, "entity/spaced", pages[1].Slug)
}

func TestUpdateChunksPostgresCastsBooleansAndIntegers(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()
	previousTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	first := makeChunk("kb-batch", "doc", types.ChunkTypeText)
	second := makeChunk("kb-batch", "doc", types.ChunkTypeText)
	untouched := makeChunk("kb-batch", "doc", types.ChunkTypeText)
	for _, chunk := range []*types.Chunk{first, second, untouched} {
		chunk.UpdatedAt = previousTime
	}
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{first, second, untouched}))

	first.Content, first.IsEnabled, first.Flags, first.Status = "changed first", false, 4, int(types.ChunkStatusIndexed)
	second.Content, second.IsEnabled, second.Flags, second.Status = "changed second", true, 2, int(types.ChunkStatusStored)
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var transactionTime time.Time
	require.NoError(t, tx.Raw("SELECT NOW()").Scan(&transactionTime).Error)
	require.NoError(t, NewChunkRepository(tx).UpdateChunks(ctx, []*types.Chunk{first, second}))

	for _, expected := range []*types.Chunk{first, second} {
		var saved types.Chunk
		require.NoError(t, tx.First(&saved, "id = ?", expected.ID).Error)
		assert.Equal(t, expected.Content, saved.Content)
		assert.Equal(t, expected.IsEnabled, saved.IsEnabled)
		assert.Equal(t, expected.Flags, saved.Flags)
		assert.Equal(t, expected.Status, saved.Status)
		assert.True(t, saved.UpdatedAt.Equal(transactionTime))
	}
	var savedUntouched types.Chunk
	require.NoError(t, tx.First(&savedUntouched, "id = ?", untouched.ID).Error)
	assert.Equal(t, untouched.Content, savedUntouched.Content)
	assert.True(t, savedUntouched.UpdatedAt.Equal(previousTime))
}

func TestUpdateChunkFlagsBatchPostgresPreservesScopeAndTimestamp(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()
	previousTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	target := makeChunk("kb-flags", "doc", types.ChunkTypeText)
	foreignKB := makeChunk("kb-other", "doc", types.ChunkTypeText)
	foreignTenant := makeChunk("kb-flags", "doc", types.ChunkTypeText)
	foreignTenant.TenantID = 2
	for _, chunk := range []*types.Chunk{target, foreignKB, foreignTenant} {
		chunk.Flags = types.ChunkFlagRecommended | types.ChunkFlags(2)
		chunk.UpdatedAt = previousTime
	}
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{target, foreignKB, foreignTenant}))

	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	var transactionTime time.Time
	require.NoError(t, tx.Raw("SELECT NOW()").Scan(&transactionTime).Error)
	require.NoError(t, NewChunkRepository(tx).UpdateChunkFlagsBatch(ctx, 1, "kb-flags",
		map[string]types.ChunkFlags{target.ID: 4, foreignKB.ID: 4, foreignTenant.ID: 4},
		map[string]types.ChunkFlags{target.ID: types.ChunkFlagRecommended},
	))

	var saved types.Chunk
	require.NoError(t, tx.First(&saved, "id = ?", target.ID).Error)
	assert.Equal(t, types.ChunkFlags(6), saved.Flags)
	assert.True(t, saved.UpdatedAt.Equal(transactionTime))
	for _, untouched := range []*types.Chunk{foreignKB, foreignTenant} {
		var savedUntouched types.Chunk
		require.NoError(t, tx.First(&savedUntouched, "id = ?", untouched.ID).Error)
		assert.Equal(t, untouched.Flags, savedUntouched.Flags)
		assert.True(t, savedUntouched.UpdatedAt.Equal(previousTime))
	}
}
