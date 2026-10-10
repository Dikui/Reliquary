package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/file"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/middleware"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type ingestHandlerQueue struct{ tasks []*asynq.Task }

func (q *ingestHandlerQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "ingest-test-task"}, nil
}

func TestIngestHTTPValidationCreatedDuplicateAndOperationLog(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Personal space"}).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-1", TenantID: 1, Name: "Articles", Type: types.KnowledgeBaseTypeDocument}).Error)
	queue := &ingestHandlerQueue{}
	bases := &stubArticleKBService{}
	articles, err := service.NewKnowledgeService(db, &config.Config{}, repository.NewKnowledgeRepository(db), bases,
		repository.NewTenantRepository(db), nil, nil, nil, file.NewLocalFileService(t.TempDir(), ""), nil, nil,
		queue, nil, nil, nil, docparser.NewImageResolver(), nil)
	require.NoError(t, err)
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	articles.(interface{ SetVault(*service.VaultService) }).SetVault(vault)
	t.Cleanup(func() { require.NoError(t, vault.Close()) })
	h := NewKnowledgeHandler(nil, articles, bases, queue, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, uint64(1))
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "external-test"})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/api/v1/ingest", h.Ingest)
	for _, body := range []string{
		`{}`, `{"url":"https://x.com/a/1"}`, `{"content":"Original"}`,
		`{"url":"https://x.com/a/1","content":" \n "}`,
		`{"url":"https://x.com/a/1","content":"Original","unexpected":true}`,
		`{"url":"https://x.com/a/1","content":"Original"} {}`,
		`{"url":"https://x.com/a/1","content":"Original","images":[{"url":"https://example.org/image.png","data":"YmFzZTY0"}]}`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	require.Empty(t, queue.tasks)
	input := types.IngestRequest{URL: "https://x.com/ada/status/123", Content: "# Complete original\n\nMeasured product capabilities.", Title: "External article", SourceName: "grok-bot", AgentSummary: "annotation-summary", AgentTags: []string{"annotation-tag"}, Thread: []types.IngestThreadItem{{Author: "Ada", PublishedAt: "2026-10-01", Content: "Supporting thread."}}, Quotes: []types.IngestQuote{{Content: "Quoted external source."}}}
	var articleID string
	for _, attempt := range []struct {
		status int
		state  string
	}{{http.StatusCreated, "pending"}, {http.StatusOK, "exists"}} {
		if articleID != "" {
			input.CollectReason = "Retain these measurements"
		}
		body, err := json.Marshal(input)
		require.NoError(t, err)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, attempt.status, response.Code, response.Body.String())
		var result struct {
			Success bool               `json:"success"`
			Data    types.IngestResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success)
		require.Equal(t, attempt.state, result.Data.Status)
		require.NotNil(t, result.Data.ImageFailures)
		if articleID == "" {
			articleID = result.Data.ArticleID
		} else {
			require.Equal(t, articleID, result.Data.ArticleID)
			require.Equal(t, "已存在", result.Data.Message)
		}
	}
	require.Len(t, queue.tasks, 1)
	var payload types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(queue.tasks[0].Payload(), &payload))
	require.Contains(t, payload.Content, "## 推文串")
	require.Contains(t, payload.Content, "## 引用")
	require.NotContains(t, string(queue.tasks[0].Payload()), "annotation-summary")
	require.NotContains(t, string(queue.tasks[0].Payload()), "annotation-tag")
	require.NotContains(t, string(queue.tasks[0].Payload()), "grok-bot")
	var article types.Knowledge
	require.NoError(t, db.First(&article, "id=?", articleID).Error)
	require.Equal(t, "Retain these measurements", article.CollectReason)
	require.Equal(t, "grok-bot", article.SourceName)
	require.Equal(t, "annotation-summary", article.AgentSummary)
	require.JSONEq(t, `["annotation-tag"]`, string(article.AgentTags))
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type=?", "ingest").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 2)
	for _, log := range logs {
		require.Equal(t, "api_key:external-test", log.Actor)
		require.Equal(t, articleID, log.ObjectID)
	}
}
