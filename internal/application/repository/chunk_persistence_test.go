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
	require.NoError(t, db.AutoMigrate(&types.Chunk{}))
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

	enabledChunk := makeChunk("kb-1", "document-knowledge", types.ChunkTypeText)
	disabledChunk := makeChunk("kb-1", "document-knowledge", types.ChunkTypeText)
	require.NoError(t, repo.CreateChunks(ctx, []*types.Chunk{enabledChunk, disabledChunk}))
	require.NoError(t, db.Model(&types.Chunk{}).
		Where("id = ?", disabledChunk.ID).
		Update("is_enabled", false).Error)

	enabled := true
	chunks, total, err := repo.ListPagedChunksByKnowledgeID(ctx,
		1,
		"document-knowledge",
		&types.Pagination{Page: 1, PageSize: 20},
		[]types.ChunkType{types.ChunkTypeText},
		"",
		"",
		&enabled)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, chunks, 1)
	assert.Equal(t, enabledChunk.ID, chunks[0].ID)

	allChunks, allTotal, err := repo.ListPagedChunksByKnowledgeID(ctx,
		1,
		"document-knowledge",
		&types.Pagination{Page: 1, PageSize: 20},
		[]types.ChunkType{types.ChunkTypeText},
		"",
		"",
		nil)
	require.NoError(t, err)
	assert.Equal(t, int64(2), allTotal)
	assert.Len(t, allChunks, 2)
}

func TestUpdateChunkPersistsContent(t *testing.T) {
	db := setupChunkTestDB(t)
	ctx := context.Background()

	kbID := uuid.New().String()
	knowledgeID := uuid.New().String()

	chunk := makeChunk(kbID, knowledgeID, "text")
	require.NoError(t, db.WithContext(ctx).Create(chunk).Error)

	// Test updating a chunk field ; verifies no NOW() related errors
	err := db.WithContext(ctx).Model(chunk).Update("content", "updated content").Error
	assert.NoError(t, err)

	var saved types.Chunk
	require.NoError(t, db.First(&saved, "id = ?", chunk.ID).Error)
	assert.Equal(t, "updated content", saved.Content)
}
