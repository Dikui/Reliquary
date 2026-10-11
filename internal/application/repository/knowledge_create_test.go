package repository

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestCreateKnowledgeDefaultsCustomMetadataToEmptyObject(t *testing.T) {
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}))

	repo := NewKnowledgeRepository(db)
	knowledge := &types.Knowledge{
		SpaceID:         1,
		KnowledgeBaseID: "kb-1",
		Type:            "file",
		Title:           "document.txt",
		ParseStatus:     types.ParseStatusPending,
		EnableStatus:    "disabled",
	}

	require.NoError(t, repo.CreateKnowledge(context.Background(), knowledge))
	require.JSONEq(t, `{}`, string(knowledge.CustomMetadata))

	persisted, err := repo.GetKnowledgeByID(context.Background(), knowledge.SpaceID, knowledge.ID)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(persisted.CustomMetadata))
}
