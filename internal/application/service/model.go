package service

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"strconv"
	"strings"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/rerank"
	"reliquary/internal/models/utils/ollama"
	"reliquary/internal/models/vlm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// ErrModelNotFound is returned when a model cannot be found in the repository
var ErrModelNotFound = errors.New("model not found")

// modelService implements the model service interface
type modelService struct {
	db                     *gorm.DB
	repo                   interfaces.ModelRepository
	kbRepo                 interfaces.KnowledgeBaseRepository
	ollamaService          *ollama.OllamaService
	pooler                 embedding.EmbedderPooler
	tenantService          interfaces.TenantService
	nameEmbeddingRebuilder EntityNameEmbeddingRebuildTrigger
}

func NewModelServiceWithRoutingDB(repo interfaces.ModelRepository, kbRepo interfaces.KnowledgeBaseRepository, ollamaService *ollama.OllamaService, pooler embedding.EmbedderPooler, tenantService interfaces.TenantService, db *gorm.DB) interfaces.ModelService {
	s := NewModelService(repo, kbRepo, ollamaService, pooler, tenantService).(*modelService)
	s.db = db
	return s
}

// NewModelService creates a new model service instance
func NewModelService(repo interfaces.ModelRepository,
	kbRepo interfaces.KnowledgeBaseRepository,
	ollamaService *ollama.OllamaService,
	pooler embedding.EmbedderPooler,
	tenantService interfaces.TenantService,
) interfaces.ModelService {
	return &modelService{
		repo:          repo,
		kbRepo:        kbRepo,
		ollamaService: ollamaService,
		pooler:        pooler,
		tenantService: tenantService,
	}
}

// decryptAppSecret 解密 AppSecret（如果为空或 cryptoSvc 为空则原样返回）
func (s *modelService) decryptAppSecret(encrypted string) string {
	if encrypted == "" {
		return encrypted
	}
	if key := utils.GetAESKey(); key != nil {
		if encrypted, err := utils.DecryptAESGCM(encrypted, key); err == nil {
			return encrypted
		}
	}
	return encrypted
}

// CreateModel creates a new model in the repository
// For local models, it initiates an asynchronous download process
// Remote models are immediately set to active status
func (s *modelService) CreateModel(ctx context.Context, model *types.Model) error {
	if !model.Type.IsSupported() {
		return apperrors.NewValidationError(fmt.Sprintf("不支持的模型类型: %s", model.Type))
	}
	logger.Infof(ctx, "Creating model: %s, type: %s, source: %s", model.Name, model.Type, model.Source)

	// Handle remote models (e.g., OpenAI)
	if model.Source == types.ModelSourceRemote {
		logger.Info(ctx, "Remote model detected, setting status to active")
		model.Status = types.ModelStatusActive

		logger.Info(ctx, "Saving remote model to repository")
		err := s.repo.Create(ctx, model)
		if err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"model_name": model.Name,
				"model_type": model.Type,
			})
			return err
		}

		logger.Infof(ctx, "Remote model created successfully: %s", model.ID)
		return nil
	}

	// Handle local models (e.g., Ollama)
	logger.Info(ctx, "Local model detected, setting status to downloading")
	model.Status = types.ModelStatusDownloading

	logger.Info(ctx, "Saving local model to repository")
	err := s.repo.Create(ctx, model)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_name": model.Name,
			"model_type": model.Type,
		})
		return err
	}

	// Start asynchronous model download
	logger.Infof(ctx, "Starting background download for model: %s", model.Name)
	newCtx := logger.CloneContext(ctx)
	go func() {
		logger.Info(newCtx, "Background download started")
		err := s.ollamaService.PullModel(newCtx, model.Name)
		if err != nil {
			logger.ErrorWithFields(newCtx, err, map[string]interface{}{
				"model_name": model.Name,
			})
			model.Status = types.ModelStatusDownloadFailed
		} else {
			logger.Infof(newCtx, "Model download completed successfully: %s", model.Name)
			model.Status = types.ModelStatusActive
		}
		logger.Infof(newCtx, "Updating model status to: %s", model.Status)
		s.repo.Update(newCtx, model)
	}()

	logger.Infof(ctx, "Model creation initiated successfully: %s", model.ID)
	return nil
}

