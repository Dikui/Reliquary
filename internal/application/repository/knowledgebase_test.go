package repository

import (
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// knowledgeBasesTestDDL mirrors the `knowledge_bases` section of
// the application knowledge-base schema with native PostgreSQL JSONB fields.
const knowledgeBasesTestDDL = `
CREATE TABLE IF NOT EXISTS knowledge_bases (
    id VARCHAR(36) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    tenant_id INTEGER NOT NULL,
    domain_id VARCHAR(36) NOT NULL,
    type VARCHAR(32) NOT NULL DEFAULT 'document',
    chunking_config JSONB NOT NULL DEFAULT '{}',
    image_processing_config JSONB NOT NULL DEFAULT '{}',
    embedding_model_id VARCHAR(64) NOT NULL,
    summary_model_id VARCHAR(64) NOT NULL,
    model_tiers JSONB NOT NULL DEFAULT '{}',
    vlm_config JSONB NOT NULL DEFAULT '{}',
    extract_config JSONB NULL DEFAULT NULL,
    question_generation_config JSONB NULL,
    auto_tag_config JSONB NULL,
    is_temporary BOOLEAN NOT NULL DEFAULT false,
    is_pinned BOOLEAN NOT NULL DEFAULT false,
    pinned_at TIMESTAMPTZ NULL,
    asr_config JSONB,
    indexing_strategy JSONB,
    creator_id VARCHAR(36),
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMPTZ
);
`

// setupKBTestDB creates an isolated PostgreSQL fixture for repository queries.
func setupKBTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(knowledgeBasesTestDDL).Error)
	return db
}

// makeKB builds a minimal KnowledgeBase suitable for insert. Only the columns
// required by the schema above and any field we explicitly care about are set;
// the rest are left at their Go zero values and serialize to "{}" via their
// custom JSON valuers.
func makeKB() *types.KnowledgeBase {
	return &types.KnowledgeBase{
		DomainID:         "test-domain",
		ID:               uuid.New().String(),
		Name:             "test-kb",
		TenantID:         1,
		EmbeddingModelID: "test-embed",
		SummaryModelID:   "test-summary",
	}
}

func TestKnowledgeBaseLookupOnlyReturnsSingleTeam(t *testing.T) {
	db := setupKBTestDB(t)
	repo := NewKnowledgeBaseRepository(db)
	teamKB, foreignKB := makeKB(), makeKB()
	foreignKB.TenantID = 2
	require.NoError(t, db.Create(teamKB).Error)
	require.NoError(t, db.Create(foreignKB).Error)
	ctx := db.Statement.Context
	got, err := repo.GetKnowledgeBaseByID(ctx, teamKB.ID)
	require.NoError(t, err)
	require.Equal(t, teamKB.ID, got.ID)
	_, err = repo.GetKnowledgeBaseByID(ctx, foreignKB.ID)
	require.ErrorIs(t, err, ErrKnowledgeBaseNotFound)
	rows, err := repo.GetKnowledgeBaseByIDs(ctx, []string{teamKB.ID, foreignKB.ID})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, teamKB.ID, rows[0].ID)
}
