package service

import (
	"reliquary/internal/types"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func TestImportedDocumentSearchUsesSummaryFilters(t *testing.T) {
	h := newEntrySearchFixture(t)
	mine := h.add(t, "imported", "mine", "2026-10-01", "tutorial", "semanticlaunch", 0)
	mine.StructuredSummary = &types.EntrySummary{Overview: "semanticlaunch"}
	mine.Description = mine.StructuredSummary.RenderText()
	mine.Starred = true
	require.NoError(t, h.db.Save(mine).Error)
	vector := pgvector.NewHalfVector(entrySearchFixtureEmbedding(mine.Title + "\n" + mine.Description))
	require.NoError(
		t,
		h.db.Exec(
			`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash)
		VALUES (?,1,?,'fixture-model',3,?,'fixture')`,
			mine.ID,
			h.library.ID,
			vector,
		).Error,
	)
	require.NoError(
		t,
		h.db.Create(
			&types.Entity{
				ID:             "document-entity",
				SpaceID:        1,
				CanonicalName:  "Document entity",
				NormalizedName: "document entity",
				EntityType:     "organization",
			},
		).Error,
	)
	require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: mine.ID, EntityID: "document-entity"}).Error)
	h.library.IndexingStrategy.KeywordEnabled = false
	require.NoError(t, h.db.Model(h.library).Update("indexing_strategy", h.library.IndexingStrategy).Error)
	starred := true
	result := h.search(
		t,
		types.EntrySearchRequest{
			Query:     "semanticlaunch",
			Sources:   []string{"mine"},
			EntityIDs: []string{"document-entity"},
			Starred:   &starred,
		},
	)
	require.Equal(t, []string{mine.ID}, entrySearchIDs(result))
	require.NotNil(t, result.Results[0].Summary)
	require.Len(t, result.Results[0].Entities, 1)

	h.add(t, "article", "article", "2026-10-02", "event", "semanticlaunch", 0)
	require.NoError(t, h.db.Exec("UPDATE article_embeddings SET embedding=? WHERE entry_id=?", vector, "article").Error)
	result = h.search(t, types.EntrySearchRequest{Query: "semanticlaunch"})
	require.Len(t, result.Results, 2)
}
