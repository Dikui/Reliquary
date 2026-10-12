package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"

	"reliquary/internal/application/repository"
	pgretriever "reliquary/internal/application/repository/retriever/postgres"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type chunkSearchEmbedder struct{}

func (chunkSearchEmbedder) GetDimensions() int   { return 3 }
func (chunkSearchEmbedder) GetModelID() string   { return "chunk-search-fixed" }
func (chunkSearchEmbedder) GetModelName() string { return "chunk-search-fixed" }
func (chunkSearchEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (e chunkSearchEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		vectors[i] = vector
	}
	return vectors, nil
}
func (e chunkSearchEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}

type chunkSearchModels struct{ interfaces.ModelService }

func (chunkSearchModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return chunkSearchEmbedder{}, nil
}

type chunkSearchKBReader struct {
	interfaces.KnowledgeBaseService
	repo interfaces.KnowledgeBaseRepository
}

func (r chunkSearchKBReader) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	return r.repo.GetKnowledgeBaseByID(ctx, id)
}

type chunkSearchFixture struct {
	db        *gorm.DB
	ctx       context.Context
	chunks    interfaces.ChunkRepository
	entry     interfaces.EntryRepository
	kbs       interfaces.KnowledgeBaseRepository
	indices   interfaces.RetrieveEngineRepository
	retrieval interfaces.RetrievalService
	service   *chunkService
}

func newChunkSearchFixture(t *testing.T, vectorEnabled bool) *chunkSearchFixture {
	t.Helper()
	db := pgtest.Open(t)
	space := &types.Space{ID: types.SingleSpaceID}
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	f := &chunkSearchFixture{
		db: db, ctx: ctx,
		chunks: repository.NewChunkRepository(db), entry: repository.NewEntryRepository(db),
		kbs:     repository.NewKnowledgeBaseRepository(db),
		indices: pgretriever.NewPostgresRetrieveEngineRepository(db),
	}
	f.retrieval = retriever.NewPostgresRetrievalService(f.indices)
	f.service = &chunkService{
		chunkRepository: f.chunks, entryRepo: f.entry, kbRepository: f.kbs,
		modelService: chunkSearchModels{}, retrieveEngine: f.retrieval,
	}
	return f
}

func (f *chunkSearchFixture) createKB(t *testing.T, vectorEnabled, keywordEnabled bool) *types.KnowledgeBase {
	t.Helper()
	id := uuid.NewString()
	strategy, err := json.Marshal(types.IndexingStrategy{VectorEnabled: vectorEnabled, KeywordEnabled: keywordEnabled})
	require.NoError(t, err)
	require.NoError(t, f.db.Exec(`INSERT INTO knowledge_bases
		(id, space_id, name, embedding_model_id, summary_model_id, indexing_strategy)
		VALUES (?, ?, ?, ?, '', ?::jsonb)`, id, types.SingleSpaceID,
		"Chunk search knowledge base", "chunk-search-fixed", string(strategy)).Error)
	kb, err := f.kbs.GetKnowledgeBaseByID(f.ctx, id)
	require.NoError(t, err)
	return kb
}

func (f *chunkSearchFixture) createEntry(t *testing.T, kb *types.KnowledgeBase, title string) *types.Entry {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, f.db.Exec(`INSERT INTO entries
		(id, space_id, knowledge_base_id, type, title, source, parse_status, embedding_model_id)
		VALUES (?, ?, ?, 'manual', ?, '', ?, ?)`, id, types.SingleSpaceID, kb.ID,
		title, types.ParseStatusCompleted, "chunk-search-fixed").Error)
	entry, err := f.entry.GetEntryByID(f.ctx, types.SingleSpaceID, id)
	require.NoError(t, err)
	return entry
}

func (f *chunkSearchFixture) createChunk(t *testing.T, entry *types.Entry, body, header, chunkType string) *types.Chunk {
	t.Helper()
	chunk := &types.Chunk{
		ID: uuid.NewString(), SpaceID: types.SingleSpaceID,
		EntryID: entry.ID, KnowledgeBaseID: entry.KnowledgeBaseID,
		Content: body, ContextHeader: header, ChunkType: chunkType,
		IsEnabled: true, Status: int(types.ChunkStatusIndexed),
	}
	require.NoError(t, f.chunks.CreateChunks(f.ctx, []*types.Chunk{chunk}))
	return chunk
}

