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

// insertEntryInKB seeds a entry row in the given KB with the given
// parse_status so we can assert the list filter's visibility rules.
func insertEntryInKB(t *testing.T, db *gorm.DB, spaceID uint64, kbID, status string) string {
	t.Helper()
	id := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO entries (id, space_id, knowledge_base_id, type, title, source, parse_status)
		VALUES (?, ?, ?, 'file', 'list-filter-test', 'manual', ?)
	`, id, spaceID, kbID, status).Error)
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
	kbID := uuid.New().String()

	completedID := insertEntryInKB(t, db, spaceID, kbID, "completed")
	failedID := insertEntryInKB(t, db, spaceID, kbID, "failed")
	deletingID := insertEntryInKB(t, db, spaceID, kbID, types.ParseStatusDeleting)

	page := &types.Pagination{Page: 1, PageSize: 100}

	// Default listing (no explicit parse_status): deleting rows are hidden.
	rows, total, err := repo.ListPagedEntryByKnowledgeBaseID(
		ctx, spaceID, kbID, page, types.EntryListFilter{},
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
	rows, total, err = repo.ListPagedEntryByKnowledgeBaseID(
		ctx, spaceID, kbID, page,
		types.EntryListFilter{ParseStatus: types.ParseStatusDeleting},
	)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, deletingID, rows[0].ID)
	assert.Equal(t, int64(1), total)
}
