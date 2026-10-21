package service

import (
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestImportedDocumentSearchUsesSummaryFiltersAndFolding(t *testing.T) {
	h := newEntrySearchFixture(t)
	mine := h.add(t, "imported", "mine", "2026-10-01", "tutorial", "semanticlaunch", 0)
	mine.StructuredSummary = &types.EntrySummary{Overview: "semanticlaunch", Points: []string{"Recorded procedure"}}
	mine.Description = mine.StructuredSummary.RenderText()
	mine.Starred = true
	require.NoError(t, h.db.Save(mine).Error)
	vector := pgvector.NewHalfVector(entrySearchFixtureEmbedding(mine.Title + "\n" + mine.Description))
	require.NoError(t, h.db.Exec(`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash)
		VALUES (?,1,?,'fixture-model',3,?,'fixture')`, mine.ID, h.library.ID, vector).Error)
	require.NoError(t, h.db.Create(&types.Entity{ID: "document-entity", SpaceID: 1, CanonicalName: "Document entity", NormalizedName: "document entity", EntityType: "project"}).Error)
	require.NoError(t, h.db.Create(&types.Topic{ID: "document-topic", SpaceID: 1, Name: "Document topic"}).Error)
	require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: mine.ID, EntityID: "document-entity"}).Error)
	require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: mine.ID, TopicID: "document-topic"}).Error)
	h.library.IndexingStrategy.KeywordEnabled = false
	require.NoError(t, h.db.Model(h.library).Update("indexing_strategy", h.library.IndexingStrategy).Error)
	starred := true
	result := h.search(t, types.EntrySearchRequest{Query: "semanticlaunch", Sources: []string{"mine"}, ContentTypes: []string{"tutorial"}, EntityIDs: []string{"document-entity"}, TopicIDs: []string{"document-topic"}, Starred: &starred, CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, []string{mine.ID}, entrySearchIDs(result))
	require.NotNil(t, result.Results[0].Summary)
	require.Len(t, result.Results[0].Entities, 1)
	require.Len(t, result.Results[0].Topics, 1)
	h.add(t, "article", "article", "2026-10-02", "event", "semanticlaunch", 0)
	require.NoError(t, h.db.Exec("UPDATE article_embeddings SET embedding=? WHERE entry_id=?", vector, "article").Error)
	result = h.search(t, types.EntrySearchRequest{Query: "semanticlaunch"})
	require.Len(t, result.Results, 1)
	require.Len(t, result.Results[0].SimilarReports, 1)
	require.ElementsMatch(t, []string{"article", mine.ID}, []string{result.Results[0].EntryID, result.Results[0].SimilarReports[0].EntryID})
}
