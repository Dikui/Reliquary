package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

// MEL-02: medium regression/spec; switching a populated library writes a log
// using the same operation type accepted by the public log filter.
func TestLibraryModeSwitchRecordsOperationLog(t *testing.T) {
	db, ctx, _ := entityFixture(t)
	entity := entityRecord("atlas", "Atlas", "company")
	require.NoError(t, db.Create(&entity).Error)
	svc := &libraryService{db: db, repo: repository.NewLibraryRepository(db)}
	mode := types.EntityModeManaged
	schema := types.EntitySchema{Kinds: []types.EntityKind{{Key: "product", Name: "Product"}}}
	library, err := svc.ConfigureLibrary(ctx, "entity-library", types.LibraryPatch{
		EntityMode: &mode, EntitySchema: &schema,
		EntityKindMapping: map[string]string{"company": "product"},
	})
	require.NoError(t, err)
	require.Equal(t, mode, library.EntityMode)
	logs := NewOperationLogService(db)
	page, err := logs.List(ctx, "", 1, 20)
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Equal(t, "switch_entity_library", page.Items[0].OperationType)
	require.Equal(t, library.ID, page.Items[0].ObjectID)
	require.JSONEq(t, `{"entity_kind_mapping":{"company":"product"},"entity_count":1}`,
		string(page.Items[0].After))
	filtered, err := logs.List(ctx, "switch_entity_library", 1, 20)
	require.NoError(t, err)
	require.Equal(t, page, filtered)
}
