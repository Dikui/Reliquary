package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type sourceModifiedCopyKBService struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s sourceModifiedCopyKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func (s sourceModifiedCopyKBService) GetKnowledgeBaseByIDOnly(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

func TestWikiSourceModifiedHTTPDescriptionsUseRequestLanguageAndCount(t *testing.T) {
	t.Setenv("RELIQUARY_LANGUAGE", "")
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	kb := &types.KnowledgeBase{ID: uuid.NewString(), TenantID: 1, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}}
	kbSvc := sourceModifiedCopyKBService{kb: kb}
	repo := repository.NewWikiPageRepository(db)
	chunks := repository.NewChunkRepository(db)
	ids := []string{uuid.NewString(), uuid.NewString()}
	for _, id := range ids {
		require.NoError(t, chunks.CreateChunks(ctx, []*types.Chunk{{
			ID: id, TenantID: 1, KnowledgeBaseID: kb.ID, KnowledgeID: uuid.NewString(),
			ChunkType: types.ChunkTypeText, Content: "Original source", IsEnabled: true,
		}}))
	}
	page := &types.WikiPage{
		ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: kb.ID, Slug: "concept/orion",
		Title: "Orion 页面", Content: strings.Repeat("Wiki page content. ", 6),
		PageType: types.WikiPageTypeConcept, Status: types.WikiPageStatusPublished, ChunkRefs: ids, Version: 1,
	}
	require.NoError(t, repo.Create(ctx, page))
	wikiSvc := service.NewWikiPageService(repo, chunks, kbSvc, nil, nil)
	handler := &WikiPageHandler{
		wikiService: wikiSvc, kbService: kbSvc,
		lintService: service.NewWikiLintService(wikiSvc, kbSvc, nil),
	}
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.Language(), func(c *gin.Context) {
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1)))
		c.Next()
	})
	router.GET("/knowledgebase/:kb_id/wiki/lint", handler.Lint)
	router.GET("/knowledgebase/:kb_id/wiki/issues", handler.ListIssues)
	router.GET("/knowledgebase/:kb_id/wiki/stats", handler.GetStats)
	router.POST("/knowledgebase/:kb_id/wiki/source-changes/ignore", handler.IgnoreSourceChanges)
	request := func(path, language string, result interface{}) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/knowledgebase/"+kb.ID+"/wiki/"+path, nil)
		req.Header.Set("Accept-Language", language)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), result))
	}
	for _, tc := range []struct {
		count  int
		zh, en string
	}{
		{1, "页面“Orion 页面”的 1 个来源分块已修改", "Page 'Orion 页面' has 1 modified source chunk"},
		{2, "页面“Orion 页面”的 2 个来源分块已修改", "Page 'Orion 页面' has 2 modified source chunks"},
	} {
		require.NoError(t, repo.ClearSourceChanges(ctx, kb.ID, page.Slug, nil))
		require.NoError(t, repo.MarkSourceChanged(ctx, 1, ids[:tc.count], time.Now()))
		for _, locale := range []struct{ header, description string }{
			{"zh-CN,zh;q=0.9", tc.zh}, {"en-US,en;q=0.9", tc.en},
		} {
			var lint service.WikiLintReport
			request("lint", locale.header, &lint)
			found := 0
			for _, issue := range lint.Issues {
				if issue.Type == service.LintIssueSourceModified {
					require.Equal(t, locale.description, issue.Description)
					found++
				}
			}
			require.Equal(t, 1, found)
			var issues []*types.WikiPageIssue
			request("issues?status=pending", locale.header, &issues)
			require.Len(t, issues, 1)
			require.Equal(t, "source_modified", issues[0].IssueType)
			require.Equal(t, page.Slug, issues[0].Slug)
			require.Equal(t, locale.description, issues[0].Description)
			var stats types.WikiStats
			request("stats", locale.header, &stats)
			require.EqualValues(t, 1, stats.PendingIssues, "count pages with a source problem, not affected chunks")
		}
	}

	body, err := json.Marshal(types.WikiSourceChangesRequest{Slug: page.Slug})
	require.NoError(t, err)
	ignore := httptest.NewRequest(http.MethodPost, "/knowledgebase/"+kb.ID+"/wiki/source-changes/ignore", strings.NewReader(string(body)))
	ignore.Header.Set("Content-Type", "application/json")
	ignored := httptest.NewRecorder()
	router.ServeHTTP(ignored, ignore)
	require.Equal(t, http.StatusOK, ignored.Code, ignored.Body.String())
	var remaining []*types.WikiPageIssue
	request("issues?status=pending", "zh-CN", &remaining)
	require.Empty(t, remaining)
	var after types.WikiStats
	request("stats", "en-US", &after)
	require.Zero(t, after.PendingIssues)
}
