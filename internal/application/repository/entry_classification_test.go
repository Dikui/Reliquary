package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestEntryContentTypeFilterIncludesUnclassifiedAndRespectsScope(t *testing.T) {
	db := pgtest.Open(t)
	libraryID := uuid.NewString()
	for _, value := range []string{types.ContentTypeEvent, types.ContentTypeTutorial, ""} {
		require.NoError(t, db.Create(&types.Entry{SpaceID: 1, LibraryID: libraryID, Type: "file", Title: "Article", Source: "manual", ParseStatus: types.ParseStatusCompleted, ContentType: value}).Error)
	}
	require.NoError(t, db.Exec(`INSERT INTO entries (space_id, library_id, type, title, source, parse_status, content_type) VALUES (1,?, 'file','Unclassified','manual','completed',NULL)`, libraryID).Error)
	require.NoError(t, db.Create(&types.Entry{SpaceID: 2, LibraryID: libraryID, Type: "file", Title: "Other space", Source: "manual", ParseStatus: types.ParseStatusCompleted, ContentType: types.ContentTypeEvent}).Error)
	repo := NewEntryRepository(db)
	page := &types.Pagination{Page: 1, PageSize: 10}
	rows, total, err := repo.ListPagedEntryByLibraryID(context.Background(), 1, libraryID, page, types.EntryListFilter{ContentType: types.ContentTypeEvent})
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.Equal(t, types.ContentTypeEvent, rows[0].ContentType)
	rows, total, err = repo.ListPagedEntryByLibraryID(context.Background(), 1, libraryID, page, types.EntryListFilter{ContentType: "unclassified"})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, rows, 2)
	for _, row := range rows {
		require.Empty(t, row.ContentType)
	}
}
