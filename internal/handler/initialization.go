package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/ollama/ollama/api"

	"reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/rerank"
	"reliquary/internal/models/utils/ollama"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// DownloadTask 下载任务信息
type DownloadTask struct {
	ID        string     `json:"id"`
	ModelName string     `json:"modelName"`
	Status    string     `json:"status"` // pending, downloading, completed, failed
	Progress  float64    `json:"progress"`
	Message   string     `json:"message"`
	StartTime time.Time  `json:"startTime"`
	EndTime   *time.Time `json:"endTime,omitempty"`
}

// 全局下载任务管理器
var (
	downloadTasks = make(map[string]*DownloadTask)
	tasksMutex    sync.RWMutex
)

// InitializationHandler 初始化处理器
type InitializationHandler struct {
	modelService  interfaces.ModelService
	ollamaService *ollama.OllamaService
	pooler        embedding.EmbedderPooler
}

// NewInitializationHandler 创建初始化处理器
func NewInitializationHandler(
	modelService interfaces.ModelService,
	ollamaService *ollama.OllamaService,
	pooler embedding.EmbedderPooler,
) *InitializationHandler {
	return &InitializationHandler{
		modelService:  modelService,
		ollamaService: ollamaService,
		pooler:        pooler,
	}
}

// CheckOllamaStatus godoc
func (h *InitializationHandler) CheckOllamaStatus(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking Ollama service status")

	// Determine Ollama base URL for display
	baseURL := os.Getenv("OLLAMA_BASE_URL")
	if baseURL == "" {
		baseURL = "http://host.docker.internal:11434"
	}

	// 检查Ollama服务是否可用
	err := h.ollamaService.StartService(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"available": false,
				"error":     err.Error(),
				"baseUrl":   baseURL,
			},
		})
		return
	}

	version, err := h.ollamaService.GetVersion(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		version = "unknown"
	}

	logger.Info(ctx, "Ollama service is available")
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": h.ollamaService.IsAvailable(),
			"version":   version,
			"baseUrl":   baseURL,
		},
	})
}

// CheckOllamaModels godoc
func (h *InitializationHandler) CheckOllamaModels(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking Ollama models status")

	var req struct {
		Models []string `json:"models" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse models check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	// 检查Ollama服务是否可用
	if !h.ollamaService.IsAvailable() {
		err := h.ollamaService.StartService(ctx)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Ollama服务不可用: " + err.Error()))
			return
		}
	}

	modelStatus := make(map[string]bool)

	// 检查每个模型是否存在
	for _, modelName := range req.Models {
		available, err := h.ollamaService.IsModelAvailable(ctx, modelName)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_name": modelName,
			})
			modelStatus[modelName] = false
		} else {
			modelStatus[modelName] = available
		}

		logger.Infof(ctx, "Model %s availability: %v", utils.SanitizeForLog(modelName), modelStatus[modelName])
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": modelStatus,
		},
	})
}

// DownloadOllamaModel godoc
func (h *InitializationHandler) DownloadOllamaModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Starting async Ollama model download")

	var req struct {
		ModelName string `json:"modelName" binding:"required"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse model download request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	// 检查Ollama服务是否可用
	if !h.ollamaService.IsAvailable() {
		err := h.ollamaService.StartService(ctx)
		if err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Ollama服务不可用: " + err.Error()))
			return
		}
	}

	// 检查模型是否已存在
	available, err := h.ollamaService.IsModelAvailable(ctx, req.ModelName)
	if err != nil {
		c.Error(errors.NewInternalServerError("检查模型状态失败: " + err.Error()))
		return
	}

	if available {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "模型已存在",
			"data": gin.H{
				"modelName": req.ModelName,
				"status":    "completed",
				"progress":  100.0,
			},
		})
		return
	}

	// 检查是否已有相同模型的下载任务
	tasksMutex.RLock()
	for _, task := range downloadTasks {
		if task.ModelName == req.ModelName && (task.Status == "pending" || task.Status == "downloading") {
			tasksMutex.RUnlock()
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"message": "模型下载任务已存在",
				"data": gin.H{
					"taskId":    task.ID,
					"modelName": task.ModelName,
					"status":    task.Status,
					"progress":  task.Progress,
				},
			})
			return
		}
	}
	tasksMutex.RUnlock()

	// 创建下载任务
	taskID := uuid.New().String()
	task := &DownloadTask{
		ID:        taskID,
		ModelName: req.ModelName,
		Status:    "pending",
		Progress:  0.0,
		Message:   "准备下载",
		StartTime: time.Now(),
	}

	tasksMutex.Lock()
	downloadTasks[taskID] = task
	tasksMutex.Unlock()

	// 启动异步下载
	newCtx, cancel := context.WithTimeout(context.Background(), 12*time.Hour)
	go func() {
		defer cancel()
		h.downloadModelAsync(newCtx, taskID, req.ModelName)
	}()

	logger.Infof(ctx, "Created download task for model, task ID: %s", taskID)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "模型下载任务已创建",
		"data": gin.H{
			"taskId":    taskID,
			"modelName": req.ModelName,
			"status":    "pending",
			"progress":  0.0,
		},
	})
}

