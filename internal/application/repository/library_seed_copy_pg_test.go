package repository

import (
	"context"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"
)

func TestSystemLibrarySeedTextPreservesSavedDescription(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Seed text"}).Error)
	_, err := EnsureSystemLibraries(context.Background(), db, 1, "")
	require.NoError(t, err)
	repo := NewLibraryRepository(db)
	library, err := repo.GetSystemLibraryByRole(context.Background(), 1, "media")
	require.NoError(t, err)
	require.NotContains(t, library.Description, "文章")
	legacy := "外部个人的观点：专家、分析师、KOL 的评论与文章。"
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", library.ID).Update("description", legacy).Error)
	_, err = EnsureSystemLibraries(context.Background(), db, 1, "")
	require.NoError(t, err)
	repaired, err := repo.GetSystemLibraryByRole(context.Background(), 1, "media")
	require.NoError(t, err)
	require.Equal(t, legacy, repaired.Description)
	require.NoError(t, db.Model(&types.Library{}).Where("id=?", library.ID).Update("description", "用户自定义说明").Error)
	_, err = EnsureSystemLibraries(context.Background(), db, 1, "")
	require.NoError(t, err)
	custom, err := repo.GetSystemLibraryByRole(context.Background(), 1, "media")
	require.NoError(t, err)
	require.Equal(t, "用户自定义说明", custom.Description)
}
