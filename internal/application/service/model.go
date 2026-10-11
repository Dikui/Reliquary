package service

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"net/http"
	"reliquary/internal/chatgpt"
	"strings"

	"reliquary/internal/application/repository"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/rerank"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
	"reliquary/internal/utils/ollama"
)

// ErrModelNotFound is returned when a model cannot be found in the repository
var ErrModelNotFound = errors.New("model not found")

// modelService implements the model service interface
type modelService struct {
	chatGPT                *chatgpt.Manager
	chatGPTPolicy          *ChatGPTPolicyService
	chatGPTHTTPClient      *http.Client
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
	if err := s.validateChatGPTModel(ctx, model); err != nil {
		return err
	}
	if err := validateImageEmbeddingModel(model); err != nil {
		return err
	}
	logger.Infof(ctx, "Creating model: %s, type: %s, source: %s", model.Name, model.Type, model.Source)

	// Handle remote models (e.g., OpenAI)
	if model.Source == types.ModelSourceRemote || model.Source == types.ModelSourceChatGPT {
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
	// A migration-paused model remains editable so the user can restore its
	// credentials and custom headers. Runtime adapter getters reject it below.
	if model.Status == types.ModelStatusNeedsConfiguration {
		if changed, err := s.restoreMaintenanceModel(ctx, model); err != nil {
			return nil, err
		} else if changed {
			if err := s.repo.Update(ctx, model); err != nil {
				return nil, err
			}
		}
		return model, nil
	}

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
	for _, model := range models {
		if model.Source == types.ModelSourceChatGPT {
			if changed, err := s.restoreMaintenanceModel(ctx, model); err != nil {
				return nil, err
			} else if changed {
				if err := s.repo.Update(ctx, model); err != nil {
					return nil, err
				}
			}
		}
	}
	return models, nil
}

// UpdateModel updates an existing model in the repository
func (s *modelService) UpdateModel(ctx context.Context, model *types.Model) error {
	if s.nameEmbeddingRebuilder != nil && s.db != nil && !entityDatabaseInTransaction(s.db) {
		return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			copy := *s
			copy.db = tx
			copy.repo = repository.NewModelRepository(tx)
			copy.kbRepo = repository.NewKnowledgeBaseRepository(tx)
			bound, err := s.nameEmbeddingRebuilder.BindTransaction(ctx, tx)
			if err != nil {
				return err
			}
			copy.nameEmbeddingRebuilder = bound
			return copy.UpdateModel(ctx, model)
		})
	}

	if model.Type != "" && !model.Type.IsSupported() {
		return apperrors.NewValidationError(fmt.Sprintf("不支持的模型类型: %s", model.Type))
	}
	if err := s.validateChatGPTModel(ctx, model); err != nil {
		return err
	}
	if err := validateImageEmbeddingModel(model); err != nil {
		return err
	}
	if model.Source != types.ModelSourceLocal || model.Type != types.ModelTypeKnowledgeQA || model.Status != types.ModelStatusActive {
		inUse, err := s.chatGPTFallbackInUse(ctx, model.ID)
		if err != nil {
			return err
		}
		if inUse {
			return apperrors.NewBadRequestError("remove the ChatGPT fallback selection before changing this model's source, type or active status")
		}
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
	preserveMaintenanceProvenance(model, existingModel)
	if existingModel != nil && existingModel.Status == types.ModelStatusNeedsConfiguration {
		model.Status = types.ModelStatusNeedsConfiguration
		if model.Parameters.ExtraConfig == nil {
			model.Parameters.ExtraConfig = map[string]string{}
		}
		model.Parameters.ExtraConfig[maintenanceRequiredFields] = existingModel.Parameters.ExtraConfig[maintenanceRequiredFields]
		if _, err := s.restoreMaintenanceModel(ctx, model); err != nil {
			return err
		}
	}

	// Configuration and vector invalidation commit together. A model ID can
	// retain its dimensions while pointing at completely different weights.
	if s.db != nil {
		err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			var current types.Model
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", model.ID, model.TenantID).First(&current).Error; err != nil {
				return err
			}
			changed := (current.Type == types.ModelTypeEmbedding || model.Type == types.ModelTypeEmbedding) &&
				entityEmbeddingProfileHash(current.ID, current.Parameters.EmbeddingParameters.Dimension, &current) != entityEmbeddingProfileHash(model.ID, model.Parameters.EmbeddingParameters.Dimension, model)
			if err := tx.Model(&types.Model{}).Where("id=? AND tenant_id=?", model.ID, model.TenantID).Select("*").Updates(model).Error; err != nil {
				return err
			}
			if !changed {
				return nil
			}
			paused, err := maintenanceops.SnapshotPaused(ctx, tx)
			if err != nil {
				return err
			}
			if paused {
				return nil
			}
			return types.RetireEmbeddingVectors(ctx, tx, model.ID)
		})
	} else {
		err = s.repo.Update(ctx, model)
	}
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
			return fmt.Errorf("model update could not resolve name embedding profile: %w", resolveErr)
		}
		if profileModel == model.ID {
			if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, types.SingleTenantID, "embedding_model_updated"); err != nil {
				return fmt.Errorf("model update could not enqueue name embedding rebuild: %w", err)
			}
		}
	}
	return nil
}

func validateImageEmbeddingModel(model *types.Model) error {
	parameters := model.Parameters.EmbeddingParameters
	if !parameters.SupportsImages {
		return nil
	}
	if model.Type != types.ModelTypeEmbedding || model.Source != types.ModelSourceRemote {
		return apperrors.NewValidationError("image embedding requires a remote HTTP embedding model")
	}
	if format := strings.TrimSpace(parameters.MultimodalFormat); format != "" && format != "jina" {
		return apperrors.NewValidationError("multimodal_format must be jina")
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
	if existing.Source == types.ModelSourceChatGPT {
		return nil, apperrors.NewBadRequestError("Use ChatGPT sign-in to manage this model's credentials")
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
	if _, err := s.restoreMaintenanceModel(ctx, existing); err != nil {
		return nil, err
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
	if inUse, err := s.chatGPTFallbackInUse(ctx, id); err != nil {
		return err
	} else if inUse {
		return apperrors.NewBadRequestError("remove the ChatGPT fallback selection before deleting this model")
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
	if model.Status == types.ModelStatusNeedsConfiguration {
		return nil, apperrors.NewServiceUnavailableError("This model is paused until its migration credentials are configured.")
	}

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
	if model.Status == types.ModelStatusNeedsConfiguration {
		return nil, apperrors.NewServiceUnavailableError("This model is paused until its migration credentials are configured.")
	}

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
	if model.Status == types.ModelStatusNeedsConfiguration {
		if changed, err := s.restoreMaintenanceModel(ctx, model); err != nil {
			return nil, err
		} else if changed {
			if err := s.repo.Update(ctx, model); err != nil {
				return nil, err
			}
		}
		if model.Status == types.ModelStatusNeedsConfiguration {
			return nil, apperrors.NewServiceUnavailableError("This model is paused until its migration credentials are configured.")
		}
	}

	if model.Source == types.ModelSourceChatGPT {
		return s.chatGPTModel(ctx, model)
	}
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
