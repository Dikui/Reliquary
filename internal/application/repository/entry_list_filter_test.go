package repository

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/types"
)

// insertEntryInLibrary seeds a entry row in the given Library with the given
// parse_status so we can assert the list filter's visibility rules.
func insertEntryInLibrary(t *testing.T, db *gorm.DB, spaceID uint64, libraryID, status string) string {
	t.Helper()
	id := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO entries (id, space_id, library_id, type, title, source, parse_status)
		VALUES (?, ?, ?, 'file', 'list-filter-test', 'manual', ?)
	`, id, spaceID, libraryID, status).Error)
	return id
}

// TestListPaged_ExcludesDeletingByDefault documents the fix for issue #2192:
// a entry row that is mid-deletion (parse_status = 'deleting') must not
// appear in the default document list, otherwise a successful async delete
// keeps showing the entry until the row is physically gone. Rows in other
// states (including the terminal 'failed' a stuck delete resolves to) stay
// visible, and an explicit parse_status=deleting filter can still surface them.
func TestListPaged_ExcludesDeletingByDefault(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	const spaceID = uint64(1)
	libraryID := uuid.New().String()

	completedID := insertEntryInLibrary(t, db, spaceID, libraryID, "completed")
	failedID := insertEntryInLibrary(t, db, spaceID, libraryID, "failed")
	deletingID := insertEntryInLibrary(t, db, spaceID, libraryID, types.ParseStatusDeleting)

	page := &types.Pagination{Page: 1, PageSize: 100}

	// Default listing (no explicit parse_status): deleting rows are hidden.
	rows, total, err := repo.ListPagedEntryByLibraryID(
		ctx, spaceID, libraryID, page, types.EntryListFilter{},
	)
	require.NoError(t, err)
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	assert.ElementsMatch(t, []string{completedID, failedID}, ids,
		"default list must include completed + failed but exclude deleting")
	assert.Equal(t, int64(2), total, "count must match the filtered rows")

	// Explicit parse_status=deleting still surfaces the in-flight row so
	// operators / tooling can inspect stuck deletes.
	rows, total, err = repo.ListPagedEntryByLibraryID(
		ctx, spaceID, libraryID, page,
		types.EntryListFilter{ParseStatus: types.ParseStatusDeleting},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, deletingID, rows[0].ID)
	assert.Equal(t, int64(1), total)
}

func TestListPagedLibraryScopeFiltersBeforePagination(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	first, second, hidden := uuid.NewString(), uuid.NewString(), uuid.NewString()
	insertEntryInLibrary(t, db, 1, first, "completed")
	insertEntryInLibrary(t, db, 1, second, "completed")
	insertEntryInLibrary(t, db, 1, hidden, "completed")
	insertEntryInLibrary(t, db, 2, first, "completed")
	filter := types.EntryListFilter{LibraryIDs: []string{first, second}}
	rows, total, err := repo.ListPagedEntryByLibraryID(context.Background(), 1, "", &types.Pagination{Page: 1, PageSize: 1}, filter)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, total)
	require.Contains(t, filter.LibraryIDs, rows[0].LibraryID)
	rows, total, err = repo.ListPagedEntryByLibraryID(context.Background(), 1, "", &types.Pagination{Page: 2, PageSize: 1}, filter)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.EqualValues(t, 2, total)
	require.Contains(t, filter.LibraryIDs, rows[0].LibraryID)
}
