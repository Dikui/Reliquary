package handler

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/file"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/middleware"
	"reliquary/internal/models/chat"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type ingestHandlerQueue struct{ tasks []*taskqueue.Task }

func (q *ingestHandlerQueue) Enqueue(task *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &taskqueue.TaskInfo{ID: "ingest-test-task"}, nil
}
func (q *ingestHandlerQueue) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}

type ingestHandlerChat struct{}

func (ingestHandlerChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	if strings.Contains(messages[1].Content, "rejectfixture") {
		return &types.ChatResponse{Content: `{"approved":false,"reason":"model rejected source"}`}, nil
	}
	return &types.ChatResponse{Content: `{"approved":true,"reason":"verified original"}`}, nil
}
func (ingestHandlerChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, nil
}
func (ingestHandlerChat) GetModelID() string   { return "intake-test" }
func (ingestHandlerChat) GetModelName() string { return "intake-test" }

type ingestHandlerModels struct{ interfaces.ModelService }

func (ingestHandlerModels) GetChatModel(context.Context, string) (chat.Chat, error) {
	return ingestHandlerChat{}, nil
}

type ingestHandlerBases struct{ stubArticleKBService }

func (ingestHandlerBases) GetKnowledgeBase(ctx context.Context) (*types.KnowledgeBase, error) {
	kb, err := (&stubArticleKBService{}).GetKnowledgeBase(ctx)
	kb.SummaryModelID = "intake-test"
	return kb, err
}