func (f *chunkSearchFixture) retrieve(t *testing.T, params types.RetrieveParams) []*types.IndexWithScore {
	t.Helper()
	if params.TopK == 0 {
		params.TopK = 50
	}
	if params.RetrieverType == "" {
		params.RetrieverType = types.KeywordsRetrieverType
	}
	groups, err := f.indices.Retrieve(f.ctx, params)
	require.NoError(t, err)
	var hits []*types.IndexWithScore
	for _, group := range groups {
		require.NoError(t, group.Error)
		hits = append(hits, group.Results...)
	}
	return hits
}

func (f *chunkSearchFixture) assertKeywordHits(t *testing.T, kbID, query string, chunkIDs ...string) {
	t.Helper()
	hits := f.retrieve(t, types.RetrieveParams{Query: query, KnowledgeBaseIDs: []string{kbID}})
	got := make([]string, len(hits))
	for i, hit := range hits {
		got[i] = hit.ChunkID
		require.Equal(t, types.MatchTypeKeywords, hit.MatchType)
	}
	require.ElementsMatch(t, chunkIDs, got, "query %q", query)
}

func TestChunkKeywordSearchPreservesFieldsWithoutEmbeddings(t *testing.T) {
	f := newChunkSearchFixture(t, false)
	kb := f.createKB(t, false, true)
	entry := f.createEntry(t, kb, "Oriontitlemarker 量子计算消息")
	chunk := f.createChunk(t, entry, "Nebulabodymarker 正文介绍向量检索。", "## Pulsarsectionmarker / 章节路径", types.ChunkTypeText)
	chunk.Metadata = types.JSON(`{"private_notes":"privatenotemarker"}`)
	require.NoError(t, f.chunks.UpdateChunk(f.ctx, chunk))
	for _, query := range []string{
		"Oriontitlemarker", "量子计算", "Pulsarsectionmarker", "章节", "Nebulabodymarker", "向量检索",
	} {
		f.assertKeywordHits(t, kb.ID, query, chunk.ID)
	}
	f.assertKeywordHits(t, kb.ID, "privatenotemarker")
	index := &types.IndexInfo{
		Content: chunk.Content, SourceID: chunk.ID, ChunkID: chunk.ID,
		EntryID: entry.ID, KnowledgeBaseID: kb.ID, IsEnabled: true,
	}
	require.NoError(t, f.indices.Save(f.ctx, index, nil))
	require.NoError(t, f.indices.BatchSave(f.ctx, []*types.IndexInfo{index}, nil))
	var embeddingCount int64
	require.NoError(t, f.db.Table("embeddings").Count(&embeddingCount).Error)
	require.Zero(t, embeddingCount)

	entry.Title = "Vegalatermarker 标题更新"
	require.NoError(t, f.entry.UpdateEntry(f.ctx, entry))
	f.assertKeywordHits(t, kb.ID, "Oriontitlemarker")
	f.assertKeywordHits(t, kb.ID, "Vegalatermarker", chunk.ID)
}

func TestChunkKeywordSearchUpdatesThroughChunkService(t *testing.T) {
	f := newChunkSearchFixture(t, false)
	kb := f.createKB(t, false, true)
	entry := f.createEntry(t, kb, "Editable document")
	chunk := f.createChunk(t, entry, "Beforebodymarker immutable source", "## Chaptermarker", types.ChunkTypeText)
	f.assertKeywordHits(t, kb.ID, "Beforebodymarker", chunk.ID)

	chunk.Content = "Afterbodymarker stored source"
	require.NoError(t, f.service.UpdateChunk(f.ctx, chunk))
	f.assertKeywordHits(t, kb.ID, "Beforebodymarker")
	f.assertKeywordHits(t, kb.ID, "Afterbodymarker", chunk.ID)
	f.assertKeywordHits(t, kb.ID, "Chaptermarker", chunk.ID)
	var count int64
	require.NoError(t, f.db.Table("embeddings").Count(&count).Error)
	require.Zero(t, count, "keyword indexing does not need vector placeholder rows")
}