// GetModelByID retrieves a model by its ID
// Returns an error if the model is not found or is in a non-active state
func (s *modelService) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	// Check if ID is empty
	if id == "" {
		logger.Error(ctx, "Model ID is empty")
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	// Fetch model from repository
	model, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  id,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	// Check if model exists
	if model == nil {
		logger.Error(ctx, "Model not found")
		return nil, ErrModelNotFound
	}

	logger.Infof(ctx, "Model found, name: %s, status: %s", model.Name, model.Status)

	// Check model status
	if model.Status == types.ModelStatusActive {
		return model, nil
	}

	if model.Status == types.ModelStatusDownloading {
		logger.Warn(ctx, "Model is currently downloading")
		return nil, errors.New("model is currently downloading")
	}

	if model.Status == types.ModelStatusDownloadFailed {
		logger.Error(ctx, "Model download failed")
		return nil, errors.New("model download failed")
	}

	logger.Error(ctx, "Model status is abnormal")
	return nil, errors.New("abnormal model status")
}

// ListModels returns all models belonging to the tenant
func (s *modelService) ListModels(ctx context.Context) ([]*types.Model, error) {
	logger.Info(ctx, "Start listing models")

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Listing models for tenant ID: %d", tenantID)

	// List models from repository with no additional filters
	models, err := s.repo.List(ctx, tenantID, "", "")
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Retrieved %d models successfully", len(models))
	return models, nil
}

// UpdateModel updates an existing model in the repository
func (s *modelService) UpdateModel(ctx context.Context, model *types.Model) error {
	if model.Type != "" && !model.Type.IsSupported() {
		return apperrors.NewValidationError(fmt.Sprintf("不支持的模型类型: %s", model.Type))
	}
	logger.Info(ctx, "Start updating model")
	logger.Infof(ctx, "Updating model ID: %s, name: %s", model.ID, model.Name)

	// Built-in model configuration can be changed by the authenticated user.
	tenantID := types.MustTenantIDFromContext(ctx)
	existingModel, err := s.repo.GetByID(ctx, tenantID, model.ID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": model.ID,
		})
		return err
	}
	if existingModel != nil && existingModel.IsBuiltin {
		if !types.IsAuthenticatedFromContext(ctx) {
			logger.Warnf(ctx, "Unauthenticated caller attempted to update builtin model: %s", model.ID)
			return apperrors.NewForbiddenError("an authenticated user is required to update builtin models")
		}
		// A UI edit is an explicit runtime override. Clear YAML ownership so
		// the startup reconciler does not silently replace the saved values.
		model.TenantID = existingModel.TenantID
		model.IsBuiltin = true
		model.ManagedBy = ""
	}

	// Update model in repository
	err = s.repo.Update(ctx, model)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return err
	}

	logger.Infof(ctx, "Model updated successfully: %s", model.ID)
	if s.nameEmbeddingRebuilder != nil && existingModel != nil &&
		(existingModel.Type == types.ModelTypeEmbedding || model.Type == types.ModelTypeEmbedding) &&
		entityEmbeddingProfileHash(existingModel.ID, existingModel.Parameters.EmbeddingParameters.Dimension, existingModel) != entityEmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, model) {
		profileModel, resolveErr := ResolveModelTier(ctx, s.db, nil, types.ModelTierEmbedding)
		if resolveErr != nil {
			return fmt.Errorf("model saved but name embedding profile could not be resolved: %w", resolveErr)
		}
		if profileModel == model.ID {
			if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, types.SingleTenantID, "embedding_model_updated"); err != nil {
				return fmt.Errorf("model saved but name embedding rebuild could not be queued: %w", err)
			}
		}
	}
	return nil
}