// GetDownloadProgress godoc
func (h *InitializationHandler) GetDownloadProgress(c *gin.Context) {
	taskID := c.Param("taskId")

	if taskID == "" {
		c.Error(errors.NewBadRequestError("任务ID不能为空"))
		return
	}

	tasksMutex.RLock()
	task, exists := downloadTasks[taskID]
	tasksMutex.RUnlock()

	if !exists {
		c.Error(errors.NewNotFoundError("下载任务不存在"))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    task,
	})
}

// ListDownloadTasks godoc
func (h *InitializationHandler) ListDownloadTasks(c *gin.Context) {
	tasksMutex.RLock()
	tasks := make([]*DownloadTask, 0, len(downloadTasks))
	for _, task := range downloadTasks {
		tasks = append(tasks, task)
	}
	tasksMutex.RUnlock()

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    tasks,
	})
}

// ListOllamaModels godoc
func (h *InitializationHandler) ListOllamaModels(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Listing installed Ollama models")

	// 确保服务可用
	if !h.ollamaService.IsAvailable() {
		if err := h.ollamaService.StartService(ctx); err != nil {
			logger.ErrorWithFields(ctx, err, nil)
			c.Error(errors.NewInternalServerError("Ollama服务不可用: " + err.Error()))
			return
		}
	}

	// 使用 ListModelsDetailed 获取包含大小等详细信息的模型列表
	models, err := h.ollamaService.ListModelsDetailed(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("获取模型列表失败: " + err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"models": models,
		},
	})
}

// downloadModelAsync 异步下载模型
func (h *InitializationHandler) downloadModelAsync(ctx context.Context,
	taskID, modelName string,
) {
	logger.Infof(ctx, "Starting async download for model, task: %s", taskID)

	// 更新任务状态为下载中
	h.updateTaskStatus(taskID, "downloading", 0.0, "开始下载模型")

	// 执行下载，带进度回调
	err := h.pullModelWithProgress(ctx, modelName, func(progress float64, message string) {
		h.updateTaskStatus(taskID, "downloading", progress, message)
	})
	if err != nil {
		logger.Error(ctx, "Failed to download model", err)
		h.updateTaskStatus(taskID, "failed", 0.0, fmt.Sprintf("下载失败: %v", err))
		return
	}

	// 下载成功
	logger.Infof(ctx, "Model downloaded successfully, task: %s", taskID)
	h.updateTaskStatus(taskID, "completed", 100.0, "下载完成")
}

