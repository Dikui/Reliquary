package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/models/rerank"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type articleSearchFixtureBases struct {
	interfaces.KnowledgeBaseService
	kb             *types.KnowledgeBase
	embeddingError error
}

func (b articleSearchFixtureBases) GetKnowledgeBase(context.Context) (*types.KnowledgeBase, error) {
	return b.kb, nil
}
func (b articleSearchFixtureBases) GetQueryEmbedding(_ context.Context, _ string, query string) ([]float32, error) {
	if b.embeddingError != nil {
		return nil, b.embeddingError
	}
	return articleSearchFixtureEmbedding(query), nil
}

// The same text always has the same vector; markers control semantic distance.
func articleSearchFixtureEmbedding(text string) []float32 {
	text = strings.ToLower(text)
	switch {
	case strings.Contains(text, "semanticlaunch"):
		return []float32{1, 0, 0}
	case strings.Contains(text, "othersemantic"):
		return []float32{0, 1, 0}
	case text == "searchfixture":
		return []float32{1, 1, 1}
	default:
		return []float32{0, 0, 1}
	}
}

type articleSearchFixture struct {
	db     *gorm.DB
	ctx    context.Context
	kb     *types.KnowledgeBase
	config *types.RetrievalConfig
	svc    interfaces.KnowledgeSearchService
}

func newArticleSearchFixture(t *testing.T) *articleSearchFixture {
	t.Helper()
	db := pgtest.Open(t)
	config := &types.RetrievalConfig{RerankTopK: 100, KeywordThreshold: 0.00001}
	space := &types.Space{ID: 1, Name: "Search fixture", RetrievalConfig: config}
	require.NoError(t, db.Create(space).Error)
	kb := &types.KnowledgeBase{ID: "search-kb", SpaceID: 1, Name: "Search", Type: types.KnowledgeBaseTypeDocument,
		EmbeddingModelID: "fixture-model", ModelTiers: types.ModelTierConfig{Embedding: "fixture-model"},
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}}
	require.NoError(t, db.Create(kb).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	svc := NewKnowledgeSearchService(articleSearchFixtureBases{kb: kb}, nil, nil, db)
	return &articleSearchFixture{db: db, ctx: ctx, kb: kb, config: config, svc: svc}
}

func (h *articleSearchFixture) add(t *testing.T, id, source, date, contentType, semantic string, count int) *types.Knowledge {
	t.Helper()
	article := &types.Knowledge{ID: id, SpaceID: 1, KnowledgeBaseID: h.kb.ID, Type: "manual",
		Title: "Searchfixture " + id, Source: "https://example.org/" + id, SourceKind: source,
		ContentType: contentType, ProcessingMode: types.ProcessingModeForContentType(contentType),
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", EmbeddingModelID: "fixture-model",
		VaultPath: source + "/" + id + ".md"}
	if source == "article" {
		article.StructuredSummary = &types.KnowledgeSummary{Overview: semantic, Points: []string{"Evidence point"}, KeyTerms: []string{"Termneedle" + id}}
		article.Description = article.StructuredSummary.RenderText()
	} else {
		article.ProcessingMode = types.ProcessingModeFull
	}
	metadata, err := json.Marshal(map[string]string{"publisher": "Publisherneedle" + id, "author": "Authorneedle" + id, "published_at": date, "original_url": article.Source})
	require.NoError(t, err)
	article.Metadata = metadata
	require.NoError(t, h.db.Create(article).Error)
	if source == "article" {
		vector := pgvector.NewHalfVector(articleSearchFixtureEmbedding(article.Title + "\n" + article.Description))
		require.NoError(t, h.db.Exec(`INSERT INTO article_embeddings(knowledge_id,space_id,knowledge_base_id,model_id,dimension,embedding,content_hash)
			VALUES (?,1,?,'fixture-model',3,?,'fixture')`, article.ID, h.kb.ID, vector).Error)
	}
	for i := 0; i < count; i++ {
		chunk := &types.Chunk{ID: fmt.Sprintf("%s-chunk-%03d", id, i), SpaceID: 1, KnowledgeBaseID: h.kb.ID,
			KnowledgeID: id, ChunkIndex: i, Content: "searchfixture " + semantic + fmt.Sprintf(" body passage %d", i),
			ChunkType: types.ChunkTypeText, IsEnabled: true}
		require.NoError(t, h.db.Create(chunk).Error)
		require.NoError(t, h.db.Exec(`INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,dimension,embedding,is_enabled)
			VALUES (?,0,?,?,?,?,?,true)`, chunk.ID, chunk.ID, article.ID, h.kb.ID, 3, pgvector.NewHalfVector(articleSearchFixtureEmbedding(chunk.Content))).Error)
	}
	return article
}