// UpdateModelCredentials writes one or more credential fields on the model's
// Parameters jsonb. Model clients read credentials from current configuration.
// Model clients
// are (each call to GetEmbeddingModel/GetChatModel rebuilds the client from
// the current Parameters), so no explicit cache invalidation is required —
// the next call will pick up the new credential automatically.
func (s *modelService) UpdateModelCredentials(
	ctx context.Context, id string, apiKey, appSecret *string,
) (*types.Model, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	existing, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrModelNotFound
	}
	if existing.IsBuiltin && !types.IsAuthenticatedFromContext(ctx) {
		return nil, apperrors.NewForbiddenError(
			"an authenticated user is required to modify builtin model credentials")
	}

	changed := false
	if apiKey != nil && *apiKey != "" && *apiKey != existing.Parameters.APIKey {
		existing.Parameters.APIKey = *apiKey
		changed = true
	}
	if appSecret != nil && *appSecret != "" && *appSecret != existing.Parameters.AppSecret {
		existing.Parameters.AppSecret = *appSecret
		changed = true
	}
	if !changed {
		return existing, nil
	}
	if existing.IsBuiltin {
		// Credential changes are also runtime overrides of YAML-managed data.
		existing.ManagedBy = ""
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	logger.Infof(ctx, "Model credentials updated: id=%s", id)
	return existing, nil
}

// ClearModelCredential removes a single credential field. Idempotent.
func (s *modelService) ClearModelCredential(ctx context.Context, id, field string) error {
	tenantID := types.MustTenantIDFromContext(ctx)
	existing, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrModelNotFound
	}
	if existing.IsBuiltin && !types.IsAuthenticatedFromContext(ctx) {
		return apperrors.NewForbiddenError(
			"an authenticated user is required to modify builtin model credentials")
	}

	changed := false
	switch field {
	case "api_key":
		if existing.Parameters.APIKey != "" {
			existing.Parameters.APIKey = ""
			changed = true
		}
	case "app_secret":
		if existing.Parameters.AppSecret != "" {
			existing.Parameters.AppSecret = ""
			changed = true
		}
	default:
		return errors.New("unknown credential field: " + field)
	}
	if !changed {
		return nil
	}
	if existing.IsBuiltin {
		existing.ManagedBy = ""
	}
	if err := s.repo.Update(ctx, existing); err != nil {
		return err
	}
	logger.Infof(ctx, "Model credential cleared by user: id=%s field=%s", id, field)
	return nil
}

// DeleteModel removes a model from the repository
func (s *modelService) DeleteModel(ctx context.Context, id string) error {
	logger.Info(ctx, "Start deleting model")
	logger.Infof(ctx, "Deleting model ID: %s", id)

	tenantID := types.MustTenantIDFromContext(ctx)
	logger.Infof(ctx, "Tenant ID: %d", tenantID)

	// Check if the model is builtin - builtin models cannot be deleted
	existingModel, err := s.repo.GetByID(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": id,
		})
		return err
	}
	if existingModel == nil {
		return ErrModelNotFound
	}
	if existingModel.IsBuiltin {
		logger.Warnf(ctx, "Attempted to delete builtin model: %s", id)
		return apperrors.NewBadRequestError("builtin models cannot be deleted")
	}

	usage, err := s.getModelUsageDetails(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": id,
		})
		return err
	}
	if usage.InUse() {
		kbCount := usage.KnowledgeBaseTotal
		if kbCount == 0 {
			kbCount = int64(len(usage.KnowledgeBases))
		}
		logger.Warnf(ctx, "Model %s is in use: kb=%d", id, kbCount)
		return apperrors.NewModelInUseError(
			formatModelInUseMessage(kbCount),
			usage,
		)
	}

	// Delete model from repository
	err = s.repo.Delete(ctx, tenantID, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  id,
			"tenant_id": tenantID,
		})
		return err
	}

	logger.Infof(ctx, "Model deleted successfully: %s", id)
	return nil
}

