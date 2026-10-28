package service

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"os"
	"reliquary/internal/application/repository"
	postgres "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/types"
	"testing"
)

func TestNamingFixedQueries(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "alpha", "article", "2026-10-01", "event", "semanticlaunch", 3)
	h.add(t, "beta", "article", "2026-10-02", "event", "semanticlaunch", 1)
	h.add(t, "summary", "article", "2026-10-03", "tutorial", "othersemantic", 0)
	h.add(t, "mine", "mine", "", "", "semanticlaunch", 2)
	h.add(t, "report", "report", "", "", "othersemantic", 1)
	h.add(t, "missing", "article", "", "research", "othersemantic", 1)
	require.NoError(t, h.db.Create(&types.Entity{ID: "entity", SpaceID: 1, LibraryID: h.library.ID, CanonicalName: "Canonicalneedle", NormalizedName: "canonicalneedle", EntityType: "company"}).Error)
	require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: "alpha", EntityID: "entity"}).Error)
	require.NoError(t, h.db.Create(&types.Topic{LibraryID: h.library.ID, ID: "topic", SpaceID: 1, Name: "Topicneedle"}).Error)
	require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: "alpha", TopicID: "topic"}).Error)
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id='alpha'").Update("starred", true).Error)
	no := false
	cases := []struct {
		Name            string
		Keyword, Vector bool
		Request         types.EntrySearchRequest
	}{
		{"keyword-body", true, false, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: &no}},
		{"keyword-header", true, false, types.EntrySearchRequest{Query: "Canonicalneedle"}},
		{"vector", false, true, types.EntrySearchRequest{Query: "semanticlaunch", CollapseSimilar: &no}},
		{"hybrid", true, true, types.EntrySearchRequest{Query: "semanticlaunch", CollapseSimilar: &no}},
		{"collapse", true, true, types.EntrySearchRequest{Query: "semanticlaunch"}},
		{"mine", true, true, types.EntrySearchRequest{Query: "semanticlaunch", Sources: []string{"mine"}}},
		{"report", true, true, types.EntrySearchRequest{Query: "othersemantic", Sources: []string{"report"}}},
		{"content-type", true, true, types.EntrySearchRequest{Query: "searchfixture", ContentTypes: []string{"tutorial"}}},
		{"entity-filter", true, true, types.EntrySearchRequest{Query: "semanticlaunch", EntityIDs: []string{"entity"}}},
		{"topic-filter", true, true, types.EntrySearchRequest{Query: "semanticlaunch", TopicIDs: []string{"topic"}}},
		{"star-filter", true, true, types.EntrySearchRequest{Query: "semanticlaunch", Starred: func() *bool { v := true; return &v }()}},
		{"date-filter", true, true, types.EntrySearchRequest{Query: "othersemantic", PublishedFrom: "2026-10-01", PublishedTo: "2026-10-03"}},
	}
	evidence := map[string]any{}
	for _, c := range cases {
		h.library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: c.Keyword, VectorEnabled: c.Vector}
		require.NoError(t, h.db.Model(h.library).Update("indexing_strategy", h.library.IndexingStrategy).Error)
		evidence[c.Name] = h.search(t, c.Request)
	}

	bases := &libraryService{db: h.db, repo: repository.NewLibraryRepository(h.db), entryRepo: repository.NewEntryRepository(h.db), chunkRepo: repository.NewChunkRepository(h.db), modelService: personalRetrievalModels{}, retrieveEngine: retriever.NewPostgresRetrievalService(postgres.NewPostgresRetrieveEngineRepository(h.db))}
	for _, mode := range []string{"keyword", "vector", "hybrid"} {
		results, err := bases.HybridSearch(h.ctx, h.library.ID, types.SearchParams{QueryText: "passage", QueryEmbedding: []float32{1, 0, 0}, KeywordThreshold: 0.00001, EntryIDs: []string{"beta"}, MatchCount: 20, DisableVectorMatch: mode == "keyword", DisableKeywordsMatch: mode == "vector", SkipContextEnrichment: true})
		require.NoError(t, err)
		require.NotEmpty(t, results)
		evidence["chunk-"+mode] = results
	}
	summaryHits, summaryErr := bases.HybridSearch(h.ctx, h.library.ID, types.SearchParams{QueryText: "searchfixture", EntryIDs: []string{"summary"}, DisableVectorMatch: true, KeywordThreshold: 0.00001, MatchCount: 20, SkipContextEnrichment: true})
	require.NoError(t, summaryErr)
	require.Len(t, summaryHits, 1)
	evidence["chunk-summary"] = summaryHits
	data, err := json.MarshalIndent(evidence, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("NAMING_SEARCH_EVIDENCE"); path != "" {
		require.NoError(t, os.WriteFile(path, append(data, '\n'), 0600))
	}
}