func (h *articleSearchFixture) search(t *testing.T, request types.ArticleSearchRequest) *types.ArticleSearchResponse {
	t.Helper()
	response, err := h.svc.Search(h.ctx, request)
	require.NoError(t, err)
	return response
}

func articleSearchIDs(response *types.ArticleSearchResponse) []string {
	ids := []string{}
	for _, result := range response.Results {
		ids = append(ids, result.ArticleID)
	}
	slices.Sort(ids)
	return ids
}

func articleSearchNoCollapse() *bool { value := false; return &value }

func TestArticleSearchPostgresMergesChunksAndSummaryOnlyRoutes(t *testing.T) {
	h := newArticleSearchFixture(t)
	h.add(t, "full", "article", "2026-10-01", "event", "semanticlaunch", 80)
	h.add(t, "summary", "article", "2026-10-02", "tutorial", "semanticlaunch", 0)
	h.add(t, "another", "article", "2026-10-04", "event", "semanticlaunch", 1)
	response := h.search(t, types.ArticleSearchRequest{Query: "semanticlaunch", CollapseSimilar: articleSearchNoCollapse(), Limit: 3})
	require.Equal(t, []string{"another", "full", "summary"}, articleSearchIDs(response))
	for _, result := range response.Results {
		require.LessOrEqual(t, len(result.Snippets), 3)
		require.NotEmpty(t, result.Title)
		require.NotEmpty(t, result.VaultPath)
		require.Contains(t, result.OriginalURL, result.ArticleID)
		require.NotNil(t, result.Summary)
		if result.ArticleID == "summary" {
			require.Len(t, result.Snippets, 1)
			require.Empty(t, result.Snippets[0].ChunkID)
			require.Equal(t, result.Summary.RenderText(), result.Snippets[0].Content)
		}
	}
	// A semantic-only query has no shared keyword and recalls summary articles.
	h.kb.IndexingStrategy.KeywordEnabled = false
	response = h.search(t, types.ArticleSearchRequest{Query: "semanticlaunch", CollapseSimilar: articleSearchNoCollapse()})
	require.Contains(t, articleSearchIDs(response), "summary")
	// A body-only keyword cannot recall a summary article whose body was not kept.
	h.kb.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	require.NoError(t, h.db.Model(&types.Chunk{}).Where("knowledge_id='full'").Update("content", "privatebodyneedle").Error)
	response = h.search(t, types.ArticleSearchRequest{Query: "privatebodyneedle", CollapseSimilar: articleSearchNoCollapse()})
	require.Equal(t, []string{"full"}, articleSearchIDs(response))
}

func TestArticleSearchPostgresHeaderTermsAndAssociations(t *testing.T) {
	h := newArticleSearchFixture(t)
	h.add(t, "summary", "article", "2026-10-01", "reference", "semanticlaunch", 0)
	entity := &types.Entity{ID: "entity-a", SpaceID: 1, CanonicalName: "Canonicalneedle", NormalizedName: "canonicalneedle", EntityType: "company"}
	require.NoError(t, h.db.Create(entity).Error)
	require.NoError(t, h.db.Create(&types.KnowledgeEntity{SpaceID: 1, KnowledgeID: "summary", EntityID: entity.ID}).Error)
	require.NoError(t, h.db.Create(&types.EntityAlias{ID: "alias-a", SpaceID: 1, EntityID: entity.ID, Alias: "Aliasneedle", NormalizedAlias: "aliasneedle", EntityType: "company"}).Error)
	require.NoError(t, h.db.Create(&types.Topic{ID: "topic-a", SpaceID: 1, Name: "Topicneedle"}).Error)
	require.NoError(t, h.db.Create(&types.KnowledgeTopic{SpaceID: 1, KnowledgeID: "summary", TopicID: "topic-a"}).Error)
	h.kb.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	for _, term := range []string{"Canonicalneedle", "Aliasneedle", "Topicneedle", "Termneedlesummary", "Publisherneedlesummary", "Authorneedlesummary"} {
		t.Run(term, func(t *testing.T) {
			response := h.search(t, types.ArticleSearchRequest{Query: term})
			require.Equal(t, []string{"summary"}, articleSearchIDs(response))
			require.Equal(t, "keyword", response.Results[0].Snippets[0].MatchType)
			require.Equal(t, "entity-a", response.Results[0].Entities[0].ID)
			require.Equal(t, []string{"Aliasneedle"}, response.Results[0].Entities[0].Aliases)
			require.Equal(t, "Topicneedle", response.Results[0].Topics[0].Name)
		})
	}
	options, err := h.svc.Options(h.ctx)
	require.NoError(t, err)
	require.Len(t, options.Entities, 1)
	require.Equal(t, []string{"Aliasneedle"}, options.Entities[0].Aliases)
	require.Len(t, options.Topics, 1)
	// Header indexes track aliases and topic names when they change.
	require.NoError(t, h.db.Model(&types.EntityAlias{}).Where("id='alias-a'").Updates(map[string]interface{}{"alias": "Renamedaliasneedle", "normalized_alias": "renamedaliasneedle"}).Error)
	require.NoError(t, h.db.Model(&types.Topic{}).Where("id='topic-a'").Update("name", "Renamedtopicneedle").Error)
	for _, term := range []string{"Renamedaliasneedle", "Renamedtopicneedle"} {
		require.Equal(t, []string{"summary"}, articleSearchIDs(h.search(t, types.ArticleSearchRequest{Query: term})))
	}
}

