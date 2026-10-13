package router

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/types"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func TestEntrySearchAndFollowHTTPPostgresIncompleteInResultsAndSimilarReports(t *testing.T) {
	f := newPersonalAuthFixture(t)
	response := personalRequest(t, f.router, http.MethodGet, "/api/v1/library", "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var base struct{ Data types.Library }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &base))
	base.Data.IndexingStrategy = types.IndexingStrategy{KeywordEnabled: true}
	base.Data.ModelTiers = types.ModelTierConfig{Embedding: "incomplete-fixture"}
	require.NoError(t, f.db.Save(&base.Data).Error)
	search := service.NewSearchService(f.params.LibraryService, f.params.SpaceService, nil, f.db)
	f.router.POST("/api/v1/search", handler.NewSearchHandler(search, nil).SearchEntry)
	RegisterFollowRoutes(f.router.Group("/api/v1"), handler.NewFollowHandler(service.NewFollowService(f.db)))
	topic := types.Topic{ID: "11111111-1111-4111-8111-111111111111", SpaceID: types.SingleSpaceID, Name: "Incomplete fixture"}
	require.NoError(t, f.db.Create(&topic).Error)
	response = personalRequest(t, f.router, http.MethodPut, "/api/v1/follows/topic/"+topic.ID, "", f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	ids := []string{"22222222-2222-4222-8222-222222222222", "33333333-3333-4333-8333-333333333333"}
	for index, id := range ids {
		collectedAt := time.Now().UTC().Add(time.Duration(-index-1) * time.Hour)
		article := &types.Entry{ID: id, SpaceID: types.SingleSpaceID, LibraryID: base.Data.ID,
			Type: "manual", Title: "Incompletefixture source", SourceKind: "article", Source: "https://example.org/" + id,
			ContentType: "event", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled", CreatedAt: collectedAt,
			StructuredSummary: &types.EntrySummary{Overview: "Incompletefixture summary"}}
		require.NoError(t, article.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "approved",
			SourceInfo: types.SourceInfo{OriginalURL: article.Source, PublishedAt: "2026-10-01"}}))
		require.NoError(t, f.db.Create(article).Error)
		require.NoError(t, f.db.Create(&types.EntryTopic{SpaceID: types.SingleSpaceID, EntryID: id, TopicID: topic.ID, CreatedAt: collectedAt}).Error)
		require.NoError(t, f.db.Exec(`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash)
			VALUES (?, ?, ?, 'incomplete-fixture', 3, ?, 'fixture')`, id, types.SingleSpaceID, base.Data.ID, pgvector.NewHalfVector([]float32{1, 0, 0})).Error)
	}
	type articleWire struct {
		EntryID        string        `json:"entry_id"`
		Incomplete     *bool         `json:"incomplete"`
		SimilarReports []articleWire `json:"similar_reports"`
	}
	for _, incomplete := range []bool{false, true} {
		require.NoError(t, f.db.Model(&types.Entry{}).Where("id=?", ids[0]).Update("incomplete", incomplete).Error)
		require.NoError(t, f.db.Model(&types.Entry{}).Where("id=?", ids[1]).Update("incomplete", !incomplete).Error)
		for _, collapse := range []bool{false, true} {
			for _, endpoint := range []string{"/api/v1/search", "/api/v1/follows/feed"} {
				t.Run(fmt.Sprintf("%s/incomplete=%t/collapse=%t", endpoint, incomplete, collapse), func(t *testing.T) {
					body := fmt.Sprintf(`{"query":"Incompletefixture","collapse_similar":%t}`, collapse)
					if endpoint == "/api/v1/follows/feed" {
						body = fmt.Sprintf(`{"include_seen":true,"collapse_similar":%t}`, collapse)
					}
					response := personalRequest(t, f.router, http.MethodPost, endpoint, body, f.key, true)
					require.Equal(t, http.StatusOK, response.Code, response.Body.String())
					var envelope struct {
						Data struct {
							Results []articleWire `json:"results"`
						} `json:"data"`
					}
					require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
					if collapse {
						require.Len(t, envelope.Data.Results, 1)
						require.Len(t, envelope.Data.Results[0].SimilarReports, 1)
					} else {
						require.Len(t, envelope.Data.Results, 2)
					}
					expected := map[string]bool{ids[0]: incomplete, ids[1]: !incomplete}
					seen := map[string]bool{}
					var check func(articleWire)
					check = func(article articleWire) {
						require.NotNil(t, article.Incomplete, "JSON must return incomplete even when false")
						require.Equal(t, expected[article.EntryID], *article.Incomplete, article.EntryID)
						seen[article.EntryID] = true
						for _, child := range article.SimilarReports {
							check(child)
						}
					}
					for _, article := range envelope.Data.Results {
						check(article)
					}
					require.Equal(t, map[string]bool{ids[0]: true, ids[1]: true}, seen)
				})
			}
		}
	}
}
