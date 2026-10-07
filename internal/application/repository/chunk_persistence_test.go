package repository

import (
	"context"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// setupChunkTestDB creates isolated PostgreSQL chunk and tag tables.
func setupChunkTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Chunk{}, &types.KnowledgeTag{}))
	require.NoError(t, db.Exec(`ALTER TABLE chunks ALTER COLUMN metadata TYPE JSONB USING metadata::jsonb`).Error)
	return db
}

func makeChunk(kbID, knowledgeID string, chunkType string) *types.Chunk {
	return &types.Chunk{
		ID:              uuid.New().String(),
		TenantID:        1,
		KnowledgeBaseID: kbID,
		KnowledgeID:     knowledgeID,
		Content:         "test content",
		ChunkType:       chunkType,
		IsEnabled:       true,
	}
}

func TestCreateChunks_CleansContextHeaderBeforePersistence(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	chunk := makeChunk(uuid.New().String(), uuid.New().String(), types.ChunkTypeText)
	chunk.ContextHeader = "# \x00投资评级说明\xff"

	require.NoError(t, repo.CreateChunks(context.Background(), []*types.Chunk{chunk}))

	require.Equal(t, "# 投资评级说明", chunk.ContextHeader)
	require.NotContains(t, chunk.ContextHeader, "\x00")
	require.True(t, utf8.ValidString(chunk.ContextHeader))

	var saved types.Chunk
	require.NoError(t, db.First(&saved, "id = ?", chunk.ID).Error)
	assert.Equal(t, "# 投资评级说明", saved.ContextHeader)
	assert.NotContains(t, saved.ContextHeader, "\x00")
	assert.True(t, utf8.ValidString(saved.ContextHeader))
}

func TestListPagedChunksByKnowledgeID_FiltersEnabledState(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	enabledChunk := makeChunk("kb-1", "faq-knowledge", types.ChunkTypeFAQ)
	disabledChunk := makeChunk("kb-1", "faq-knowledge", types.ChunkTypeFAQ)
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{enabledChunk, disabledChunk}))
	require.NoError(t, db.Model(&types.Chunk{}).
		Where("id = ?", disabledChunk.ID).
		Update("is_enabled", false).Error)

	enabled := true
	chunks, total, err := repo.ListPagedChunksByKnowledgeID(
		ctx, 1, "faq-knowledge", &types.Pagination{Page: 1, PageSize: 20},
		[]types.ChunkType{types.ChunkTypeFAQ}, nil, "", "", "", types.KnowledgeTypeFAQ, &enabled,
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, chunks, 1)
	assert.Equal(t, enabledChunk.ID, chunks[0].ID)

	allChunks, allTotal, err := repo.ListPagedChunksByKnowledgeID(
		ctx, 1, "faq-knowledge", &types.Pagination{Page: 1, PageSize: 20},
		[]types.ChunkType{types.ChunkTypeFAQ}, nil, "", "", "", types.KnowledgeTypeFAQ, nil,
	)
	require.NoError(t, err)
	assert.Equal(t, int64(2), allTotal)
	assert.Len(t, allChunks, 2)
}

func makeSuggestedFAQChunk(t *testing.T, kbID, knowledgeID, tagID, question string) *types.Chunk {
	t.Helper()
	chunk := makeChunk(kbID, knowledgeID, types.ChunkTypeFAQ)
	chunk.TagID = tagID
	chunk.Flags = types.ChunkFlagRecommended
	require.NoError(t, chunk.SetFAQMetadata(&types.FAQChunkMetadata{StandardQuestion: question}))
	return chunk
}

func makeSuggestedDocumentChunk(t *testing.T, kbID, knowledgeID, question string) *types.Chunk {
	t.Helper()
	chunk := makeChunk(kbID, knowledgeID, types.ChunkTypeText)
	require.NoError(t, chunk.SetDocumentMetadata(&types.DocumentChunkMetadata{
		GeneratedQuestions: []types.GeneratedQuestion{{ID: uuid.NewString(), Question: question}},
	}))
	return chunk
}

func TestListRecommendedFAQChunks_FiltersByTagWithoutWideningToParentKB(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	selectedTag := uuid.NewString()
	otherTag := uuid.NewString()
	selected := makeSuggestedFAQChunk(t, "kb-1", "faq-knowledge", selectedTag, "selected question")
	other := makeSuggestedFAQChunk(t, "kb-1", "faq-knowledge", otherTag, "other question")
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{selected, other}))

	got, err := repo.ListRecommendedFAQChunks(ctx, 1, nil, nil, []string{selectedTag}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, selected.ID, got[0].ID)
}

func TestListRecommendedFAQChunks_UnionsOnlyExplicitScopes(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	selectedTag := uuid.NewString()
	tagged := makeSuggestedFAQChunk(t, "kb-tag", "faq-tag", selectedTag, "tagged question")
	explicitKB := makeSuggestedFAQChunk(t, "kb-explicit", "faq-explicit", uuid.NewString(), "explicit KB question")
	unselected := makeSuggestedFAQChunk(t, "kb-other", "faq-other", uuid.NewString(), "unselected question")
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{tagged, explicitKB, unselected}))

	got, err := repo.ListRecommendedFAQChunks(ctx, 1, []string{"kb-explicit"}, nil, []string{selectedTag}, 10)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{tagged.ID, explicitKB.ID}, []string{got[0].ID, got[1].ID})
}

func TestListRecentDocumentChunksWithQuestions_KnowledgeScopeDoesNotIncludeSiblingDocuments(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	selected := makeSuggestedDocumentChunk(t, "kb-1", "doc-selected", "selected document question")
	sibling := makeSuggestedDocumentChunk(t, "kb-1", "doc-sibling", "sibling document question")
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{selected, sibling}))

	got, err := repo.ListRecentDocumentChunksWithQuestions(ctx, 1, nil, []string{"doc-selected"}, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, selected.ID, got[0].ID)
}

func TestListRecentDocumentChunksWithQuestions_UnionsExplicitKBAndKnowledge(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()

	fromExplicitKB := makeSuggestedDocumentChunk(t, "kb-explicit", "doc-1", "explicit KB question")
	fromExplicitDocument := makeSuggestedDocumentChunk(t, "kb-other", "doc-selected", "selected document question")
	unselected := makeSuggestedDocumentChunk(t, "kb-other", "doc-other", "unselected question")
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{fromExplicitKB, fromExplicitDocument, unselected}))

	got, err := repo.ListRecentDocumentChunksWithQuestions(
		ctx, 1, []string{"kb-explicit"}, []string{"doc-selected"}, 10,
	)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.ElementsMatch(t, []string{fromExplicitKB.ID, fromExplicitDocument.ID}, []string{got[0].ID, got[1].ID})
}

func TestUpdateChunkPersistsContent(t *testing.T) {
	db := setupChunkTestDB(t)
	ctx := context.Background()

	kbID := uuid.New().String()
	knowledgeID := uuid.New().String()

	chunk := makeChunk(kbID, knowledgeID, "faq")
	require.NoError(t, db.WithContext(ctx).Create(chunk).Error)

	// Test updating a chunk field ; verifies no NOW() related errors
	err := db.WithContext(ctx).Model(chunk).Update("content", "updated content").Error
	assert.NoError(t, err)

	var saved types.Chunk
	require.NoError(t, db.First(&saved, "id = ?", chunk.ID).Error)
	assert.Equal(t, "updated content", saved.Content)
}