func TestIngestHTTPValidationCreatedDuplicateAndOperationLog(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Personal space"}).Error)
	spaceID := uint64(1)
	key := &types.APIKey{SpaceID: &spaceID, Name: "HTTP 外部工具", KeyHash: "ingest-http-fixture"}
	require.NoError(t, db.Create(key).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-1", SpaceID: 1, Name: "Articles", SummaryModelID: "intake-test", ModelTiers: types.ModelTierConfig{Light: "intake-test"}, Type: types.KnowledgeBaseTypeDocument}).Error)
	queue := &ingestHandlerQueue{}
	bases := &ingestHandlerBases{}
	articles, err := service.NewEntryService(db, &config.Config{}, repository.NewEntryRepository(db), bases,
		repository.NewSpaceRepository(db), nil, nil, nil, file.NewLocalFileService(t.TempDir(), ""), nil, ingestHandlerModels{},
		queue, nil, nil, docparser.NewImageResolver(), nil)
	require.NoError(t, err)
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	articles.(interface{ SetVault(*service.VaultService) }).SetVault(vault)
	t.Cleanup(func() { require.NoError(t, vault.Close()) })
	h := NewEntryHandler(nil, articles, bases, queue, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1))
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIKey, ID: fmt.Sprint(key.ID), Name: key.Name})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/api/v1/ingest", h.Ingest)
	for _, body := range []string{
		`{}`, `{"items":[]}`, `{"items":"bad"}`, `{"items": [{}],"unexpected":true}`,
		`{"items":[{}]} {}`, `{"url":"https://example.org/old-format","content":"Original"}`,
	} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	}
	invalidResponse := httptest.NewRecorder()
	invalidRequest := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(`{"items":[{"url":"https://example.org/missing-acquisition","content":"Original"},{"url":"https://example.org/wrong-type","content":42},{"acquisition":"extracted","unknown":true},null]}`))
	router.ServeHTTP(invalidResponse, invalidRequest)
	require.Equal(t, http.StatusOK, invalidResponse.Code, invalidResponse.Body.String())
	var invalid struct {
		Data types.IngestBatchResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(invalidResponse.Body.Bytes(), &invalid))
	require.Len(t, invalid.Data.Items, 4)
	for i, result := range invalid.Data.Items {
		require.Equal(t, i, result.Index)
		require.Equal(t, "invalid", result.Status)
	}
	require.Empty(t, queue.tasks)
	rejectedResponse := httptest.NewRecorder()
	rejectedRequest := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(`{"items":[{"url":"https://example.org/rejected","content":"rejectfixture complete original","publisher":"Journal","acquisition":"extracted"}]}`))
	rejectedRequest.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rejectedResponse, rejectedRequest)
	require.Equal(t, http.StatusOK, rejectedResponse.Code, rejectedResponse.Body.String())
	var rejected struct {
		Data types.IngestBatchResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rejectedResponse.Body.Bytes(), &rejected))
	require.Equal(t, "pending_review", rejected.Data.Items[0].Status)
	require.Equal(t, "model rejected source", rejected.Data.Items[0].Reason)
	require.Empty(t, queue.tasks)
	var pending types.Entry
	require.NoError(t, db.First(&pending, "id=?", rejected.Data.Items[0].EntryID).Error)
	require.Equal(t, types.ParseStatusRejected, pending.ParseStatus)
	require.Empty(t, pending.VaultPath)
	input := types.IngestRequest{Acquisition: "extracted", URL: "https://example.org/ada/article123", Publisher: "Journal", Content: "# Complete original\n\nMeasured product capabilities.", Title: "External article", SourceName: "grok-bot", AgentSummary: "annotation-summary", AgentTags: []string{"annotation-tag"}, Thread: []types.IngestThreadItem{{Author: "Ada", PublishedAt: "2026-10-01", Content: "Supporting thread."}}, Quotes: []types.IngestQuote{{Content: "Quoted external source."}}}
	var articleID string
	for _, attempt := range []struct {
		status int
		state  string
	}{{http.StatusOK, "accepted"}, {http.StatusOK, "exists"}} {
		if articleID != "" {
			input.CollectReason = "Retain these measurements"
		}
		body, err := json.Marshal(types.IngestBatchRequest{Items: []types.IngestRequest{input}})
		require.NoError(t, err)
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", strings.NewReader(string(body)))
		request.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(response, request)
		require.Equal(t, attempt.status, response.Code, response.Body.String())
		var result struct {
			Success bool                    `json:"success"`
			Data    types.IngestBatchResult `json:"data"`
		}
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		require.True(t, result.Success)
		require.Equal(t, attempt.state, result.Data.Items[0].Status)
		require.NotNil(t, result.Data.Items[0].ImageFailures)
		if articleID == "" {
			articleID = result.Data.Items[0].EntryID
		} else {
			require.Equal(t, articleID, result.Data.Items[0].EntryID)
			require.Equal(t, "该文章已收录。", result.Data.Items[0].Message)
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
	var article types.Entry
	require.NoError(t, db.First(&article, "id=?", articleID).Error)
	require.Equal(t, "Retain these measurements", article.CollectReason)
	require.Equal(t, "grok-bot", article.SourceName)
	require.Equal(t, "annotation-summary", article.AgentSummary)
	require.JSONEq(t, `["annotation-tag"]`, string(article.AgentTags))
	var logs []types.OperationLog
	require.NoError(t, db.Where("operation_type=?", "ingest").Order("created_at,id").Find(&logs).Error)
	require.Len(t, logs, 3)
	for _, log := range logs {
		require.Equal(t, "api_key:"+fmt.Sprint(key.ID), log.Actor)
		require.Equal(t, key.Name, log.APIKeyName)
		require.Contains(t, []string{articleID, rejected.Data.Items[0].EntryID}, log.ObjectID)
	}
}

func TestIngestHTTPUploadReferenceAndWhitelist(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Personal space"}).Error)
	spaceID := uint64(1)
	key := &types.APIKey{SpaceID: &spaceID, Name: "HTTP 文件工具", KeyHash: "ingest-http-assets"}
	require.NoError(t, db.Create(key).Error)
	require.NoError(t, db.Create(&types.KnowledgeBase{ID: "kb-1", SpaceID: 1, Name: "Articles", SummaryModelID: "intake-test", ModelTiers: types.ModelTierConfig{Light: "intake-test"}, Type: types.KnowledgeBaseTypeDocument}).Error)
	queue := &ingestHandlerQueue{}
	bases := &ingestHandlerBases{}
	articles, err := service.NewEntryService(db, &config.Config{}, repository.NewEntryRepository(db), bases, repository.NewSpaceRepository(db), nil, nil, nil, file.NewLocalFileService(t.TempDir(), ""), nil, ingestHandlerModels{}, queue, nil, nil, nil, nil)
	require.NoError(t, err)
	vault, err := service.NewVaultService(db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	articles.(interface{ SetVault(*service.VaultService) }).SetVault(vault)
	t.Cleanup(func() { require.NoError(t, vault.Close()) })
	handler := NewEntryHandler(nil, articles, bases, queue, nil)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(middleware.ErrorHandler(), func(c *gin.Context) {
		ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, spaceID)
		ctx = types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIKey, ID: fmt.Sprint(key.ID), Name: key.Name})
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	})
	router.POST("/ingest", handler.Ingest)
	router.POST("/ingest/assets", handler.UploadIngestAsset)
	upload := func(name string, data []byte) *httptest.ResponseRecorder {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", name)
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		request := httptest.NewRequest(http.MethodPost, "/ingest/assets", &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	png, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jGmEAAAAASUVORK5CYII=")
	require.NoError(t, err)
	response := upload("chart.png", png)
	require.Equal(t, http.StatusCreated, response.Code, response.Body.String())
	var uploaded struct {
		Data types.IngestAssetResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &uploaded))
	require.NotEmpty(t, uploaded.Data.AssetID)
	require.Equal(t, "png", uploaded.Data.Type)
	require.EqualValues(t, len(png), uploaded.Data.Size)
	body, err := json.Marshal(types.IngestBatchRequest{Items: []types.IngestRequest{{URL: "https://example.org/uploaded-chart", Acquisition: "verbatim", Content: "# Measurements\n\nOriginal source values.\n\n![Chart](https://example.org/chart.png)", Publisher: "Fixture Journal", Images: []types.IngestImage{{AssetID: uploaded.Data.AssetID, SourceURL: "https://example.org/chart.png"}}}}})
	require.NoError(t, err)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/ingest", bytes.NewReader(body)))
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var ingested struct {
		Data types.IngestBatchResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &ingested))
	require.Equal(t, "accepted", ingested.Data.Items[0].Status)
	var asset types.IngestAsset
	require.NoError(t, db.First(&asset, "id=?", uploaded.Data.AssetID).Error)
	require.NotNil(t, asset.ReferencedAt)
	require.Equal(t, ingested.Data.Items[0].EntryID, asset.EntryID)
	response = upload("program.exe", []byte("unexpected executable"))
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
	t.Setenv("MAX_FILE_SIZE_MB", "1")
	response = upload("oversized.pdf", make([]byte, (1<<20)+1))
	require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
}
