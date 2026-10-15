package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestCreateEntryDefaultsCustomMetadataToEmptyObject(t *testing.T) {
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Entry{}))

	repo := NewEntryRepository(db)
	entry := &types.Entry{
		SpaceID:      1,
		LibraryID:    "library-1",
		Type:         "file",
		Title:        "document.txt",
		ParseStatus:  types.ParseStatusPending,
		EnableStatus: "disabled",
	}

	require.NoError(t, repo.CreateEntry(context.Background(), entry))
	require.JSONEq(t, `{}`, string(entry.CustomMetadata))

	persisted, err := repo.GetEntryByID(context.Background(), entry.SpaceID, entry.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(persisted.CustomMetadata))
}