func TestArticleSearchPostgresFiltersAndMissingPublicationCount(t *testing.T) {
	h := newArticleSearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "article", "2026-10-03T23:59:59Z", "tutorial", "semanticlaunch", 0)
	h.add(t, "c", "article", "2026-10-04T00:00:00Z", "opinion", "semanticlaunch", 1)
	h.add(t, "missing", "article", "", "event", "semanticlaunch", 1)
	h.add(t, "missing-unrelated", "article", "", "research", "othersemantic", 1)
	h.add(t, "mine", "mine", "", "", "semanticlaunch", 1)
	h.add(t, "report", "report", "", "", "semanticlaunch", 1)
	for _, id := range []string{"a", "b"} {
		require.NoError(t, h.db.Create(&types.Entity{ID: "entity-" + id, SpaceID: 1, CanonicalName: id, NormalizedName: id, EntityType: "company"}).Error)
		require.NoError(t, h.db.Create(&types.Topic{ID: "topic-" + id, SpaceID: 1, Name: id}).Error)
		require.NoError(t, h.db.Create(&types.KnowledgeEntity{SpaceID: 1, KnowledgeID: id, EntityID: "entity-" + id}).Error)
		require.NoError(t, h.db.Create(&types.KnowledgeTopic{SpaceID: 1, KnowledgeID: id, TopicID: "topic-" + id}).Error)
	}
	// Keep query recall entirely semantic so the unrelated missing article is excluded.
	h.kb.IndexingStrategy.KeywordEnabled = false
	tests := []struct {
		name    string
		req     types.ArticleSearchRequest
		ids     []string
		missing int64
	}{
		{"content", types.ArticleSearchRequest{ContentTypes: []string{"event", "tutorial"}}, []string{"a", "b", "missing"}, 0},
		{"entities", types.ArticleSearchRequest{EntityIDs: []string{"entity-a", "entity-b"}}, []string{"a", "b"}, 0},
		{"topics", types.ArticleSearchRequest{TopicIDs: []string{"topic-b"}}, []string{"b"}, 0},
		{"sources", types.ArticleSearchRequest{Sources: []string{"mine", "report"}}, []string{"mine", "report"}, 0},
		{"date", types.ArticleSearchRequest{PublishedFrom: "2026-10-01", PublishedTo: "2026-10-03"}, []string{"a", "b"}, 1},
		{"datetime", types.ArticleSearchRequest{PublishedFrom: "2026-10-03T23:59:59Z", PublishedTo: "2026-10-03T23:59:59Z"}, []string{"b"}, 1},
		{"combined", types.ArticleSearchRequest{ContentTypes: []string{"event", "tutorial"}, EntityIDs: []string{"entity-a", "entity-b"}, TopicIDs: []string{"topic-b"}, Sources: []string{"article"}, PublishedFrom: "2026-10-02", PublishedTo: "2026-10-03"}, []string{"b"}, 0},
		{"date excludes documents without counting them", types.ArticleSearchRequest{Sources: []string{"mine", "report"}, PublishedFrom: "2026-10-01"}, []string{}, 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.req.Query = "semanticlaunch"
			test.req.CollapseSimilar = articleSearchNoCollapse()
			response := h.search(t, test.req)
			require.Equal(t, test.ids, articleSearchIDs(response))
			require.Equal(t, test.missing, response.ExcludedMissingPublishedAt)
		})
	}
	response := h.search(t, types.ArticleSearchRequest{Query: "semanticlaunch", PublishedFrom: "2026-10-01", Limit: 1})
	require.EqualValues(t, 1, response.ExcludedMissingPublishedAt, "count is independent of limit and folding")
}

