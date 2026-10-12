package repository

import (
	"context"

	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHardDeleteEntry(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	const spaceID uint64 = 52
	kbID := uuid.New().String()
	id := uuid.New().String()
	metadata := `{"datasource_id":"ds-1","external_id":"file:1"}`
	require.NoError(t, db.Exec(`
		INSERT INTO entries
		  (id, space_id, knowledge_base_id, type, title, source, parse_status, metadata, deleted_at)
		VALUES (?, ?, ?, 'document', 'file:1', 'rss', 'completed', ?, '2026-01-01 00:00:00')
	`, id, spaceID, kbID, metadata).Error)

	require.NoError(t, repo.HardDeleteEntry(ctx, spaceID, id))

	var count int64
	require.NoError(t, db.Unscoped().Model(&struct{}{}).Table("entries").Where("id = ?", id).Count(&count).Error)
	assert.Equal(t, int64(0), count)
}
