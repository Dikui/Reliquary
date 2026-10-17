package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestCheckEntryExists_FileHashIsScopedByFileType(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db)
	ctx := context.Background()
	spaceID := uint64(1)
	libraryID := uuid.NewString()
	const fileHash = "same-content-hash"

	require.NoError(t, db.Exec(`
		INSERT INTO entries (id, space_id, library_id, type, title, file_name, file_type, file_hash, parse_status)
		VALUES (?, ?, ?, 'file', 'document.md', 'document.md', 'md', ?, 'completed')
	`, uuid.NewString(), spaceID, libraryID, fileHash).Error)

	t.Run("same content with another file type is allowed", func(t *testing.T) {
		exists, entry, err := repo.CheckEntryExists(ctx, spaceID, libraryID, &types.EntryCheckParams{
			Type:     "file",
			FileHash: fileHash,
			FileType: "txt",
		})

		require.NoError(t, err)
		assert.False(t, exists)
		assert.Nil(t, entry)
	})

	t.Run("same content and file type remains a duplicate", func(t *testing.T) {
		exists, entry, err := repo.CheckEntryExists(ctx, spaceID, libraryID, &types.EntryCheckParams{
			Type:     "file",
			FileHash: fileHash,
			FileType: "md",
		})

		require.NoError(t, err)
		assert.True(t, exists)
		require.NotNil(t, entry)
		assert.Equal(t, "md", entry.FileType)
	})

	t.Run("file type matching is case-insensitive", func(t *testing.T) {
		exists, entry, err := repo.CheckEntryExists(ctx, spaceID, libraryID, &types.EntryCheckParams{
			Type:     "file",
			FileHash: fileHash,
			FileType: "MD",
		})

		require.NoError(t, err)
		assert.True(t, exists)
		require.NotNil(t, entry)
		assert.Equal(t, "md", entry.FileType)
	})
}

func TestEntryListSeparatesMarkdownFormatFromSubmissionChannel(t *testing.T) {
	db := setupEntryTestDB(t)
	require.NoError(t, db.Exec(`INSERT INTO entries (id, space_id, library_id, type, file_type, channel, parse_status) VALUES
		('ingest',1,'library','manual','manual','api','completed'),
		('markdown',1,'library','file','MD','web','completed'),
		('legacy',1,'library','manual','pdf','web','completed')`).Error)
	var ids []string
	query := applyEntryListFilter(db.Model(&types.Entry{}), types.EntryListFilter{FileType: "md"})
	require.NoError(t, query.Order("id").Pluck("id", &ids).Error)
	require.Equal(t, []string{"ingest", "markdown"}, ids)
	ids = nil
	query = applyEntryListFilter(db.Model(&types.Entry{}), types.EntryListFilter{FileType: "md", Source: "api"})
	require.NoError(t, query.Pluck("id", &ids).Error)
	require.Equal(t, []string{"ingest"}, ids)
	var count int64
	query = applyEntryListFilter(db.Model(&types.Entry{}), types.EntryListFilter{Source: "manual"})
	require.NoError(t, query.Count(&count).Error)
	require.Zero(t, count, "manual storage type must not match a submission channel")
}