// pullModelWithProgress 下载模型并提供进度回调
func (h *InitializationHandler) pullModelWithProgress(ctx context.Context,
	modelName string,
	progressCallback func(float64, string),
) error {
	// 检查服务是否可用
	if err := h.ollamaService.StartService(ctx); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return err
	}

	// 检查模型是否已存在
	available, err := h.ollamaService.IsModelAvailable(ctx, modelName)
	if err != nil {
		logger.Error(ctx, "Failed to check model availability", err)
		return err
	}
	if available {
		progressCallback(100.0, "模型已存在")
		return nil
	}

	// 创建下载请求
	pullReq := &api.PullRequest{
		Name: modelName,
	}

	// 使用Ollama客户端的Pull方法，带进度回调
	err = h.ollamaService.GetClient().Pull(ctx, pullReq, func(progress api.ProgressResponse) error {
		progressPercent := 0.0
		message := "下载中"

		if progress.Total > 0 && progress.Completed > 0 {
			progressPercent = float64(progress.Completed) / float64(progress.Total) * 100
			message = fmt.Sprintf("下载中: %.1f%% (%s)", progressPercent, progress.Status)
		} else if progress.Status != "" {
			message = progress.Status
		}

		// 调用进度回调
		progressCallback(progressPercent, message)

		logger.Infof(ctx,
			"Download progress: %.2f%% - %s", progressPercent, message,
		)
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to pull model: %w", err)
	}

	return nil
}

// updateTaskStatus 更新任务状态
func (h *InitializationHandler) updateTaskStatus(
	taskID, status string, progress float64, message string,
) {
	tasksMutex.Lock()
	defer tasksMutex.Unlock()

	if task, exists := downloadTasks[taskID]; exists {
		task.Status = status
		task.Progress = progress
		task.Message = message

		if status == "completed" || status == "failed" {
			now := time.Now()
			task.EndTime = &now
		}
	}
}

// GetCurrentConfigByKB godoc
// ModelTestRequest 统一的"测试连接"请求体。
//
// 三种模型（chat/embedding/rerank）的测试接口共享同一份结构，以便：
//   - 前端只需维护一份表单 → 后端映射。
//   - 后端可以直接把请求转成 *types.Model，再调用各包的 ConfigFromModel，
//     与生产路径（service.modelService.GetXxxModel）走完全相同的装配流程，
//     彻底消除过去每个测试端点手工拼 Config 的样板代码。
//
// 所有 provider/model 通用字段都在这里集中声明；若未来新增字段（比如现在的
// custom_headers），只需改一处，生产路径和测试路径会同时生效。
type ModelTestRequest struct {
	Source                    string            `json:"source"` // 为空时按需默认为 "remote"
	ModelName                 string            `json:"modelName" binding:"required"`
	BaseURL                   string            `json:"baseUrl"`
	APIKey                    string            `json:"apiKey"`
	Provider                  string            `json:"provider"`
	InterfaceType             string            `json:"interfaceType,omitempty"`
	Dimension                 int               `json:"dimension,omitempty"`
	SupportsDimensionOverride bool              `json:"supportsDimensionOverride,omitempty"`
	CustomHeaders             map[string]string `json:"customHeaders,omitempty"`
	ExtraConfig               map[string]string `json:"extraConfig,omitempty"`
	// AppSecret 保留模型凭据的存储与测试请求兼容。
	AppSecret string `json:"appSecret,omitempty"`
	// ModelID, when set, instructs the handler to substitute any missing
	// secrets (APIKey, AppSecret via ExtraConfig) from the stored model
	// record before assembling the test client. This lets the "Test
	// connection" button work on existing models without making the
	// frontend reload — and ship — the plaintext API key. Other fields
	// (BaseURL, ModelName, etc.) on this request still override the
	// stored values, so a user can validate a new endpoint against the
	// existing credentials in one click.
	ModelID string `json:"modelId,omitempty"`
}

// fillSecretsFromStoredModel mutates req in place: if req.ModelID is set
// and a secret field on the request is empty, the corresponding value from
// the stored (and decrypted) model is copied in. Non-empty request values
// are always preferred — they represent the user actively typing a new key
// they want to verify. Missing or inaccessible model is treated as a no-op
// (the connection test will fail downstream with a clearer "missing apiKey"
// error than we could produce here).
func (h *InitializationHandler) fillSecretsFromStoredModel(ctx context.Context, req *ModelTestRequest) {
	if req == nil || req.ModelID == "" {
		return
	}
	if req.APIKey != "" && req.AppSecret != "" {
		return
	}
	stored, err := h.modelService.GetModelByID(ctx, req.ModelID)
	if err != nil || stored == nil {
		logger.Warnf(ctx, "test-connection: stored model %s not found, leaving secrets empty: %v",
			utils.SanitizeForLog(req.ModelID), err)
		return
	}
	if req.APIKey == "" {
		req.APIKey = stored.Parameters.APIKey
	}
	if req.AppSecret == "" {
		req.AppSecret = stored.Parameters.AppSecret
	}
}

