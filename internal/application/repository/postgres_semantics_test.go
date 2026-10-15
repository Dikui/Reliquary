package repository

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func TestUpdateChunksPostgresCastsBooleansAndIntegers(t *testing.T) {
	db := setupChunkTestDB(t)
	repo := NewChunkRepository(db)
	ctx := context.Background()
	previousTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	first := makeChunk("library-batch", "doc", types.ChunkTypeText)
	second := makeChunk("library-batch", "doc", types.ChunkTypeText)
	untouched := makeChunk("library-batch", "doc", types.ChunkTypeText)
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
