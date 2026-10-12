package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// setupTestDB creates an isolated PostgreSQL space table.
func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Space{}))
	return db
}

func TestDeleteSpace_SoftDeletesWorkspace(t *testing.T) {
	db := setupTestDB(t)
	ctx := context.Background()
	repo := NewSpaceRepository(db)

	space := &types.Space{Name: "gone", Status: "active"}
	require.NoError(t, db.Create(space).Error)

	require.NoError(t, repo.DeleteSpace(ctx, space.ID))

	var spaceCount int64
	require.NoError(t, db.Model(&types.Space{}).Count(&spaceCount).Error)
	assert.Equal(t, int64(0), spaceCount)

	// Unscoped: rows still exist but are soft-deleted.
	var rawSpaceCount int64
	require.NoError(t, db.Unscoped().Model(&types.Space{}).Count(&rawSpaceCount).Error)
	assert.Equal(t, int64(1), rawSpaceCount)

}