// decryptModelAppSecret 解密模型 Parameters 中的 AppSecret（与 modelService 行为一致）。
func decryptModelAppSecret(encrypted string) string {
	if encrypted == "" {
		return encrypted
	}
	if key := utils.GetAESKey(); key != nil {
		if plain, err := utils.DecryptAESGCM(encrypted, key); err == nil {
			return plain
		}
	}
	return encrypted
}

// buildTestModel 把测试连接请求转成一个临时的 *types.Model（不落库），
// 供 ConfigFromModel 使用。source 为空时按 defaultSource 兜底（chat/rerank
// 默认 remote，embedding 会根据前端传入的 source 决定）。
func (h *InitializationHandler) buildTestModel(
	req *ModelTestRequest, modelType types.ModelType, defaultSource types.ModelSource,
) *types.Model {
	source := types.ModelSource(strings.ToLower(req.Source))
	if source == "" {
		source = defaultSource
	}
	return &types.Model{
		Name:   req.ModelName,
		Type:   modelType,
		Source: source,
		Parameters: types.ModelParameters{
			BaseURL:       req.BaseURL,
			APIKey:        req.APIKey,
			AppSecret:     req.AppSecret,
			Provider:      req.Provider,
			InterfaceType: req.InterfaceType,
			ExtraConfig:   req.ExtraConfig,
			CustomHeaders: req.CustomHeaders,
			EmbeddingParameters: types.EmbeddingParameters{
				Dimension:                 req.Dimension,
				TruncatePromptTokens:      256,
				SupportsDimensionOverride: req.SupportsDimensionOverride,
			},
		},
	}
}

// CheckRemoteModel godoc
func (h *InitializationHandler) CheckRemoteModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking remote model connection")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse remote model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required")
		c.Error(errors.NewBadRequestError("模型名称和Base URL不能为空"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for remote model BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeKnowledgeQA, types.ModelSourceRemote)
	available, message := h.checkChatModelConnection(ctx, model)

	logger.Infof(ctx, "Remote model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// TestEmbeddingModel godoc
func (h *InitializationHandler) TestEmbeddingModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Testing embedding model connectivity and functionality")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse embedding test request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)
	if req.Source == "" {
		req.Source = string(types.ModelSourceRemote)
	}

	if req.BaseURL != "" {
		if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
			logger.Warnf(ctx, "SSRF validation failed for embedding BaseURL: %v", err)
			c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
			return
		}
	}

	// 阿里云多模态 Embedding 模型暂不支持
	if strings.ToLower(req.Provider) == "aliyun" {
		modelNameLower := strings.ToLower(req.ModelName)
		if strings.Contains(modelNameLower, "vision") || strings.Contains(modelNameLower, "multimodal") {
			logger.Infof(ctx, "Aliyun multimodal embedding model not supported: %s", req.ModelName)
			c.JSON(http.StatusOK, gin.H{
				"success": true,
				"data": gin.H{
					"available": false,
					"message":   "阿里云多模态 Embedding 模型暂不支持，请使用纯文本 Embedding 模型（如 text-embedding-v4）",
					"dimension": 0,
				},
			})
			return
		}
	}

	model := h.buildTestModel(&req, types.ModelTypeEmbedding, types.ModelSourceRemote)
	emb, err := embedding.NewEmbedder(embedding.ConfigFromModel(model), h.pooler, h.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"model": utils.SanitizeForLog(req.ModelName)})
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{`available`: false, `message`: fmt.Sprintf("创建Embedder失败: %v", err), `dimension`: 0},
		})
		return
	}

	vec, err := emb.Embed(ctx, "hello")
	if err != nil {
		logger.Error(ctx, "Failed to call embedder", err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data":    gin.H{`available`: false, `message`: fmt.Sprintf("调用Embedding失败: %v", err), `dimension`: 0},
		})
		return
	}

	logger.Infof(ctx, "Embedding test succeeded, dimension: %d", len(vec))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    gin.H{`available`: true, `message`: fmt.Sprintf("测试成功，向量维度=%d", len(vec)), `dimension`: len(vec)},
	})
}

