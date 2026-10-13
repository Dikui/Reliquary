package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

type personalRetrievalModels struct{ chunkSearchModels }

func (personalRetrievalModels) GetModelByID(context.Context, string) (*types.Model, error) {
	return &types.Model{Name: "chunk-search-fixed"}, nil
}

type personalRetrievalHit struct {
	Content   string          `json:"content"`
	Score     float64         `json:"score"`
	MatchType types.MatchType `json:"match_type"`
}

func TestPersonalPostgresRetrievalResults(t *testing.T) {
	f := newChunkSearchFixture(t, true)
	require.NoError(t, f.db.Exec("INSERT INTO spaces (id, name, business) VALUES (?, ?, 'personal')", types.SingleSpaceID, "Retrieval fixture").Error)
	first := f.createLibrary(t, true, true)
	second := first
	definitions := []struct {
		library *types.Library
		body    string
		vector  []float32
	}{
		{first, "retrieval retrieval retrieval retrieval retrieval postgres postgres postgres postgres", []float32{1, 0, 0}},
		{second, "retrieval retrieval retrieval postgres postgres context budgets keyword vector", []float32{0.9, 0.2, 0}},
		{first, "retrieval postgres storage parsing indexing document content context scopes", []float32{0.7, 0.5, 0}},
		{second, "personal entry local files queue background processing task worker", []float32{0.1, 1, 0}},
	}
	for _, definition := range definitions {
		entry := f.createEntry(t, definition.library, "Sample document")
		chunk := f.createChunk(t, entry, definition.body, "", types.ChunkTypeText)
		require.NoError(t, f.indices.Save(f.ctx, &types.IndexInfo{
			Content: chunk.Content, SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID,
			EntryID: entry.ID, LibraryID: definition.library.ID, IsEnabled: true,
		}, map[string]any{"embedding": map[string][]float32{chunk.ID: definition.vector}}))
	}
	service := &libraryService{db: f.db, repo: f.libraries, entryRepo: f.entry, chunkRepo: f.chunks,
		modelService: personalRetrievalModels{}, retrieveEngine: f.retrieval}
	recorded := make(map[string][]personalRetrievalHit)
	for _, query := range []string{"retrieval", "postgres"} {
		for _, mode := range []string{"vector", "keyword", "hybrid"} {
			results, err := service.HybridSearch(f.ctx, first.ID, types.SearchParams{
				QueryText: query, QueryEmbedding: []float32{1, 0, 0},
				MatchCount: 10, DisableVectorMatch: mode == "keyword", DisableKeywordsMatch: mode == "vector",
				SkipContextEnrichment: true,
			})
			require.NoError(t, err)
			require.NotEmpty(t, results)
			key := query + "/" + mode
			for _, result := range results {
				recorded[key] = append(recorded[key], personalRetrievalHit{result.Content, result.Score, result.MatchType})
			}
		}
	}
	expectedScores := map[string][]float64{
		"retrieval/vector":  {1, 0.9761932912662488, 0.8138101547408724, 0.09947966729125401},
		"postgres/vector":   {1, 0.9761932912662488, 0.8138101547408724, 0.09947966729125401},
		"retrieval/keyword": {2.214513063430786, 1.9975495338439941, 1.3861066102981567},
		"postgres/keyword":  {2.1268932819366455, 1.7873659133911133, 1.3861066102981567},
		"retrieval/hybrid":  {0.016393442622950817, 0.016129032258064516, 0.015873015873015872, 0.0109375},
		"postgres/hybrid":   {0.016393442622950817, 0.016129032258064516, 0.015873015873015872, 0.0109375},
	}
	for key, scores := range expectedScores {
		require.Len(t, recorded[key], len(scores), key)
		for i, score := range scores {
			require.Equal(t, definitions[i].body, recorded[key][i].Content, key)
			require.Equal(t, score, recorded[key][i].Score, key)
			wantMatch := types.MatchTypeEmbedding
			if key == "retrieval/keyword" || key == "postgres/keyword" {
				wantMatch = types.MatchTypeKeywords
			}
			require.Equal(t, wantMatch, recorded[key][i].MatchType, key)
		}
	}
	encoded, err := json.MarshalIndent(recorded, "", "  ")
	require.NoError(t, err)
	if path := os.Getenv("RELIQUARY_RETRIEVAL_RECORD"); path != "" {
		require.NoError(t, os.WriteFile(path, append(encoded, '\n'), 0600))
	}
	if path := os.Getenv("RELIQUARY_RETRIEVAL_COMPARE"); path != "" {
		baseline, err := os.ReadFile(path)
		require.NoError(t, err)
		require.JSONEq(t, string(baseline), string(encoded), "PostgreSQL result order, exact scores and match types must match the recorded baseline")
	}
}

func TestPersonalLibraryPostgresSchemaAndEmbedding(t *testing.T) {
	f := newChunkSearchFixture(t, true)
	require.NoError(t, f.db.Exec("INSERT INTO spaces (id, name, business) VALUES (?, ?, 'personal')", types.SingleSpaceID, "Personal fixture").Error)
	service := &libraryService{db: f.db, repo: f.libraries}
	created := &types.Library{ID: "personal-library", SpaceID: types.SingleSpaceID, Name: "Personal library", EmbeddingModelID: "chunk-search-fixed"}
	created.EnsureDefaults()
	require.NoError(t, f.libraries.CreateLibrary(f.ctx, created))
	loaded, err := service.GetLibraryByID(f.ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "chunk-search-fixed", loaded.EmbeddingModelID)
	for _, library := range []*types.Library{created, loaded} {
		encoded, err := json.Marshal(library)
		require.NoError(t, err)
		var fields map[string]any
		require.NoError(t, json.Unmarshal(encoded, &fields))
		require.NotContains(t, fields, "vector_store_id")
	}
	require.False(t, f.db.Migrator().HasTable("vector_stores"))
	require.False(t, f.db.Migrator().HasColumn("libraries", "vector_store_id"))
	require.False(t, f.db.Migrator().HasColumn("spaces", "retriever_engines"))
}