func TestArticleSearchPostgresSimilarReportsAndConfiguration(t *testing.T) {
	h := newArticleSearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "article", "2026-10-03", "event", "semanticlaunch", 1)
	h.add(t, "far", "article", "2026-10-09", "event", "semanticlaunch", 1)
	h.add(t, "other", "article", "2026-10-02", "event", "othersemantic", 1)
	h.add(t, "missing", "article", "", "event", "semanticlaunch", 1)
	h.add(t, "mine", "mine", "2026-10-01", "", "semanticlaunch", 1)
	h.add(t, "report", "report", "2026-10-01", "", "semanticlaunch", 1)
	response := h.search(t, types.ArticleSearchRequest{Query: "searchfixture"})
	require.Len(t, response.Results, 6)
	folded := 0
	for _, result := range response.Results {
		folded += len(result.SimilarReports)
		if len(result.SimilarReports) > 0 {
			require.Contains(t, []string{"a", "b"}, result.ArticleID)
			require.Contains(t, []string{"a", "b"}, result.SimilarReports[0].ArticleID)
			require.GreaterOrEqual(t, result.Score, result.SimilarReports[0].Score)
		}
	}
	require.Equal(t, 1, folded)
	response = h.search(t, types.ArticleSearchRequest{Query: "searchfixture", CollapseSimilar: articleSearchNoCollapse()})
	require.Len(t, response.Results, 7)
	for _, result := range response.Results {
		require.Empty(t, result.SimilarReports)
	}
	h.config.SimilarReportsWindowDays = 1
	require.Len(t, h.search(t, types.ArticleSearchRequest{Query: "searchfixture"}).Results, 7)
	h.config.SimilarReportsWindowDays = 10
	require.Len(t, h.search(t, types.ArticleSearchRequest{Query: "searchfixture"}).Results, 5)
	// Threshold equality and changes use the stored article vectors, with no model call.
	require.NoError(t, h.db.Exec("UPDATE article_embeddings SET embedding=? WHERE knowledge_id='b'", pgvector.NewHalfVector([]float32{0.86, 0.51, 0})).Error)
	h.config.SimilarReportsWindowDays = 3
	h.config.SimilarReportsThreshold = 0.9
	require.Len(t, h.search(t, types.ArticleSearchRequest{Query: "searchfixture"}).Results, 7)
	h.config.SimilarReportsThreshold = 0.85
	require.Len(t, h.search(t, types.ArticleSearchRequest{Query: "searchfixture"}).Results, 6)
}

type articleSearchRerankModels struct {
	interfaces.ModelService
	selected string
	err      error
}

func (m *articleSearchRerankModels) GetRerankModel(_ context.Context, id string) (rerank.Reranker, error) {
	m.selected = id
	return nil, m.err
}

func TestArticleSearchUsesConfiguredRerankTier(t *testing.T) {
	h := newArticleSearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	sentinel := errors.New("configured rerank unavailable")
	models := &articleSearchRerankModels{err: sentinel}
	svc := NewKnowledgeSearchService(articleSearchFixtureBases{kb: h.kb}, nil, models, h.db)
	for _, id := range []string{"selected-rerank", "replacement-rerank"} {
		h.kb.ModelTiers.Rerank = id
		_, err := svc.Search(h.ctx, types.ArticleSearchRequest{Query: "semanticlaunch"})
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, id, models.selected)
	}
	h.kb.ModelTiers.Rerank = ""
	models.selected = ""
	_, err := svc.Search(h.ctx, types.ArticleSearchRequest{Query: "semanticlaunch"})
	require.NoError(t, err)
	require.Empty(t, models.selected)
}

func TestArticleSearchRejectsInvalidFilters(t *testing.T) {
	svc := &knowledgeSearchService{}
	for _, request := range []types.ArticleSearchRequest{
		{}, {Query: "  "}, {Query: "test", Sources: []string{"unknown"}}, {Query: "test", ContentTypes: []string{"unknown"}},
		{Query: "test", EntityIDs: []string{""}}, {Query: "test", TopicIDs: []string{" "}},
		{Query: "test", Limit: 101}, {Query: "test", Limit: -1},
		{Query: "test", PublishedFrom: "yesterday"}, {Query: "test", PublishedTo: "2026-02-30"},
		{Query: "test", PublishedFrom: "2026-10-04", PublishedTo: "2026-10-03"},
	} {
		_, err := svc.Search(context.Background(), request)
		require.Error(t, err, "%+v", request)
	}
}

func TestArticleSearchEmbeddingFailure(t *testing.T) {
	h := newArticleSearchFixture(t)
	cause := errors.New("EmbedBatch API error: Http Status 503 <html>upstream secret</html>")
	svc := NewKnowledgeSearchService(articleSearchFixtureBases{kb: h.kb, embeddingError: cause}, nil, nil, h.db)
	result, err := svc.Search(h.ctx, types.ArticleSearchRequest{Query: "searchfixture"})
	require.Nil(t, result)
	require.ErrorIs(t, err, cause)
}