// classifyConnectionError maps an upstream error string to a short
// human-readable hint in Chinese. Callers should always combine the hint
// with the raw error message (e.g. fmt.Sprintf("%s：%v", hint, err)) so
// the operator can still see what URL / response body the SDK actually
// got — the hint is for "where to start looking", the raw error is for
// "what actually happened".
func classifyConnectionError(errMsg string) string {
	switch {
	case strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthorized"):
		return "认证失败，请检查API Key"
	case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "forbidden"):
		return "权限不足，请检查API Key权限"
	case strings.Contains(errMsg, "404") || strings.Contains(errMsg, "not found"):
		return "API端点不存在，请检查Base URL"
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "context deadline exceeded"):
		return "连接超时，请检查网络连接"
	case strings.Contains(errMsg, "connection refused") || strings.Contains(errMsg, "no such host") || strings.Contains(errMsg, "dial tcp"):
		return "无法连接到服务器，请检查Base URL"
	default:
		return "连接失败"
	}
}

// checkChatModelConnection 使用 chat 模块做一次最小化调用来测试连通性与鉴权。
// 与生产路径走完全相同的 ConfigFromModel → NewChat 流程，因此 CustomHeaders、
// ExtraConfig、Provider 等字段都会被正确透传。
func (h *InitializationHandler) checkChatModelConnection(
	ctx context.Context, model *types.Model,
) (bool, string) {
	chatInstance, err := chat.NewChat(chat.ConfigFromModel(model), h.ollamaService)
	if err != nil {
		return false, fmt.Sprintf("创建聊天实例失败: %v", err)
	}

	testMessages := []chat.Message{{Role: "user", Content: "test"}}
	testOptions := &chat.ChatOptions{
		MaxTokens: 1,
		Thinking:  &[]bool{false}[0], // for dashscope.aliyuncs qwen3-32b
	}

	_, err = chatInstance.Chat(ctx, testMessages, testOptions)
	if err != nil {
		errMsg := err.Error()
		// 400 = endpoint reachable + auth ok, just a parameter mismatch
		// (e.g. max_tokens vs max_completion_tokens). Treat as success.
		if strings.Contains(errMsg, "status code: 400") {
			return true, "连接正常，模型可用"
		}
		// For every other failure mode we surface a human-readable hint
		// AND the upstream error verbatim. Swallowing the underlying
		// message used to hide things like the actual URL the SDK
		// tried, response body, etc. — making remote debugging nearly
		// impossible. Format: "<hint>：<raw err>".
		return false, fmt.Sprintf("%s：%v", classifyConnectionError(errMsg), err)
	}

	// 连接成功，模型可用
	return true, "连接正常，模型可用"
}

// checkRerankModelConnection 使用 rerank 模块做一次最小化调用来测试连通性与鉴权。
// 与生产路径共用 ConfigFromModel，所有字段（CustomHeaders 等）都透传。
func (h *InitializationHandler) checkRerankModelConnection(
	ctx context.Context, model *types.Model,
) (bool, string) {
	reranker, err := rerank.NewReranker(rerank.ConfigFromModel(model, decryptModelAppSecret(model.Parameters.AppSecret)))
	if err != nil {
		return false, fmt.Sprintf("创建Reranker失败: %v", err)
	}

	results, err := reranker.Rerank(ctx, "ping", []string{"pong"})
	if err != nil {
		return false, fmt.Sprintf("重排测试失败: %v", err)
	}
	if len(results) > 0 {
		return true, fmt.Sprintf("重排功能正常，返回%d个结果", len(results))
	}
	return false, "重排接口连接成功，但未返回重排结果"
}

