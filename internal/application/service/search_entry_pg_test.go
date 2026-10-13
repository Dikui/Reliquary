package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"reliquary/internal/models/rerank"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type entrySearchFixtureBases struct {
	interfaces.LibraryService
	library        *types.Library
	embeddingError error
}

func (b entrySearchFixtureBases) GetSystemLibrary(context.Context, ...string) (*types.Library, error) {
	return b.library, nil
}
func (b entrySearchFixtureBases) GetQueryEmbedding(_ context.Context, _ string, query string) ([]float32, error) {
	if b.embeddingError != nil {
		return nil, b.embeddingError
	}
	return entrySearchFixtureEmbedding(query), nil
}

// The same text always has the same vector; markers control semantic distance.
func entrySearchFixtureEmbedding(text string) []float32 {
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

type entrySearchFixture struct {
	db      *gorm.DB
	ctx     context.Context
	library *types.Library
	config  *types.RetrievalConfig
	svc     interfaces.SearchService
}

func newEntrySearchFixture(t *testing.T) *entrySearchFixture {
	t.Helper()
	db := pgtest.Open(t)
	config := &types.RetrievalConfig{RerankTopK: 100, KeywordThreshold: 0.00001}
	space := &types.Space{ID: 1, Name: "Search fixture", EmbeddingModelID: "fixture-model", RetrievalConfig: config}
	require.NoError(t, db.Create(space).Error)
	library := &types.Library{ID: "search-library", SpaceID: 1, Template: types.TemplateExternalTimely, SourceEvidenceRule: "link_required", Name: "Search", FeedEnabled: true, Type: types.LibraryTypeDocument,
		EmbeddingModelID: "fixture-model", ModelTiers: types.ModelTierConfig{Embedding: "fixture-model"},
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true, KeywordEnabled: true}}
	require.NoError(t, db.Create(library).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	svc := NewSearchService(entrySearchFixtureBases{library: library}, nil, nil, db)
	return &entrySearchFixture{db: db, ctx: ctx, library: library, config: config, svc: svc}
}

func (h *entrySearchFixture) add(t *testing.T, id, source, date, contentType, semantic string, count int, libraries ...string) *types.Entry {
	t.Helper()
	libraryID := h.library.ID
	if len(libraries) > 0 {
		libraryID = libraries[0]
	}
	article := &types.Entry{ID: id, SpaceID: 1, LibraryID: libraryID, Type: "manual",
		Title: "Searchfixture " + id, Source: "https://example.org/" + id, SourceKind: source,
		ContentType: contentType,
		ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", EmbeddingModelID: "fixture-model",
		VaultPath: source + "/" + id + ".md"}
	if source == "article" {
		article.StructuredSummary = &types.EntrySummary{Overview: semantic, Points: []string{"Evidence point"}, KeyTerms: []string{"Termneedle" + id}}
		article.Description = article.StructuredSummary.RenderText()
	} else {
	}
	metadata, err := json.Marshal(map[string]string{"publisher": "Publisherneedle" + id, "author": "Authorneedle" + id, "published_at": date, "original_url": article.Source})
	require.NoError(t, err)
	article.Metadata = metadata
	require.NoError(t, h.db.Create(article).Error)
	if source == "article" {
		vector := pgvector.NewHalfVector(entrySearchFixtureEmbedding(article.Title + "\n" + article.Description))
		require.NoError(t, h.db.Exec(`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash)
			VALUES (?,1,?,'fixture-model',3,?,'fixture')`, article.ID, libraryID, vector).Error)
	}
	for i := 0; i < count; i++ {
		chunk := &types.Chunk{ID: fmt.Sprintf("%s-chunk-%03d", id, i), SpaceID: 1, LibraryID: libraryID,
			EntryID: id, ChunkIndex: i, Content: "searchfixture " + semantic + fmt.Sprintf(" body passage %d", i),
			ChunkType: types.ChunkTypeText, IsEnabled: true}
		require.NoError(t, h.db.Create(chunk).Error)
		require.NoError(t, h.db.Exec(`INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,library_id,dimension,embedding,is_enabled)
			VALUES (?,0,?,?,?,?,?,true)`, chunk.ID, chunk.ID, article.ID, libraryID, 3, pgvector.NewHalfVector(entrySearchFixtureEmbedding(chunk.Content))).Error)
	}
	return article
}

func (h *entrySearchFixture) search(t *testing.T, request types.EntrySearchRequest) *types.EntrySearchResponse {
	t.Helper()
	response, err := h.svc.Search(h.ctx, request)
	require.NoError(t, err)
	return response
}

func entrySearchIDs(response *types.EntrySearchResponse) []string {
	ids := []string{}
	for _, result := range response.Results {
		ids = append(ids, result.EntryID)
	}
	slices.Sort(ids)
	return ids
}

func entrySearchNoCollapse() *bool { value := false; return &value }

func TestEntrySearchPostgresIncompleteStateIncludesDocuments(t *testing.T) {
	h := newEntrySearchFixture(t)
	for _, item := range []struct {
		id         string
		source     string
		incomplete bool
	}{
		{"complete", "article", false},
		{"incomplete", "article", true},
		{"mine", "mine", true},
		{"report", "report", true},
	} {
		article := h.add(t, item.id, item.source, "2026-10-01", "event", "semanticlaunch", 0)
		require.NoError(t, h.db.Model(article).Update("incomplete", item.incomplete).Error)
	}
	response := h.search(t, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, []string{"complete", "incomplete", "mine", "report"}, entrySearchIDs(response))
	for _, result := range response.Results {
		require.Equal(t, result.EntryID == "incomplete" || result.EntryID == "mine", result.Incomplete, result.EntryID)
	}
}

func TestEntrySearchPostgresHeaderTermsAndAssociations(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "summary", "article", "2026-10-01", "reference", "semanticlaunch", 0)
	entity := &types.Entity{ID: "entity-a", SpaceID: 1, CanonicalName: "Canonicalneedle", NormalizedName: "canonicalneedle", EntityType: "company"}
	require.NoError(t, h.db.Create(entity).Error)
	require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: "summary", EntityID: entity.ID}).Error)
	require.NoError(t, h.db.Create(&types.EntityAlias{ID: "alias-a", SpaceID: 1, EntityID: entity.ID, Alias: "Aliasneedle", NormalizedAlias: "aliasneedle", EntityType: "company"}).Error)
	require.NoError(t, h.db.Create(&types.Topic{ID: "topic-a", SpaceID: 1, Name: "Topicneedle"}).Error)
	require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: "summary", TopicID: "topic-a"}).Error)
	h.library.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	for _, term := range []string{"Canonicalneedle", "Aliasneedle", "Topicneedle", "Termneedlesummary", "Publisherneedlesummary", "Authorneedlesummary"} {
		t.Run(term, func(t *testing.T) {
			response := h.search(t, types.EntrySearchRequest{Query: term})
			require.Equal(t, []string{"summary"}, entrySearchIDs(response))
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
		require.Equal(t, []string{"summary"}, entrySearchIDs(h.search(t, types.EntrySearchRequest{Query: term})))
	}
}

func TestEntrySearchPostgresFiltersAndMissingPublicationCount(t *testing.T) {
	h := newEntrySearchFixture(t)
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
		require.NoError(t, h.db.Create(&types.EntryEntity{SpaceID: 1, EntryID: id, EntityID: "entity-" + id}).Error)
		require.NoError(t, h.db.Create(&types.EntryTopic{SpaceID: 1, EntryID: id, TopicID: "topic-" + id}).Error)
	}
	// Keep query recall entirely semantic so the unrelated missing article is excluded.
	h.library.IndexingStrategy.KeywordEnabled = false
	tests := []struct {
		name    string
		req     types.EntrySearchRequest
		ids     []string
		missing int64
	}{
		{"content", types.EntrySearchRequest{ContentTypes: []string{"event", "tutorial"}}, []string{"a", "b", "missing"}, 0},
		{"entities", types.EntrySearchRequest{EntityIDs: []string{"entity-a", "entity-b"}}, []string{"a", "b"}, 0},
		{"topics", types.EntrySearchRequest{TopicIDs: []string{"topic-b"}}, []string{"b"}, 0},
		{"sources", types.EntrySearchRequest{Sources: []string{"mine", "report"}}, []string{"mine", "report"}, 0},
		{"date", types.EntrySearchRequest{PublishedFrom: "2026-10-01", PublishedTo: "2026-10-03"}, []string{"a", "b"}, 2},
		{"datetime", types.EntrySearchRequest{PublishedFrom: "2026-10-03T23:59:59Z", PublishedTo: "2026-10-03T23:59:59Z"}, []string{"b"}, 2},
		{"combined", types.EntrySearchRequest{ContentTypes: []string{"event", "tutorial"}, EntityIDs: []string{"entity-a", "entity-b"}, TopicIDs: []string{"topic-b"}, Sources: []string{"article"}, PublishedFrom: "2026-10-02", PublishedTo: "2026-10-03"}, []string{"b"}, 0},
		{"date counts missing document dates", types.EntrySearchRequest{Sources: []string{"mine", "report"}, PublishedFrom: "2026-10-01"}, []string{}, 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.req.Query = "semanticlaunch"
			test.req.CollapseSimilar = entrySearchNoCollapse()
			response := h.search(t, test.req)
			require.Equal(t, test.ids, entrySearchIDs(response))
			require.Equal(t, test.missing, response.ExcludedMissingPublishedAt)
		})
	}
	response := h.search(t, types.EntrySearchRequest{Query: "semanticlaunch", PublishedFrom: "2026-10-01", Limit: 1})
	require.EqualValues(t, 2, response.ExcludedMissingPublishedAt, "count is independent of limit and folding")
}

