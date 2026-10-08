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
	domainID  string
	chunks    interfaces.ChunkRepository
	knowledge interfaces.KnowledgeRepository
	kbs       interfaces.KnowledgeBaseRepository
	indices   interfaces.RetrieveEngineRepository
	retrieval interfaces.RetrievalService
	service   *chunkService
}

func newChunkSearchFixture(t *testing.T, vectorEnabled bool) *chunkSearchFixture {
	t.Helper()
	db := pgtest.Open(t)
	tenant := &types.Tenant{ID: types.SingleTenantID}
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	f := &chunkSearchFixture{
		db: db, ctx: ctx, domainID: uuid.NewString(),
		chunks: repository.NewChunkRepository(db), knowledge: repository.NewKnowledgeRepository(db),
		kbs:      repository.NewKnowledgeBaseRepository(db),
		indices:  pgretriever.NewPostgresRetrieveEngineRepository(db),

	}
	require.NoError(t, db.Exec("INSERT INTO knowledge_domains (id, tenant_id, name) VALUES (?, ?, ?)",
		f.domainID, types.SingleTenantID, "Chunk search domain").Error)
	f.retrieval = retriever.NewPostgresRetrievalService(f.indices)
	f.service = &chunkService{
		chunkRepository: f.chunks, knowledgeRepo: f.knowledge, kbRepository: f.kbs,
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
		(id, tenant_id, domain_id, name, embedding_model_id, summary_model_id, indexing_strategy)
		VALUES (?, ?, ?, ?, ?, '', ?::jsonb)`, id, types.SingleTenantID, f.domainID,
		"Chunk search knowledge base", "chunk-search-fixed", string(strategy)).Error)
	kb, err := f.kbs.GetKnowledgeBaseByID(f.ctx, id)
	require.NoError(t, err)
	return kb
}

func (f *chunkSearchFixture) createKnowledge(t *testing.T, kb *types.KnowledgeBase, title string) *types.Knowledge {
	t.Helper()
	id := uuid.NewString()
	require.NoError(t, f.db.Exec(`INSERT INTO knowledges
		(id, tenant_id, knowledge_base_id, type, title, source, parse_status, embedding_model_id)
		VALUES (?, ?, ?, 'manual', ?, '', ?, ?)`, id, types.SingleTenantID, kb.ID,
		title, types.ParseStatusCompleted, "chunk-search-fixed").Error)
	knowledge, err := f.knowledge.GetKnowledgeByID(f.ctx, types.SingleTenantID, id)
	require.NoError(t, err)
	return knowledge
}

func (f *chunkSearchFixture) createChunk(t *testing.T, knowledge *types.Knowledge, body, header, chunkType string) *types.Chunk {
	t.Helper()
	chunk := &types.Chunk{
		ID: uuid.NewString(), TenantID: types.SingleTenantID,
		KnowledgeID: knowledge.ID, KnowledgeBaseID: knowledge.KnowledgeBaseID,
		Content: body, SourceContent: body, ContextHeader: header, ChunkType: chunkType,
		IsEnabled: true, IndexStatus: "ready", Status: int(types.ChunkStatusIndexed),
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
	knowledge := f.createKnowledge(t, kb, "Oriontitlemarker 量子计算消息")
	chunk := f.createChunk(t, knowledge, "Nebulabodymarker 正文介绍向量检索。", "## Pulsarsectionmarker / 章节路径", types.ChunkTypeText)
	chunk.Metadata = types.JSON(`{"generated_questions":[{"id":"opaqueidentitymarker","question":"Quasarquestionmarker 生成问题如何工作？"}],"private_notes":"privatenotemarker"}`)
	require.NoError(t, f.chunks.UpdateChunk(f.ctx, chunk))
	for _, query := range []string{
		"Oriontitlemarker", "量子计算", "Pulsarsectionmarker", "章节", "Quasarquestionmarker", "生成问题", "Nebulabodymarker", "向量检索",
	} {
		f.assertKeywordHits(t, kb.ID, query, chunk.ID)
	}
	f.assertKeywordHits(t, kb.ID, "opaqueidentitymarker")
	f.assertKeywordHits(t, kb.ID, "privatenotemarker")
	index := &types.IndexInfo{
		Content: chunk.Content, SourceID: chunk.ID, ChunkID: chunk.ID,
		KnowledgeID: knowledge.ID, KnowledgeBaseID: kb.ID, IsEnabled: true,
	}
	require.NoError(t, f.indices.Save(f.ctx, index, nil))
	require.NoError(t, f.indices.BatchSave(f.ctx, []*types.IndexInfo{index}, nil))
	var embeddingCount int64
	require.NoError(t, f.db.Table("embeddings").Count(&embeddingCount).Error)
	require.Zero(t, embeddingCount)

	knowledge.Title = "Vegalatermarker 标题更新"
	require.NoError(t, f.knowledge.UpdateKnowledge(f.ctx, knowledge))
	f.assertKeywordHits(t, kb.ID, "Oriontitlemarker")
	f.assertKeywordHits(t, kb.ID, "Vegalatermarker", chunk.ID)
}

func TestChunkKeywordSearchUpdatesThroughChunkService(t *testing.T) {
	f := newChunkSearchFixture(t, false)
	kb := f.createKB(t, false, true)
	knowledge := f.createKnowledge(t, kb, "Editable document")
	chunk := f.createChunk(t, knowledge, "Beforebodymarker immutable source", "## Chaptermarker", types.ChunkTypeText)
	f.assertKeywordHits(t, kb.ID, "Beforebodymarker", chunk.ID)

	question, err := f.service.UpsertGeneratedQuestion(f.ctx, chunk.ID, "", "Beforequestionmarker 为什么需要生成问题？")
	require.NoError(t, err)
	f.assertKeywordHits(t, kb.ID, "Beforequestionmarker", chunk.ID)
	_, err = f.service.UpsertGeneratedQuestion(f.ctx, chunk.ID, question.ID, "Afterquestionmarker 如何更新问题？")
	require.NoError(t, err)
	f.assertKeywordHits(t, kb.ID, "Beforequestionmarker")
	f.assertKeywordHits(t, kb.ID, "Afterquestionmarker", chunk.ID)

	newBody := "Afterbodymarker 当前正文已编辑"
	updated, err := f.service.UpdateDocumentChunk(f.ctx, chunk.ID, &newBody, nil, nil)
	require.NoError(t, err)
	require.Equal(t, "ready", updated.IndexStatus)
	require.Equal(t, chunk.SourceContent, updated.SourceContent)
	require.Equal(t, 1, updated.ContentRevision)
	f.assertKeywordHits(t, kb.ID, "Beforebodymarker")
	f.assertKeywordHits(t, kb.ID, "Afterbodymarker", chunk.ID)
	f.assertKeywordHits(t, kb.ID, "Chaptermarker", chunk.ID)
	f.assertKeywordHits(t, kb.ID, "Afterquestionmarker", chunk.ID)
	require.NoError(t, f.service.DeleteGeneratedQuestion(f.ctx, chunk.ID, question.ID))
	f.assertKeywordHits(t, kb.ID, "Afterquestionmarker")
	revisions, err := f.chunks.ListChunkRevisions(f.ctx, types.SingleTenantID, chunk.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 1)
	require.Equal(t, chunk.Content, revisions[0].Content)
	var count int64
	require.NoError(t, f.db.Table("embeddings").Count(&count).Error)
	require.Zero(t, count, "keyword indexing does not need vector placeholder rows")
}

func TestChunkKeywordSearchFiltersAndTypes(t *testing.T) {
	f := newChunkSearchFixture(t, false)
	kb := f.createKB(t, false, true)
	knowledge := f.createKnowledge(t, kb, "Filter document")
	var expected []string
	for _, chunkType := range []string{
		types.ChunkTypeText, types.ChunkTypeSummary, types.ChunkTypeImageOCR,
		types.ChunkTypeImageCaption, types.ChunkTypeTableSummary, types.ChunkTypeTableColumn,
	} {
		chunk := f.createChunk(t, knowledge, "Filterbodymarker searchable content", "", chunkType)
		expected = append(expected, chunk.ID)
	}
	for _, chunkType := range []string{
		types.ChunkTypeParentText, types.ChunkTypeEntity, types.ChunkTypeRelationship,
		types.ChunkTypeWebSearch, types.ChunkTypeWikiPage,
	} {
		f.createChunk(t, knowledge, "Filterbodymarker excluded content", "", chunkType)
	}
	for _, status := range []string{"processing", "failed"} {
		chunk := f.createChunk(t, knowledge, "Filterbodymarker stale content", "", types.ChunkTypeText)
		chunk.IndexStatus = status
		require.NoError(t, f.chunks.UpdateChunk(f.ctx, chunk))
	}
	disabled := f.createChunk(t, knowledge, "Filterbodymarker disabled content", "", types.ChunkTypeText)
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", disabled.ID).Update("is_enabled", false).Error)
	deleted := f.createChunk(t, knowledge, "Filterbodymarker deleted content", "", types.ChunkTypeText)
	require.NoError(t, f.db.Delete(deleted).Error)
	otherKB := f.createKB(t, false, true)
	otherKnowledge := f.createKnowledge(t, otherKB, "Other document")
	f.createChunk(t, otherKnowledge, "Filterbodymarker other knowledge base", "", types.ChunkTypeText)
	noKeywordKB := f.createKB(t, true, false)
	noKeywordKnowledge := f.createKnowledge(t, noKeywordKB, "Vector only document")
	f.createChunk(t, noKeywordKnowledge, "Filterbodymarker keywords disabled", "", types.ChunkTypeText)
	f.assertKeywordHits(t, kb.ID, "Filterbodymarker", expected...)
	f.assertKeywordHits(t, noKeywordKB.ID, "Filterbodymarker")

	tagID := uuid.NewString()
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("id = ?", expected[0]).Update("tag_id", tagID).Error)
	hits := f.retrieve(t, types.RetrieveParams{
		Query: "Filterbodymarker", KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{knowledge.ID}, TagIDs: []string{tagID},
	})
	require.Len(t, hits, 1)
	require.Equal(t, expected[0], hits[0].ChunkID)
	hits = f.retrieve(t, types.RetrieveParams{
		Query: "Filterbodymarker", KnowledgeBaseIDs: []string{kb.ID}, KnowledgeIDs: []string{otherKnowledge.ID},
	})
	require.Empty(t, hits, "knowledge and knowledge base restrictions combine with AND")
	f.assertKeywordHits(t, otherKB.ID, "Filterbodymarker", hitsForKnowledge(t, f, otherKnowledge.ID)...)
}

func hitsForKnowledge(t *testing.T, f *chunkSearchFixture, knowledgeID string) []string {
	t.Helper()
	chunks, err := f.chunks.ListChunksByKnowledgeIDAndTypes(f.ctx, types.SingleTenantID, knowledgeID,
		[]string{types.ChunkTypeText, types.ChunkTypeSummary, types.ChunkTypeImageOCR,
			types.ChunkTypeImageCaption, types.ChunkTypeTableSummary, types.ChunkTypeTableColumn})
	require.NoError(t, err)
	var ids []string
	for _, chunk := range chunks {
		if chunk.IsEnabled {
			ids = append(ids, chunk.ID)
		}
	}
	return ids
}

func TestChunkVectorSearchRestoresGeneratedQuestionMatchedContent(t *testing.T) {
	f := newChunkSearchFixture(t, true)
	kb := f.createKB(t, true, true)
	knowledge := f.createKnowledge(t, kb, "Vector message title")
	chunk := f.createChunk(t, knowledge, "Current canonical body", "## Section context", types.ChunkTypeText)
	questionID := uuid.NewString()
	questionText := "Questionvectormarker 为什么使用多向量？"
	require.NoError(t, chunk.SetDocumentMetadata(&types.DocumentChunkMetadata{
		GeneratedQuestions: []types.GeneratedQuestion{{ID: questionID, Question: questionText}},
	}))
	require.NoError(t, f.chunks.UpdateChunk(f.ctx, chunk))
	questionSourceID := types.GeneratedQuestionSourceID(chunk.ID, questionID)
	indices := []*types.IndexInfo{
		{Content: "Obsolete duplicated body", SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: knowledge.ID, KnowledgeBaseID: kb.ID, IsEnabled: true},
		{Content: "Obsolete duplicated question", SourceID: questionSourceID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID, KnowledgeID: knowledge.ID, KnowledgeBaseID: kb.ID, IsEnabled: true},
	}
	require.NoError(t, f.indices.BatchSave(f.ctx, indices, map[string]any{"embedding": map[string][]float32{
		chunk.ID: {0, 1, 0}, questionSourceID: {1, 0, 0},
	}}))
	hits := f.retrieve(t, types.RetrieveParams{
		RetrieverType: types.VectorRetrieverType, KnowledgeBaseIDs: []string{kb.ID},
		Embedding: []float32{1, 0, 0}, TopK: 1, Threshold: 0.99,
	})
	require.Len(t, hits, 1)
	require.Equal(t, questionSourceID, hits[0].SourceID)
	require.Equal(t, chunk.ID, hits[0].ChunkID)
	require.Equal(t, buildKnowledgeIndexContent(knowledge, questionText), hits[0].Content)
	searchService := &knowledgeBaseService{}
	index := searchService.buildChunkIndex(hits)
	result := searchService.buildSearchResult(chunk, knowledge, hits[0].Score, hits[0].MatchType, index.matchedContents[chunk.ID])
	require.Equal(t, chunk.Content, result.Content)
	require.Equal(t, buildKnowledgeIndexContent(knowledge, questionText), result.MatchedContent)

	hits = f.retrieve(t, types.RetrieveParams{
		RetrieverType: types.VectorRetrieverType, KnowledgeBaseIDs: []string{kb.ID},
		Embedding: []float32{0, 1, 0}, TopK: 1, Threshold: 0.99,
	})
	require.Len(t, hits, 1)
	require.Equal(t, buildKnowledgeIndexContent(knowledge, chunk.EmbeddingContent()), hits[0].Content)
}

func TestChunkKeywordSearchCloneAndMoveKnowledge(t *testing.T) {
	f := newChunkSearchFixture(t, true)
	sourceKB := f.createKB(t, true, true)
	targetKB := f.createKB(t, true, true)
	movedKB := f.createKB(t, true, true)
	source := f.createKnowledge(t, sourceKB, "Source title sourcetitlemarker")
	target := f.createKnowledge(t, targetKB, "Target title targettitlemarker")
	var sourceChunks []*types.Chunk
	for _, chunkType := range []string{
		types.ChunkTypeText, types.ChunkTypeSummary, types.ChunkTypeImageOCR,
		types.ChunkTypeImageCaption, types.ChunkTypeTableSummary, types.ChunkTypeTableColumn,
	} {
		sourceChunks = append(sourceChunks, f.createChunk(t, source, "Copybodymarker original body", "## Copysectionmarker", chunkType))
	}
	questionID := uuid.NewString()
	require.NoError(t, sourceChunks[0].SetDocumentMetadata(&types.DocumentChunkMetadata{
		GeneratedQuestions: []types.GeneratedQuestion{{ID: questionID, Question: "Copyquestionmarker generated question"}},
	}))
	require.NoError(t, f.chunks.UpdateChunk(f.ctx, sourceChunks[0]))
	for _, chunk := range sourceChunks {
		require.NoError(t, f.service.syncChunkIndex(f.ctx, chunk))
	}
	disabled := f.createChunk(t, source, "Copydisabledmarker unavailable body", "", types.ChunkTypeText)
	disabled.IsEnabled = false
	require.NoError(t, f.chunks.UpdateChunk(f.ctx, disabled))
	require.NoError(t, f.indices.Save(f.ctx, &types.IndexInfo{
		Content: disabled.Content, SourceID: disabled.ID, ChunkID: disabled.ID,
		KnowledgeID: source.ID, KnowledgeBaseID: sourceKB.ID,
	}, map[string]any{
		"embedding":     map[string][]float32{disabled.ID: {1, 0, 0}},
		"chunk_enabled": map[string]bool{disabled.ID: false},
	}))
	service := &knowledgeService{
		db: f.db, repo: f.knowledge, chunkRepo: f.chunks,
		kbService: chunkSearchKBReader{repo: f.kbs}, modelService: chunkSearchModels{}, retrieveEngine: f.retrieval,
	}
	require.NoError(t, service.CloneChunk(f.ctx, source, target))
	targetChunks, err := f.chunks.ListChunksByKnowledgeIDAndTypes(f.ctx, types.SingleTenantID, target.ID,
		[]string{types.ChunkTypeText, types.ChunkTypeSummary, types.ChunkTypeImageOCR,
			types.ChunkTypeImageCaption, types.ChunkTypeTableSummary, types.ChunkTypeTableColumn})
	require.NoError(t, err)
	require.Len(t, targetChunks, len(sourceChunks)+1)
	targetIDs := hitsForKnowledge(t, f, target.ID)
	f.assertKeywordHits(t, targetKB.ID, "Copybodymarker", targetIDs...)
	f.assertKeywordHits(t, targetKB.ID, "Copysectionmarker", targetIDs...)
	f.assertKeywordHits(t, targetKB.ID, "targettitlemarker", targetIDs...)
	f.assertKeywordHits(t, targetKB.ID, "sourcetitlemarker")
	var targetQuestionChunk *types.Chunk
	var targetDisabledChunk *types.Chunk
	for _, chunk := range targetChunks {
		metadata, err := chunk.DocumentMetadata()
		require.NoError(t, err)
		if metadata != nil && len(metadata.GeneratedQuestions) > 0 {
			targetQuestionChunk = chunk
		}
		if !chunk.IsEnabled {
			targetDisabledChunk = chunk
		}
	}
	require.NotNil(t, targetQuestionChunk)
	f.assertKeywordHits(t, targetKB.ID, "Copyquestionmarker", targetQuestionChunk.ID)
	var copiedQuestionCount int64
	require.NoError(t, f.db.Table("embeddings").Where("knowledge_base_id = ? AND source_id = ?", targetKB.ID,
		types.GeneratedQuestionSourceID(targetQuestionChunk.ID, questionID)).Count(&copiedQuestionCount).Error)
	require.Equal(t, int64(1), copiedQuestionCount)
	require.NotNil(t, targetDisabledChunk)
	var disabledVectorCount int64
	require.NoError(t, f.db.Table("embeddings").Where("knowledge_base_id = ? AND source_id = ? AND is_enabled = false",
		targetKB.ID, targetDisabledChunk.ID).Count(&disabledVectorCount).Error)
	require.Equal(t, int64(1), disabledVectorCount)
	f.assertKeywordHits(t, targetKB.ID, "Copydisabledmarker")

	require.NoError(t, service.moveOneKnowledge(f.ctx, source.ID, sourceKB, movedKB, "reuse_vectors"))
	f.assertKeywordHits(t, sourceKB.ID, "Copybodymarker")
	sourceIDs := make([]string, len(sourceChunks))
	for i, chunk := range sourceChunks {
		sourceIDs[i] = chunk.ID
	}
	f.assertKeywordHits(t, movedKB.ID, "Copybodymarker", sourceIDs...)
	f.assertKeywordHits(t, movedKB.ID, "Copysectionmarker", sourceIDs...)
	f.assertKeywordHits(t, movedKB.ID, "sourcetitlemarker", sourceIDs...)
	f.assertKeywordHits(t, movedKB.ID, "Copyquestionmarker", sourceChunks[0].ID)
	var movedVectorCount int64
	require.NoError(t, f.db.Table("embeddings").Where("knowledge_base_id = ? AND knowledge_id = ?", movedKB.ID, source.ID).Count(&movedVectorCount).Error)
	require.Equal(t, int64(len(sourceChunks)+2), movedVectorCount)
	require.NoError(t, f.db.Table("embeddings").Where("knowledge_base_id = ?", sourceKB.ID).Count(&movedVectorCount).Error)
	require.Zero(t, movedVectorCount)
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
	knowledge := f.createKnowledge(t, kb, "HNSW candidate document")
	chunk := f.createChunk(t, knowledge, "Body hydrated after vector candidates", "", types.ChunkTypeText)
	vector := make([]float32, 1024)
	vector[0] = 1
	require.NoError(t, f.indices.Save(f.ctx, &types.IndexInfo{
		Content: chunk.Content, SourceID: chunk.ID, ChunkID: chunk.ID,
		KnowledgeID: knowledge.ID, KnowledgeBaseID: kb.ID, IsEnabled: true,
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