// CheckRerankModel godoc
func (h *InitializationHandler) CheckRerankModel(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Checking rerank model connection and functionality")

	var req ModelTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse rerank model check request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	h.fillSecretsFromStoredModel(ctx, &req)

	if req.ModelName == "" || req.BaseURL == "" {
		logger.Error(ctx, "Model name and base URL are required")
		c.Error(errors.NewBadRequestError("模型名称和Base URL不能为空"))
		return
	}

	if err := utils.ValidateURLForSSRF(req.BaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for rerank BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("Base URL", req.BaseURL, err)))
		return
	}

	model := h.buildTestModel(&req, types.ModelTypeRerank, types.ModelSourceRemote)
	available, message := h.checkRerankModelConnection(ctx, model)

	logger.Infof(ctx, "Rerank model check completed, available: %v, message: %s", available, message)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"available": available,
			"message":   message,
		},
	})
}

// TestMultimodalFunction godoc
func (h *InitializationHandler) TestMultimodalFunction(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Testing multimodal functionality")

	var req testMultimodalForm
	if err := c.ShouldBind(&req); err != nil {
		logger.Error(ctx, "Failed to parse form data", err)
		c.Error(errors.NewBadRequestError("表单参数解析失败"))
		return
	}
	// ollama 场景自动拼接 base url
	if req.VLMInterfaceType == "ollama" {
		req.VLMBaseURL = os.Getenv("OLLAMA_BASE_URL") + "/v1"
	}

	if req.VLMModel == "" || req.VLMBaseURL == "" {
		logger.Error(ctx, "VLM model name and base URL are required")
		c.Error(errors.NewBadRequestError("VLM模型名称和Base URL不能为空"))
		return
	}

	// SSRF validation for VLM BaseURL
	if err := utils.ValidateURLForSSRF(req.VLMBaseURL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for VLM BaseURL: %v", err)
		c.Error(errors.NewBadRequestError(utils.FormatSSRFError("VLM Base URL", req.VLMBaseURL, err)))
		return
	}

	// 验证文件大小 — MAX_FILE_SIZE_MB env (50MB 默认)。
	// 见 utils/filesize.go 注释：故意保留为部署期 env，不做 runtime setting。
	maxSizeMB := utils.GetMaxFileSizeMB()
	maxSize := maxSizeMB * 1024 * 1024
	// 上限必须在 multipart 解析之前生效：FormFile 会先把整个 body 缓冲下来
	// （超出内存部分落临时文件），之后的 header.Size 检查看到的已是收完的上传。
	// nginx 的 location /api/ 仍是 MAX_FILE_SIZE；这里是直连 app 时的同一道上限。
	limitUploadBody(c, maxSize)

	// 获取上传的图片文件
	file, header, err := c.Request.FormFile("image")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			logger.Error(ctx, "File size too large")
			c.Error(errors.NewBadRequestError(fmt.Sprintf("图片文件大小不能超过%dMB", maxSizeMB)))
			return
		}
		logger.Error(ctx, "Failed to get uploaded image", err)
		c.Error(errors.NewBadRequestError("获取上传图片失败"))
		return
	}
	defer file.Close()

	// 验证文件类型
	if !strings.HasPrefix(header.Header.Get("Content-Type"), "image/") {
		logger.Error(ctx, "Invalid file type, only images are allowed")
		c.Error(errors.NewBadRequestError("只允许上传图片文件"))
		return
	}

	if header.Size > maxSize {
		logger.Error(ctx, "File size too large")
		c.Error(errors.NewBadRequestError(fmt.Sprintf("图片文件大小不能超过%dMB", maxSizeMB)))
		return
	}
	logger.Infof(ctx, "Processing image: %s", utils.SanitizeForLog(header.Filename))

	// 解析文档分割配置
	chunkSizeInt32, err := strconv.ParseInt(req.ChunkSize, 10, 32)
	if err != nil {
		logger.Error(ctx, "Failed to parse chunk size", err)
		c.Error(errors.NewBadRequestError("Failed to parse chunk size"))
		return
	}
	chunkSize := int32(chunkSizeInt32)
	if chunkSize < 100 || chunkSize > 10000 {
		chunkSize = 1000
	}

	chunkOverlapInt32, err := strconv.ParseInt(req.ChunkOverlap, 10, 32)
	if err != nil {
		logger.Error(ctx, "Failed to parse chunk overlap", err)
		c.Error(errors.NewBadRequestError("Failed to parse chunk overlap"))
		return
	}
	chunkOverlap := int32(chunkOverlapInt32)
	if chunkOverlap < 0 || chunkOverlap >= chunkSize {
		chunkOverlap = 200
	}

	var separators []string
	if req.SeparatorsRaw != "" {
		if err := json.Unmarshal([]byte(req.SeparatorsRaw), &separators); err != nil {
			separators = []string{"\n\n", "\n", "。", "！", "？", ";", "；"}
		}
	} else {
		separators = []string{"\n\n", "\n", "。", "！", "？", ";", "；"}
	}

	// 读取图片文件内容
	imageContent, err := io.ReadAll(file)
	if err != nil {
		logger.Error(ctx, "Failed to read image file", err)
		c.Error(errors.NewBadRequestError("读取图片文件失败"))
		return
	}

	// 调用多模态测试
	startTime := time.Now()
	result, err := h.testMultimodalWithParser(
		ctx,
		imageContent, header.Filename,
		chunkSize, chunkOverlap, separators, &req,
	)
	processingTime := time.Since(startTime).Milliseconds()

	if err != nil {
		logger.Error(ctx, "Failed to test multimodal", err)
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"success":         false,
				"message":         err.Error(),
				"processing_time": processingTime,
			},
		})
		return
	}

	logger.Infof(ctx, "Multimodal test completed successfully in %dms", processingTime)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"success":         true,
			"caption":         result["caption"],
			"ocr":             result["ocr"],
			"processing_time": processingTime,
		},
	})
}

