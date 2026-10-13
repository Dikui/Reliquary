package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// librariesTestDDL mirrors the `libraries` section of
// the application library schema with native PostgreSQL JSONB fields.
const librariesTestDDL = `
CREATE TABLE IF NOT EXISTS libraries (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    space_id INTEGER NOT NULL,
    type VARCHAR(32) NOT NULL DEFAULT 'document',
    chunking_config JSONB NOT NULL DEFAULT '{}',
    embedding_model_id VARCHAR(64) NOT NULL,
    summary_model_id VARCHAR(64) NOT NULL,
    model_tiers JSONB NOT NULL DEFAULT '{}',
    indexing_strategy JSONB,
    creator_id VARCHAR(36),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);
`

// setupLibraryTestDB creates an isolated PostgreSQL fixture for repository queries.
func setupLibraryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(librariesTestDDL).Error)
	return db
}

// makeLibrary builds a minimal Library suitable for insert. Only the columns
// required by the schema above and any field we explicitly care about are set;
// the rest are left at their Go zero values and serialize to "{}" via their
// custom JSON valuers.
func makeLibrary() *types.Library {
	return &types.Library{
		ID:               uuid.New().String(),
		Name:             "test-library",
		SpaceID:          1,
		EmbeddingModelID: "test-embed",
		SummaryModelID:   "test-summary",
	}
}

func TestLibraryLookupOnlyReturnsPersonalSpace(t *testing.T) {
	db := setupLibraryTestDB(t)
	repo := NewLibraryRepository(db)
	teamLibrary, foreignLibrary := makeLibrary(), makeLibrary()
	foreignLibrary.SpaceID = 2
	require.NoError(t, db.Create(teamLibrary).Error)
	require.NoError(t, db.Create(foreignLibrary).Error)
	ctx := db.Statement.Context
	got, err := repo.GetLibraryByID(ctx, teamLibrary.ID)
	require.NoError(t, err)
	require.Equal(t, teamLibrary.ID, got.ID)
	_, err = repo.GetLibraryByID(ctx, foreignLibrary.ID)
	require.ErrorIs(t, err, ErrLibraryNotFound)
	rows, err := repo.GetLibraryByIDs(ctx, []string{teamLibrary.ID, foreignLibrary.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, teamLibrary.ID, rows[0].ID)
}