func (s *modelService) getModelUsageDetails(
	ctx context.Context, tenantID uint64, modelID string,
) (types.ModelUsageDetails, error) {
	details := types.ModelUsageDetails{
		ModelTiers:     []types.ModelTierUsageResource{},
		KnowledgeBases: make([]types.ModelUsageResource, 0),
	}

	kbCount, err := s.kbRepo.CountByModelID(ctx, tenantID, modelID)
	if err != nil {
		return details, err
	}
	details.KnowledgeBaseTotal = kbCount
	if kbCount > 0 {
		details.KnowledgeBases, err = s.kbRepo.ListModelUsages(ctx, tenantID, modelID)
		if err != nil {
			return details, err
		}
		if details.KnowledgeBases == nil {
			details.KnowledgeBases = make([]types.ModelUsageResource, 0)
		}
	}

	if s.tenantService == nil {
		return details, nil
	}
	tenant, err := s.tenantService.GetTenantByID(ctx, tenantID)
	if err != nil {
		return details, err
	}
	if tenant == nil {
		return details, nil
	}
	if bindings := tenant.ModelTiers.UsageBindings(modelID); len(bindings) > 0 {
		details.ModelTiers = append(details.ModelTiers, types.ModelTierUsageResource{Scope: "global", ID: strconv.FormatUint(tenantID, 10), Name: tenant.Name, Bindings: bindings})
	}
	return details, nil
}

// GetEmbeddingModel retrieves and initializes an embedding model instance
// Takes a model ID and returns an Embedder interface implementation
func (s *modelService) GetEmbeddingModel(ctx context.Context, modelId string) (embedding.Embedder, error) {
	// Get the model details
	model, err := s.GetModelByID(ctx, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": modelId,
		})
		return nil, err
	}

	logger.Infof(ctx, "Getting embedding model: %s, source: %s", model.Name, model.Source)

	embedder, err := embedding.NewEmbedder(embedding.ConfigFromModel(model), s.pooler, s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	logger.Info(ctx, "Embedding model initialized successfully")
	return embedder, nil
}

// GetRerankModel retrieves and initializes a reranking model instance
// Takes a model ID and returns a Reranker interface implementation
func (s *modelService) GetRerankModel(ctx context.Context, modelId string) (rerank.Reranker, error) {
	// Get the model details
	model, err := s.GetModelByID(ctx, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id": modelId,
		})
		return nil, err
	}

	logger.Infof(ctx, "Getting rerank model: %s, source: %s", model.Name, model.Source)

	reranker, err := rerank.NewReranker(rerank.ConfigFromModel(model, s.decryptAppSecret(model.Parameters.AppSecret)))
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	logger.Info(ctx, "Rerank model initialized successfully")
	return reranker, nil
}

// GetChatModel retrieves and initializes a chat model instance
// Takes a model ID and returns a Chat interface implementation
func (s *modelService) GetChatModel(ctx context.Context, modelId string) (chat.Chat, error) {
	// Check if model ID is empty
	if modelId == "" {
		logger.Error(ctx, "Model ID is empty")
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	// Get the model directly from repository to avoid status checks
	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		logger.Error(ctx, "Chat model not found")
		return nil, ErrModelNotFound
	}

	logger.Infof(ctx, "Getting chat model: %s, source: %s", model.Name, model.Source)

	chatModel, err := chat.NewChat(chat.ConfigFromModel(model), s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	return chatModel, nil
}

// GetVLMModel retrieves and initializes a vision language model instance.
func (s *modelService) GetVLMModel(ctx context.Context, modelId string) (vlm.VLM, error) {
	if modelId == "" {
		return nil, errors.New("model ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)

	model, err := s.repo.GetByID(ctx, tenantID, modelId)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":  modelId,
			"tenant_id": tenantID,
		})
		return nil, err
	}

	if model == nil {
		return nil, ErrModelNotFound
	}

	logger.Infof(ctx, "Getting VLM model: %s, source: %s", model.Name, model.Source)
	if err := validateImageProcessingModel(model); err != nil {
		return nil, err
	}

	vlmModel, err := vlm.NewVLM(vlm.ConfigFromModel(model), s.ollamaService)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"model_id":   model.ID,
			"model_name": model.Name,
		})
		return nil, err
	}

	return vlmModel, nil
}

func formatModelInUseMessage(kbCount int64) string {
	var parts []string
	if kbCount > 0 {
		parts = append(parts, fmt.Sprintf("%d knowledge base(s)", kbCount))
	}

	joined := strings.Join(parts, " and ")
	return fmt.Sprintf(
		"model is used by %s; reconfigure or remove those references before deleting",
		joined,
	)
}