// testMultimodalWithParser uses the process-local image reader,
// then returns basic information about the result.
func (h *InitializationHandler) testMultimodalWithParser(
	ctx context.Context,
	imageContent []byte, filename string,
	chunkSize, chunkOverlap int32, separators []string,
	req *testMultimodalForm,
) (map[string]string, error) {
	fileExt := ""
	if idx := strings.LastIndex(filename, "."); idx != -1 {
		fileExt = strings.ToLower(filename[idx+1:])
	}

	requestID, _ := types.RequestIDFromContext(ctx)

	readResult, err := (&docparser.SimpleFormatReader{}).Read(ctx, &types.ReadRequest{
		FileContent: imageContent,
		FileName:    filename,
		FileType:    fileExt,
		RequestID:   requestID,
	})
	if err != nil {
		return nil, fmt.Errorf("图片解析失败: %v", err)
	}
	if readResult.Error != "" {
		return nil, fmt.Errorf("图片解析返回错误: %s", readResult.Error)
	}

	result := map[string]string{
		"markdown": readResult.MarkdownContent,
		"caption":  "",
		"ocr":      "",
	}
	return result, nil
}

// 使用结构体解析表单数据
type testMultimodalForm struct {
	VLMModel         string `form:"vlm_model"`
	VLMBaseURL       string `form:"vlm_base_url"`
	VLMAPIKey        string `form:"vlm_api_key"`
	VLMInterfaceType string `form:"vlm_interface_type"`

	COSSecretID   string `form:"cos_secret_id"`
	COSSecretKey  string `form:"cos_secret_key"`
	COSRegion     string `form:"cos_region"`
	COSBucketName string `form:"cos_bucket_name"`
	COSAppID      string `form:"cos_app_id"`
	COSPathPrefix string `form:"cos_path_prefix"`

	// 文档切分配置（字符串后续自行解析，以避免类型绑定失败）
	ChunkSize     string `form:"chunk_size"`
	ChunkOverlap  string `form:"chunk_overlap"`
	SeparatorsRaw string `form:"separators"`
}