func TestEntrySearchPostgresSimilarReportsAndConfiguration(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	h.add(t, "b", "article", "2026-10-03", "event", "semanticlaunch", 1)
	h.add(t, "far", "article", "2026-10-09", "event", "semanticlaunch", 1)
	h.add(t, "other", "article", "2026-10-02", "event", "othersemantic", 1)
	h.add(t, "missing", "article", "", "event", "semanticlaunch", 1)
	h.add(t, "mine", "mine", "2026-10-01", "", "semanticlaunch", 1)
	h.add(t, "report", "report", "2026-10-01", "", "semanticlaunch", 1)
	response := h.search(t, types.EntrySearchRequest{Query: "searchfixture"})
	require.Len(t, response.Results, 6)
	folded := 0
	for _, result := range response.Results {
		folded += len(result.SimilarReports)
		if len(result.SimilarReports) > 0 {
			require.Contains(t, []string{"a", "b"}, result.EntryID)
			require.Contains(t, []string{"a", "b"}, result.SimilarReports[0].EntryID)
			require.GreaterOrEqual(t, result.Score, result.SimilarReports[0].Score)
		}
	}
	require.Equal(t, 1, folded)
	response = h.search(t, types.EntrySearchRequest{Query: "searchfixture", CollapseSimilar: entrySearchNoCollapse()})
	require.Len(t, response.Results, 7)
	for _, result := range response.Results {
		require.Empty(t, result.SimilarReports)
	}
	h.config.SimilarReportsWindowDays = 1
	require.Len(t, h.search(t, types.EntrySearchRequest{Query: "searchfixture"}).Results, 7)
	h.config.SimilarReportsWindowDays = 10
	require.Len(t, h.search(t, types.EntrySearchRequest{Query: "searchfixture"}).Results, 5)
	// Threshold equality and changes use the stored article vectors, with no model call.
	require.NoError(t, h.db.Exec("UPDATE article_embeddings SET embedding=? WHERE entry_id='b'", pgvector.NewHalfVector([]float32{0.86, 0.51, 0})).Error)
	h.config.SimilarReportsWindowDays = 3
	h.config.SimilarReportsThreshold = 0.9
	require.Len(t, h.search(t, types.EntrySearchRequest{Query: "searchfixture"}).Results, 7)
	h.config.SimilarReportsThreshold = 0.85
	require.Len(t, h.search(t, types.EntrySearchRequest{Query: "searchfixture"}).Results, 6)
}

