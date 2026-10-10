package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

	"reliquary/internal/application/repository"
	files "reliquary/internal/application/service/file"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type visionIntegrationModels struct{ interfaces.ModelService }

func (visionIntegrationModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return parentChildEmbedder{}, nil
}

type visionIntegrationQueue struct{ tasks []*asynq.Task }

func (q *visionIntegrationQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.tasks = append(q.tasks, task)
	return &asynq.TaskInfo{ID: "vision-integration-task"}, nil
}

type visionIntegrationRequest struct {
	model       string
	prompt      string
	imageURL    string
	path        string
	apiKey      string
	customValue string
	parts       int
	decodeErr   error
}

func TestVisionConversationImageUploadProducesOCRAndCaption(t *testing.T) {
	db := pgtest.Open(t)
	withSSRFWhitelist(t, "127.0.0.1,localhost")
	ctx := newCreateKnowledgeFileContext()
	require.NoError(t, EnsureSingleTeamSpace(ctx, db))

	var requestsMu sync.Mutex
	var requests []visionIntegrationRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model    string `json:"model"`
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		response := `{"approved":true,"reason":"complete sourced image"}`
		for _, message := range req.Messages {
			if len(message.Content) == 0 || message.Content[0] != '[' {
				continue
			}
			var parts []struct {
				Type     string `json:"type"`
				Text     string `json:"text"`
				ImageURL *struct {
					URL string `json:"url"`
				} `json:"image_url"`
			}
			captured := visionIntegrationRequest{model: req.Model, path: r.URL.Path, apiKey: r.Header.Get("Authorization"), customValue: r.Header.Get("X-Vision-Integration")}
			captured.decodeErr = json.Unmarshal(message.Content, &parts)
			captured.parts = len(parts)
			for _, part := range parts {
				if part.Type == "text" {
					captured.prompt = part.Text
				}
				if part.Type == "image_url" && part.ImageURL != nil {
					captured.imageURL = part.ImageURL.URL
				}
			}
			if strings.Contains(captured.prompt, "You are an OCR assistant") {
				response = "测试图中文字：Reliquary 05"
			} else {
				response = "图片显示一张包含 Reliquary 05 文字的测试卡片。"
			}
			requestsMu.Lock()
			requests = append(requests, captured)
			requestsMu.Unlock()
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "vision-test", "object": "chat.completion", "model": req.Model,
			"choices": []map[string]any{{"index": 0, "finish_reason": "stop", "message": map[string]string{"role": "assistant", "content": response}}},
		})
	}))
	t.Cleanup(server.Close)

	models := NewModelService(repository.NewModelRepository(db), nil, nil, nil, nil)
	model := &types.Model{
		TenantID: types.SingleTenantID, Name: "vision-conversation", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceRemote,
		Parameters: types.ModelParameters{
			SupportsVision: true, BaseURL: server.URL + "/v1", APIKey: "vision-test-key",
			CustomHeaders: map[string]string{"X-Vision-Integration": "conversation-image-processing"},
		},
	}
	require.NoError(t, models.CreateModel(ctx, model))
	kbRepo := repository.NewKnowledgeBaseRepository(db)
	knowledgeRepo := repository.NewKnowledgeRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	tenantRepo := repository.NewTenantRepository(db)
	kbService := &knowledgeBaseService{db: db, repo: kbRepo, kgRepo: knowledgeRepo, modelService: models}
	kb, err := kbService.CreateKnowledgeBase(ctx, &types.KnowledgeBase{
		Name: "Vision conversation knowledge base", EmbeddingModelID: "test-embedding", SummaryModelID: model.ID,
		VLMConfig: types.VLMConfig{Enabled: true, ModelID: model.ID},
	})
	require.NoError(t, err)
	selected, err := kbService.GetKnowledgeBaseByID(ctx, kb.ID)
	require.NoError(t, err)
	require.Equal(t, model.ID, selected.VLMConfig.ModelID)

	queue := &visionIntegrationQueue{}
	fileSvc := files.NewLocalFileService(t.TempDir(), "")
	processingModels := visionIntegrationModels{ModelService: models}
	chunkService := NewChunkService(chunkRepo, knowledgeRepo, kbRepo, processingModels, nil)
	retrieval := &retriever.PostgresRetrievalService{RetrieveEngineService: &parentChildRetrieveEngine{}}
	svc := &knowledgeService{
		db: db, repo: knowledgeRepo, kbService: kbService, chunkRepo: chunkRepo, chunkService: chunkService,
		tenantRepo: tenantRepo, modelService: processingModels, fileSvc: fileSvc, task: queue,
		imageResolver: docparser.NewImageResolver(), retrieveEngine: retrieval,
	}
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 200, A: 255})
	var pngBytes bytes.Buffer
	require.NoError(t, png.Encode(&pngBytes, picture))
	knowledge, err := svc.CreateKnowledgeFromFile(ctx, kb.ID, newMultipartFileHeader(t, "vision.png", pngBytes.String()), map[string]string{
		"author": "Image fixture author", "original_url": "https://example.com/vision.png", "published_at": "2026-10-01",
	}, nil, "", "", nil)
	require.NoError(t, err)
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeDocumentProcess, queue.tasks[0].Type())
	require.NoError(t, svc.ProcessDocument(ctx, queue.tasks[0]))
	require.Len(t, queue.tasks, 2)
	require.Equal(t, types.TypeImageMultimodal, queue.tasks[1].Type())
	imageService := &ImageMultimodalService{
		chunkService: chunkService, modelService: processingModels, kbService: kbService, knowledgeRepo: knowledgeRepo,
		tenantRepo: tenantRepo, retrieveEngine: retrieval, taskEnqueuer: queue, fileSvc: fileSvc,
	}
	require.NoError(t, imageService.Handle(ctx, queue.tasks[1]))

	var chunks []types.Chunk
	require.NoError(t, db.Where("knowledge_id = ? AND chunk_type IN ?", knowledge.ID, []string{types.ChunkTypeImageOCR, types.ChunkTypeImageCaption}).Find(&chunks).Error)
	require.Len(t, chunks, 2)
	contents := map[string]string{}
	for _, chunk := range chunks {
		contents[chunk.ChunkType] = chunk.Content
		require.NotEmpty(t, chunk.ParentChunkID)
		require.Equal(t, int(types.ChunkStatusIndexed), chunk.Status)
		var info []types.ImageInfo
		require.NoError(t, json.Unmarshal([]byte(chunk.ImageInfo), &info))
		require.Len(t, info, 1)
		require.NotEmpty(t, info[0].OCRText)
		require.NotEmpty(t, info[0].Caption)
	}
	require.Equal(t, "测试图中文字：Reliquary 05", contents[types.ChunkTypeImageOCR])
	require.Equal(t, "图片显示一张包含 Reliquary 05 文字的测试卡片。", contents[types.ChunkTypeImageCaption])
	requestsMu.Lock()
	defer requestsMu.Unlock()
	require.Len(t, requests, 2)
	for _, request := range requests {
		require.NoError(t, request.decodeErr)
		require.Equal(t, model.Name, request.model)
		require.Equal(t, "/v1/chat/completions", request.path)
		require.Equal(t, "Bearer vision-test-key", request.apiKey)
		require.Equal(t, "conversation-image-processing", request.customValue)
		require.Equal(t, 2, request.parts)
		require.NotEmpty(t, request.prompt)
		require.True(t, strings.HasPrefix(request.imageURL, "data:image/png;base64,"))
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(request.imageURL, "data:image/png;base64,"))
		require.NoError(t, err)
		require.Equal(t, pngBytes.Bytes(), decoded)
	}
	t.Logf("KnowledgeQA model %s produced image_ocr and image_caption using two image-bearing requests", model.ID)
}