func TestChunkKeywordSearchFiltersAndTypes(t *testing.T) {
	f := newChunkSearchFixture(t, false)
	kb := f.createKB(t, false, true)
	entry := f.createEntry(t, kb, "Filter document")
	var expected []string
	for _, chunkType := range []string{
		types.ChunkTypeText, types.ChunkTypeSummary,
		types.ChunkTypeTableSummary, types.ChunkTypeTableColumn,
	} {
		chunk := f.createChunk(t, entry, "Filterbodymarker searchable content", "", chunkType)
		expected = append(expected, chunk.ID)
	}
	for _, chunkType := range []string{
		types.ChunkTypeParentText, types.ChunkTypeEntity, types.ChunkTypeRelationship,
	} {
		f.createChunk(t, entry, "Filterbodymarker excluded content", "", chunkType)
	}
	disabled := f.createChunk(t, entry, "Filterbodymarker disabled content", "", types.ChunkTypeText)
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", disabled.ID).Update("is_enabled", false).Error)
	deleted := f.createChunk(t, entry, "Filterbodymarker deleted content", "", types.ChunkTypeText)
	require.NoError(t, f.db.Delete(deleted).Error)
	f.assertKeywordHits(t, kb.ID, "Filterbodymarker", expected...)
	hits := f.retrieve(t, types.RetrieveParams{Query: "Filterbodymarker", KnowledgeBaseIDs: []string{kb.ID}, EntryIDs: []string{"missing-article"}})
	require.Empty(t, hits, "article restrictions are applied within the knowledge base")
	hits = f.retrieve(t, types.RetrieveParams{Query: "Filterbodymarker", KnowledgeBaseIDs: []string{"missing-kb"}, EntryIDs: []string{entry.ID}})
	require.Empty(t, hits, "entry and knowledge base restrictions combine with AND")
	strategy, err := json.Marshal(types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: false})
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&types.KnowledgeBase{}).Where("id = ?", kb.ID).Update("indexing_strategy", string(strategy)).Error)
	f.assertKeywordHits(t, kb.ID, "Filterbodymarker")

}

func hitsForEntry(t *testing.T, f *chunkSearchFixture, entryID string) []string {
	t.Helper()
	chunks, err := f.chunks.ListChunksByEntryIDAndTypes(f.ctx, types.SingleSpaceID, entryID,
		[]string{types.ChunkTypeText, types.ChunkTypeSummary,
			types.ChunkTypeTableSummary, types.ChunkTypeTableColumn})
	require.NoError(t, err)
	var ids []string
	for _, chunk := range chunks {
		if chunk.IsEnabled {
			ids = append(ids, chunk.ID)
		}
	}
	return ids
}

type chunkSearchSQLTrace struct {
	gormlogger.Interface
	statements []string
}

func (l *chunkSearchSQLTrace) Trace(_ context.Context, _ time.Time, statement func() (string, int64), _ error) {
	sql, _ := statement()
	l.statements = append(l.statements, sql)
}

func TestChunkVectorSearchKeepsHNSWLimitBeforeHydration(t *testing.T) {
	f := newChunkSearchFixture(t, true)
	kb := f.createKB(t, true, true)
	entry := f.createEntry(t, kb, "HNSW candidate document")
	chunk := f.createChunk(t, entry, "Body hydrated after vector candidates", "", types.ChunkTypeText)
	vector := make([]float32, 1024)
	vector[0] = 1
	require.NoError(t, f.indices.Save(f.ctx, &types.IndexInfo{
		Content: chunk.Content, SourceID: chunk.ID, ChunkID: chunk.ID,
		EntryID: entry.ID, KnowledgeBaseID: kb.ID, IsEnabled: true,
	}, map[string]any{"embedding": map[string][]float32{chunk.ID: vector}}))
	trace := &chunkSearchSQLTrace{Interface: gormlogger.Default.LogMode(gormlogger.Silent)}
	f.indices = pgretriever.NewPostgresRetrieveEngineRepository(f.db.Session(&gorm.Session{Logger: trace}))
	hits := f.retrieve(t, types.RetrieveParams{
		RetrieverType: types.VectorRetrieverType, KnowledgeBaseIDs: []string{kb.ID},
		Embedding: vector, TopK: 1, Threshold: 0.99,
	})
	require.Len(t, hits, 1)
	var candidateSQL string
	for _, statement := range trace.statements {
		if strings.Contains(statement, "AS candidates") {
			candidateSQL = statement
		}
	}
	require.NotEmpty(t, candidateSQL)
	var explainJSON string
	require.NoError(t, f.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SET LOCAL enable_seqscan = off").Error; err != nil {
			return err
		}
		return tx.Raw("EXPLAIN (FORMAT JSON) " + candidateSQL).Scan(&explainJSON).Error
	}))
	var explain []struct {
		Plan map[string]any `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal([]byte(explainJSON), &explain))
	require.Len(t, explain, 1)
	require.True(t, chunkSearchPlanHasLimitedHNSW(explain[0].Plan),
		"HNSW index scan should sit directly below the candidate LIMIT before chunk hydration: %s", explainJSON)
}

func chunkSearchPlanHasLimitedHNSW(plan map[string]any) bool {
	children, _ := plan["Plans"].([]any)
	for _, item := range children {
		child, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if plan["Node Type"] == "Limit" && child["Index Name"] == "embeddings_embedding_idx_1024" {
			return true
		}
		if chunkSearchPlanHasLimitedHNSW(child) {
			return true
		}
	}
	return false
}