type entrySearchRerankModels struct {
	interfaces.ModelService
	selected string
	err      error
}

func (m *entrySearchRerankModels) GetRerankModel(_ context.Context, id string) (rerank.Reranker, error) {
	m.selected = id
	return nil, m.err
}

func TestEntrySearchUsesConfiguredRerankTier(t *testing.T) {
	h := newEntrySearchFixture(t)
	h.add(t, "a", "article", "2026-10-01", "event", "semanticlaunch", 1)
	sentinel := errors.New("configured rerank unavailable")
	models := &entrySearchRerankModels{err: sentinel}
	svc := NewSearchService(entrySearchFixtureBases{library: h.library}, nil, models, h.db)
	for _, id := range []string{"selected-rerank", "replacement-rerank"} {
		h.library.ModelTiers.Rerank = id
		_, err := svc.Search(h.ctx, types.EntrySearchRequest{Query: "semanticlaunch"})
		require.ErrorIs(t, err, sentinel)
		require.Equal(t, id, models.selected)
	}
	h.library.ModelTiers.Rerank = ""
	models.selected = ""
	_, err := svc.Search(h.ctx, types.EntrySearchRequest{Query: "semanticlaunch"})
	require.NoError(t, err)
	require.Empty(t, models.selected)
}

func TestEntrySearchRejectsInvalidFilters(t *testing.T) {
	svc := &searchService{}
	for _, request := range []types.EntrySearchRequest{
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

func TestEntrySearchEmbeddingFailure(t *testing.T) {
	h := newEntrySearchFixture(t)
	cause := errors.New("EmbedBatch API error: Http Status 503 <html>upstream secret</html>")
	svc := NewSearchService(entrySearchFixtureBases{library: h.library, embeddingError: cause}, nil, nil, h.db)
	result, err := svc.Search(h.ctx, types.EntrySearchRequest{Query: "searchfixture"})
	require.Nil(t, result)
	require.ErrorIs(t, err, cause)
}
