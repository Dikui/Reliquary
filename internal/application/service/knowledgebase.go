package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/application/repository"
	"reliquary/internal/datasource"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// ErrInvalidTenantID represents an error for invalid tenant ID
var ErrInvalidTenantID = errors.New("invalid tenant ID")

const kbTaskCleanupTimeout = 5 * time.Second

// knowledgeBaseService implements the knowledge base service interface
type knowledgeBaseService struct {
	db              *gorm.DB
	repo            interfaces.KnowledgeBaseRepository
	kgRepo          interfaces.KnowledgeRepository
	chunkRepo       interfaces.ChunkRepository
	modelService    interfaces.ModelService
	retrieveEngine  interfaces.RetrievalService
	tenantRepo      interfaces.TenantRepository
	fileSvc         interfaces.FileService
	asynqClient     interfaces.TaskEnqueuer
	taskInspector   interfaces.TaskInspector
	taskPendingRepo interfaces.TaskPendingOpsRepository
	dsRepo          interfaces.DataSourceRepository
	syncLogRepo     interfaces.SyncLogRepository
	dsScheduler     *datasource.Scheduler

	resourceCatalog        interfaces.ResourceCatalog
	nameEmbeddingRebuilder ContextNameEmbeddingRebuildTrigger
}

// NewKnowledgeBaseService creates a new knowledge base service
func NewKnowledgeBaseService(db *gorm.DB, repo interfaces.KnowledgeBaseRepository,
	kgRepo interfaces.KnowledgeRepository,
	chunkRepo interfaces.ChunkRepository,
	modelService interfaces.ModelService,
	retrieveEngine interfaces.RetrievalService,
	tenantRepo interfaces.TenantRepository,
	fileSvc interfaces.FileService,
	asynqClient interfaces.TaskEnqueuer,
	taskInspector interfaces.TaskInspector,
	taskPendingRepo interfaces.TaskPendingOpsRepository,
	dsRepo interfaces.DataSourceRepository,
	syncLogRepo interfaces.SyncLogRepository,
	dsScheduler *datasource.Scheduler,

	resourceCatalog interfaces.ResourceCatalog,
) interfaces.KnowledgeBaseService {
	return &knowledgeBaseService{
		db:              db,
		repo:            repo,
		kgRepo:          kgRepo,
		chunkRepo:       chunkRepo,
		modelService:    modelService,
		retrieveEngine:  retrieveEngine,
		tenantRepo:      tenantRepo,
		fileSvc:         fileSvc,
		asynqClient:     asynqClient,
		taskInspector:   taskInspector,
		taskPendingRepo: taskPendingRepo,
		dsRepo:          dsRepo,
		syncLogRepo:     syncLogRepo,
		dsScheduler:     dsScheduler,

		resourceCatalog: resourceCatalog,
	}
}

// GetRepository gets the knowledge base repository
// Parameters:
//   - ctx: Context with authentication and request information
//
// Returns:
//   - interfaces.KnowledgeBaseRepository: Knowledge base repository
func (s *knowledgeBaseService) GetRepository() interfaces.KnowledgeBaseRepository {
	return s.repo
}

// CreateKnowledgeBase creates a new knowledge base.
func (s *knowledgeBaseService) CreateKnowledgeBase(ctx context.Context,
	kb *types.KnowledgeBase,
) (*types.KnowledgeBase, error) {
	// Generate UUID and set creation timestamps
	if kb.ID == "" {
		kb.ID = uuid.New().String()
	}
	kb.CreatedAt = time.Now()
	kb.TenantID = types.MustTenantIDFromContext(ctx)
	if err := s.validateKnowledgeBaseDomain(ctx, kb.TenantID, kb.DomainID); err != nil {
		return nil, err
	}
	if err := ValidateModelTierConfig(ctx, s.db, kb.ModelTiers); err != nil {
		return nil, err
	}
	kb.UpdatedAt = time.Now()
	if uid, ok := types.UserIDFromContext(ctx); ok {
		kb.CreatorID = uid
	}
	kb.EnsureDefaults()

	logger.Infof(ctx, "Creating knowledge base, ID: %s, tenant ID: %d, name: %s", kb.ID, kb.TenantID, kb.Name)

	if err := s.persistKnowledgeBase(ctx, kb, true); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": kb.ID,
			"tenant_id":         kb.TenantID,
		})
		return nil, err
	}
	if s.nameEmbeddingRebuilder != nil && kb.ModelTiers.ModelID(types.ModelTierEmbedding) != "" {
		if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, kb.TenantID, "knowledge_base_embedding_override_changed"); err != nil {
			return kb, fmt.Errorf("knowledge base saved but name embedding rebuild could not be queued: %w", err)
		}
	}

	logger.Infof(ctx, "Knowledge base created successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

func (s *knowledgeBaseService) validateKnowledgeBaseDomain(ctx context.Context, tenantID uint64, domainID string) error {
	if strings.TrimSpace(domainID) == "" {
		return apperrors.NewBadRequestError("domain_id is required")
	}
	// In-memory repository doubles omit the database; production always has it.
	if s.db == nil {
		return nil
	}
	if _, err := getKnowledgeDomain(ctx, s.db, tenantID, domainID, false); err != nil {
		if appErr, ok := apperrors.IsAppError(err); ok && appErr.Code == apperrors.ErrNotFound {
			return apperrors.NewBadRequestError("domain_id must reference an existing team domain")
		}
		return err
	}
	return nil
}

// The row lock keeps domain deletion and knowledge-base writes consistent.
func (s *knowledgeBaseService) persistKnowledgeBase(ctx context.Context, kb *types.KnowledgeBase, create bool) error {
	persist := func(repo interfaces.KnowledgeBaseRepository) error {
		if create {
			return repo.CreateKnowledgeBase(ctx, kb)
		}
		return repo.UpdateKnowledgeBase(ctx, kb)
	}
	if s.db == nil {
		return persist(s.repo)
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if !create {
			var active types.KnowledgeBase
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").Where("tenant_id = ? AND id = ?", kb.TenantID, kb.ID).First(&active).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return apperrors.NewNotFoundError("knowledge base not found")
				}
				return err
			}
		}
		var domain types.KnowledgeDomain
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("tenant_id = ? AND id = ?", kb.TenantID, kb.DomainID).First(&domain).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return apperrors.NewBadRequestError("domain_id must reference an existing team domain")
			}
			return err
		}
		return persist(repository.NewKnowledgeBaseRepository(tx))
	})
}

// GetKnowledgeBaseByID retrieves a knowledge base by its ID
func (s *knowledgeBaseService) GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	if kb == nil || kb.TenantID != types.SingleTenantID {
		return nil, repository.ErrKnowledgeBaseNotFound
	}
	kb.EnsureDefaults()
	return kb, nil
}

func (s *knowledgeBaseService) GetKnowledgeBaseByIDOnly(ctx context.Context, id string) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	if kb == nil || kb.TenantID != types.SingleTenantID {
		return nil, repository.ErrKnowledgeBaseNotFound
	}
	kb.EnsureDefaults()
	return kb, nil
}

// GetKnowledgeBasesByIDsOnly retrieves team knowledge bases by IDs (batch).
func (s *knowledgeBaseService) GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	kbs, err := s.repo.GetKnowledgeBaseByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	teamKBs := make([]*types.KnowledgeBase, 0, len(kbs))
	for _, kb := range kbs {
		if kb != nil && kb.TenantID == types.SingleTenantID {
			kb.EnsureDefaults()
			teamKBs = append(teamKBs, kb)
		}
	}
	return teamKBs, nil
}

// ListKnowledgeBases returns all knowledge bases for a tenant
func (s *knowledgeBaseService) ListKnowledgeBases(ctx context.Context) ([]*types.KnowledgeBase, error) {
	tenantID := types.MustTenantIDFromContext(ctx)

	kbs, err := s.repo.ListKnowledgeBasesByTenantID(ctx, tenantID)
	if err != nil {
		for _, kb := range kbs {
			kb.EnsureDefaults()
		}

		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}

	// Query knowledge count and chunk count for each knowledge base
	for _, kb := range kbs {
		kb.EnsureDefaults()

		// Get knowledge count
		switch kb.Type {
		case types.KnowledgeBaseTypeDocument:
			knowledgeCount, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID)
			if err != nil {
				logger.Warnf(ctx, "Failed to get knowledge count for knowledge base %s: %v", kb.ID, err)
			} else {
				kb.KnowledgeCount = knowledgeCount
			}
		}

		// Check if there is a processing import task
		processingCount, err := s.kgRepo.CountKnowledgeByStatus(
			ctx,
			tenantID,
			kb.ID,
			[]string{"pending", "processing"},
		)
		if err != nil {
			logger.Warnf(ctx, "Failed to check processing status for knowledge base %s: %v", kb.ID, err)
		} else {
			kb.IsProcessing = processingCount > 0
			kb.ProcessingCount = processingCount
		}
	}

	if userID, ok := types.UserIDFromContext(ctx); ok && userID != "" {
		s.applyUserKBPins(ctx, tenantID, userID, kbs)
	}
	return kbs, nil
}

func (s *knowledgeBaseService) ListKnowledgeBasesByTenantID(ctx context.Context, tenantID uint64) ([]*types.KnowledgeBase, error) {
	kbs, err := s.repo.ListKnowledgeBasesByTenantID(ctx, tenantID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id": tenantID,
		})
		return nil, err
	}
	for _, kb := range kbs {
		kb.EnsureDefaults()
		switch kb.Type {
		case types.KnowledgeBaseTypeDocument:
			if cnt, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
				kb.KnowledgeCount = cnt
			}
		}
		if processingCount, err := s.kgRepo.CountKnowledgeByStatus(ctx, tenantID, kb.ID, []string{"pending", "processing"}); err == nil {
			kb.IsProcessing = processingCount > 0
			kb.ProcessingCount = processingCount
		}
	}

	if userID, ok := types.UserIDFromContext(ctx); ok && userID != "" {
		s.applyUserKBPins(ctx, tenantID, userID, kbs)
	}
	return kbs, nil
}

// FillKnowledgeBaseCounts fills KnowledgeCount, ChunkCount, IsProcessing, ProcessingCount for the given KB using kb.TenantID.
func (s *knowledgeBaseService) FillKnowledgeBaseCounts(ctx context.Context, kb *types.KnowledgeBase) error {
	if kb == nil {
		return nil
	}
	tenantID := kb.TenantID
	kb.EnsureDefaults()
	switch kb.Type {
	case types.KnowledgeBaseTypeDocument:
		if cnt, err := s.kgRepo.CountKnowledgeByKnowledgeBaseID(ctx, tenantID, kb.ID); err == nil {
			kb.KnowledgeCount = cnt
		}
	}
	if processingCount, err := s.kgRepo.CountKnowledgeByStatus(ctx, tenantID, kb.ID, []string{"pending", "processing"}); err == nil {
		kb.IsProcessing = processingCount > 0
		kb.ProcessingCount = processingCount
	}
	return nil
}

// UpdateKnowledgeBase updates a knowledge base's mutable properties.
func (s *knowledgeBaseService) UpdateKnowledgeBase(ctx context.Context,
	id string,
	name string,
	description string,
	config *types.KnowledgeBaseConfig,
	domainIDs ...string,
) (*types.KnowledgeBase, error) {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return nil, errors.New("knowledge base ID cannot be empty")
	}

	logger.Infof(ctx, "Updating knowledge base, ID: %s, name: %s", id, name)

	// Get existing knowledge base
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}

	changedFields := make([]string, 0, 3)
	previousEmbeddingOverride := kb.ModelTiers.ModelID(types.ModelTierEmbedding)
	if len(domainIDs) > 0 {
		if err := s.validateKnowledgeBaseDomain(ctx, kb.TenantID, domainIDs[0]); err != nil {
			return nil, err
		}
		if kb.DomainID != domainIDs[0] {
			changedFields = append(changedFields, "domain_id")
			kb.DomainID = domainIDs[0]
		}
	}
	if kb.Name != name {
		changedFields = append(changedFields, "name")
	}
	if kb.Description != description {
		changedFields = append(changedFields, "description")
	}
	if config != nil {
		changedFields = append(changedFields, "config")
	}

	// Update the knowledge base properties
	kb.Name = name
	kb.Description = description
	if config != nil {
		kb.ChunkingConfig = config.ChunkingConfig
		kb.ImageProcessingConfig = config.ImageProcessingConfig

		if config.WikiConfig != nil {
			kb.WikiConfig = config.WikiConfig
		}
		if config.AutoTagConfig != nil {
			config.AutoTagConfig.Normalize()
			kb.AutoTagConfig = config.AutoTagConfig
		}
		// Update indexing strategy — syncs to ExtractConfig for backward compat
		if config.IndexingStrategy != nil {
			if !config.IndexingStrategy.HasAnyIndexing() {
				return nil, errors.New("at least one indexing strategy must be enabled")
			}
			kb.IndexingStrategy = *config.IndexingStrategy
			// Ensure WikiConfig exists when wiki indexing is enabled so that
			// wiki-specific tunables (synthesis model, granularity, …) have a home.
			if kb.WikiConfig == nil && config.IndexingStrategy.WikiEnabled {
				kb.WikiConfig = &types.WikiConfig{}
			}

		}
	}
	if config != nil && config.ModelTiers != nil {
		if err := ValidateModelTierConfig(ctx, s.db, *config.ModelTiers); err != nil {
			return nil, err
		}
		kb.ModelTiers = *config.ModelTiers
	}
	kb.UpdatedAt = time.Now()
	kb.EnsureDefaults()

	logger.Info(ctx, "Saving knowledge base update")
	if err := s.persistKnowledgeBase(ctx, kb, false); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return nil, err
	}
	if s.nameEmbeddingRebuilder != nil && previousEmbeddingOverride != kb.ModelTiers.ModelID(types.ModelTierEmbedding) {
		if err := s.nameEmbeddingRebuilder.MarkAndEnqueue(ctx, kb.TenantID, "knowledge_base_embedding_override_changed"); err != nil {
			return kb, fmt.Errorf("knowledge base saved but name embedding rebuild could not be queued: %w", err)
		}
	}

	logger.Infof(ctx, "Knowledge base updated successfully, ID: %s, name: %s", kb.ID, kb.Name)
	return kb, nil
}

// TogglePinKnowledgeBase toggles whether the calling user has pinned
// this knowledge base. Pin state is per-(user, kb) in the baseline
// schema; previously this method flipped a tenant-wide column on the
// KB row which broke down under RBAC (only Admin/creator could pin,
// and the pin reordered the list for everyone in the tenant). The
// public signature is unchanged so the HTTP handler / CLI / SDK don't
// move.
//
// The KB still has to belong to the caller's tenant — the route is
// already gated behind KBAccessRead, but we re-check via
// GetKnowledgeBaseByIDAndTenant so a stale param survives a tenant
// switch cleanly.
func (s *knowledgeBaseService) TogglePinKnowledgeBase(
	ctx context.Context, id string,
) (*types.KnowledgeBase, error) {
	if id == "" {
		return nil, errors.New("knowledge base ID cannot be empty")
	}
	tenantID := types.MustTenantIDFromContext(ctx)
	userID, ok := types.UserIDFromContext(ctx)
	if !ok || userID == "" {
		// API-key callers without a user identity can't have a personal
		// pin set. We surface this rather than silently flipping a
		// shared-tenant flag like the old behaviour.
		return nil, errors.New("pin requires an authenticated user")
	}

	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
		})
		return nil, err
	}

	// Read current pin state to decide direction. ListUserKBPinIDs is
	// already optimised for the "many KBs at once" path; for a single-id
	// check the round-trip is acceptable and avoids leaking a second
	// repository method just for this.
	pins, err := s.repo.ListUserKBPinIDs(ctx, tenantID, userID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
			"user_id":           userID,
		})
		return nil, err
	}
	_, currentlyPinned := pins[id]

	pinnedAt, err := s.repo.SetUserKBPin(ctx, tenantID, userID, id, !currentlyPinned)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
			"tenant_id":         tenantID,
			"user_id":           userID,
			"target_pinned":     !currentlyPinned,
		})
		return nil, err
	}

	kb.EnsureDefaults()
	kb.IsPinned = !currentlyPinned
	kb.PinnedAt = pinnedAt
	logger.Infof(ctx, "Knowledge base pin toggled, ID: %s, user: %s, is_pinned: %v",
		id, userID, kb.IsPinned)
	return kb, nil
}

// applyUserKBPins stamps IsPinned / PinnedAt onto each KB in the slice
// from the caller's perspective and sorts the slice so pinned rows
// float to the top (newest pin first, ties broken by created_at desc).
// Safe to call with an empty userID (no-op stamp; default sort by
// created_at preserved).
func (s *knowledgeBaseService) applyUserKBPins(
	ctx context.Context, tenantID uint64, userID string, kbs []*types.KnowledgeBase,
) {
	if len(kbs) == 0 || userID == "" {
		return
	}
	pins, err := s.repo.ListUserKBPinIDs(ctx, tenantID, userID)
	if err != nil {
		// Pin enrichment is best-effort: a transient DB blip here
		// should not break listing KBs. Log and bail without altering
		// the slice — caller still gets a valid list, just unsorted by
		// pin.
		logger.Warnf(ctx, "applyUserKBPins: failed to load pins for tenant=%d user=%s: %v",
			tenantID, userID, err)
		return
	}
	if len(pins) == 0 {
		return
	}
	for _, kb := range kbs {
		if ts, ok := pins[kb.ID]; ok {
			kb.IsPinned = true
			t := ts
			kb.PinnedAt = &t
		}
	}
	sort.SliceStable(kbs, func(i, j int) bool {
		a, b := kbs[i], kbs[j]
		if a.IsPinned != b.IsPinned {
			return a.IsPinned
		}
		if a.IsPinned && b.IsPinned {
			at, bt := a.PinnedAt, b.PinnedAt
			if at != nil && bt != nil && !at.Equal(*bt) {
				return at.After(*bt)
			}
		}
		return a.CreatedAt.After(b.CreatedAt)
	})
}

// DeleteKnowledgeBase deletes a knowledge base by its ID
// This method marks the knowledge base as deleted and enqueues an async task
// to handle the heavy cleanup operations (embeddings, chunks, files, graph data)
func (s *knowledgeBaseService) DeleteKnowledgeBase(ctx context.Context, id string) error {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return errors.New("knowledge base ID cannot be empty")
	}

	logger.Infof(ctx, "Deleting knowledge base, ID: %s", id)

	// Get tenant ID from context
	tenantID := types.MustTenantIDFromContext(ctx)
	if _, err := s.repo.GetKnowledgeBaseByID(ctx, id); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{"knowledge_base_id": id})
		return err
	}

	// Step 1: Delete the knowledge base record first (mark as deleted)
	logger.Infof(ctx, "Deleting knowledge base from database")
	err := s.repo.DeleteKnowledgeBase(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return err
	}
	if s.db != nil && s.db.Migrator().HasTable(&types.KnowledgeWorkflow{}) {
		if err := s.disableKnowledgeWorkflows(ctx, id); err != nil {
			logger.Warnf(ctx, "Stop deleted KB workflows: %v", err)
		}
	}

	// Stop both ephemeral queue work and durable wiki operations that target
	// the now-deleted KB. ProcessKBDelete repeats this with document IDs and
	// performs one final scrub after heavy cleanup to close enqueue races.
	//
	// Run detached with a bounded timeout so a disconnecting API client cannot
	// truncate this best-effort scrub mid-scan, matching ProcessKBDelete's
	// cleanup semantics. The KB row is already soft-deleted, so the async
	// delete task remains the durable backstop even if this pass is cut short.
	kbCleanupCtx, cancelKBCleanup := context.WithTimeout(
		context.WithoutCancel(ctx), kbTaskCleanupTimeout,
	)
	s.cleanupTasksForKnowledgeBase(kbCleanupCtx, id, nil, nil)
	cancelKBCleanup()

	// Step 1c: Stop and soft-delete all data sources bound to this KB so cron
	// schedules and in-flight sync logs do not keep running against a deleted KB.
	dataSourceIDs := s.deleteDataSourcesForKnowledgeBase(ctx, id)
	if len(dataSourceIDs) > 0 {
		dsCancelCtx, cancelDSCancel := context.WithTimeout(
			context.WithoutCancel(ctx), kbTaskCleanupTimeout,
		)
		s.cancelTasksForKnowledgeBase(dsCancelCtx, id, nil, dataSourceIDs)
		cancelDSCancel()
	}

	// Step 2: Enqueue async task for heavy cleanup operations
	payload := types.KBDeletePayload{
		TenantID:        tenantID,
		KnowledgeBaseID: id,
		DataSourceIDs:   dataSourceIDs,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		logger.Warnf(ctx, "Failed to marshal KB delete payload: %v", err)
		// Don't fail the request, the KB record is already deleted
		return nil
	}

	task := asynq.NewTask(types.TypeKBDelete, payloadBytes,
		asynq.Queue(types.QueueMaintenance), asynq.MaxRetry(3), asynq.Timeout(2*time.Hour))
	info, err := s.asynqClient.Enqueue(task)
	if err != nil {
		logger.Warnf(ctx, "Failed to enqueue KB delete task: %v", err)
		// Don't fail the request, the KB record is already deleted
		return nil
	}

	logger.Infof(ctx, "KB delete task enqueued: %s, knowledge base ID: %s", info.ID, id)
	logger.Infof(ctx, "Knowledge base deleted successfully, ID: %s", id)
	return nil
}

// ProcessKBDelete handles async knowledge base deletion task
// This method performs heavy cleanup operations: deleting embeddings, chunks, files, and graph data
func (s *knowledgeBaseService) ProcessKBDelete(ctx context.Context, t *asynq.Task) error {
	var payload types.KBDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil {
		logger.Errorf(ctx, "Failed to unmarshal KB delete payload: %v", err)
		return err
	}

	tenantID := payload.TenantID
	kbID := payload.KnowledgeBaseID
	var knowledgeIDs []string

	// Set tenant context for downstream services
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	defer func() {
		// Workers may enqueue downstream work while the delete task performs
		// heavy storage cleanup. A detached, bounded final scrub runs on every
		// return path, including retryable failures and cancellation.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), kbTaskCleanupTimeout)
		defer cancel()
		s.cleanupTasksForKnowledgeBase(cleanupCtx, kbID, knowledgeIDs, payload.DataSourceIDs)
	}()

	logger.Infof(ctx, "Processing KB delete task for knowledge base: %s", kbID)

	// Step 1: Get all knowledge entries in this knowledge base
	logger.Infof(ctx, "Fetching all knowledge entries in knowledge base, ID: %s", kbID)
	knowledgeList, err := s.kgRepo.ListKnowledgeByKnowledgeBaseID(ctx, tenantID, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": kbID,
		})
		return err
	}
	logger.Infof(ctx, "Found %d knowledge entries to delete", len(knowledgeList))
	knowledgeIDs = make([]string, 0, len(knowledgeList))
	for _, knowledge := range knowledgeList {
		knowledgeIDs = append(knowledgeIDs, knowledge.ID)
	}

	// Repeat the best-effort queue scrub with document IDs. Some batch tasks
	// only carry knowledge_id(s), and active work from the first pass may have
	// enqueued another downstream task before cancellation reached it.
	s.cleanupTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, payload.DataSourceIDs)

	// Step 2: Delete all knowledge entries and their resources
	if len(knowledgeList) > 0 {
		logger.Infof(ctx, "Deleting all knowledge entries and their resources")

		retrieveEngine := s.retrieveEngine
		// Group knowledge by embedding model and type
		type groupKey struct {
			EmbeddingModelID string
			Type             string
		}
		embeddingGroups := make(map[groupKey][]string)
		for _, knowledge := range knowledgeList {
			key := groupKey{EmbeddingModelID: knowledge.EmbeddingModelID, Type: knowledge.Type}
			embeddingGroups[key] = append(embeddingGroups[key], knowledge.ID)
		}

		for key, knowledgeGroup := range embeddingGroups {
			embeddingModel, err := s.modelService.GetEmbeddingModel(ctx, key.EmbeddingModelID)
			if err != nil {
				logger.Warnf(ctx, "Failed to get embedding model %s: %v", key.EmbeddingModelID, err)
				continue
			}
			if err := retrieveEngine.DeleteByKnowledgeIDList(ctx, knowledgeGroup, embeddingModel.GetDimensions(), key.Type); err != nil {
				logger.Warnf(ctx, "Failed to delete embeddings for model %s: %v", key.EmbeddingModelID, err)
			}
		}

		// Collect image URLs before chunks are deleted
		chunkImageInfos, imgErr := s.chunkRepo.ListImageInfoByKnowledgeIDs(ctx, tenantID, knowledgeIDs)
		if imgErr != nil {
			logger.Warnf(ctx, "Failed to collect image URLs for KB delete: %v", imgErr)
		}
		var imageInfoStrs []string
		for _, ci := range chunkImageInfos {
			imageInfoStrs = append(imageInfoStrs, ci.ImageInfo)
		}
		imageURLs := collectImageURLs(ctx, imageInfoStrs)
		historicalImages, err := evidenceImageReferences(ctx, s.db, tenantID, knowledgeIDs)
		if err != nil {
			return err
		}
		imageURLs = append(imageURLs, historicalImages...)

		// Delete all chunks
		logger.Infof(ctx, "Deleting all chunks in knowledge base")
		for _, knowledgeID := range knowledgeIDs {
			if err := s.chunkRepo.DeleteChunksByKnowledgeID(ctx, tenantID, knowledgeID); err != nil {
				logger.Warnf(ctx, "Failed to delete chunks for knowledge %s: %v", knowledgeID, err)
			}
		}

		// Delete physical files, extracted images, and adjust storage
		logger.Infof(ctx, "Deleting physical files and extracted images")
		storageAdjust := int64(0)
		for _, knowledge := range knowledgeList {
			if knowledge.FilePath != "" {
				if err := s.fileSvc.DeleteFile(ctx, knowledge.FilePath); err != nil {
					logger.Warnf(ctx, "Failed to delete file %s: %v", knowledge.FilePath, err)
				}
			}
			storageAdjust -= knowledge.StorageSize
		}
		deleteExtractedImages(ctx, s.fileSvc, knowledgeResourceOwners(s.resourceCatalog, knowledgeIDs...), imageURLs)
		if storageAdjust != 0 {
			if err := s.tenantRepo.AdjustStorageUsed(ctx, tenantID, storageAdjust); err != nil {
				logger.Warnf(ctx, "Failed to adjust tenant storage: %v", err)
			}
		}

		// Delete all knowledge entries from database
		logger.Infof(ctx, "Deleting knowledge entries from database")
		if err := s.kgRepo.DeleteKnowledgeList(ctx, tenantID, knowledgeIDs); err != nil {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"knowledge_base_id": kbID,
			})
			return err
		}
	}

	if err := s.cleanupKnowledgeFusion(ctx, kbID); err != nil {
		return err
	}

	logger.Infof(ctx, "KB delete task completed successfully, knowledge base ID: %s", kbID)
	return nil
}

// cancelTasksForKnowledgeBase removes queue work for a deleted KB when the
// configured task backend supports knowledge-base-wide inspection. Queue
// cleanup is an optimization: the soft-deleted database row remains the
// durable source of truth, so backend failures must not fail KB deletion.
func (s *knowledgeBaseService) cancelTasksForKnowledgeBase(
	ctx context.Context,
	kbID string,
	knowledgeIDs []string,
	dataSourceIDs []string,
) {
	canceller, ok := s.taskInspector.(interfaces.KnowledgeBaseTaskCanceller)
	if !ok || kbID == "" {
		return
	}
	if _, _, err := canceller.CancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, dataSourceIDs); err != nil {
		logger.Warnf(ctx, "Failed to cancel queued tasks for deleted KB %s: %v", kbID, err)
	}
}

// cleanupTasksForKnowledgeBase removes both asynq records and durable wiki
// operations. The latter must be cleared as well or startup recovery can
// recreate Redis triggers for a KB that no longer exists.
func (s *knowledgeBaseService) cleanupTasksForKnowledgeBase(
	ctx context.Context,
	kbID string,
	knowledgeIDs []string,
	dataSourceIDs []string,
) {
	if kbID == "" {
		return
	}
	cleaner, ok := s.taskPendingRepo.(interfaces.TaskPendingOpsScopeCleaner)
	if ok {
		// Clear durable work before scanning Redis. A large or degraded queue
		// must not consume the caller's entire deadline and starve the database
		// fence that prevents startup recovery from reviving this KB.
		if err := cleaner.DeleteByScope(ctx, types.TaskScopeKnowledgeBase, kbID); err != nil {
			logger.Warnf(ctx, "Failed to clear durable tasks for deleted KB %s: %v", kbID, err)
		}
	}
	s.cancelTasksForKnowledgeBase(ctx, kbID, knowledgeIDs, dataSourceIDs)
}

// deleteDataSourcesForKnowledgeBase mirrors DataSourceService.DeleteDataSource for
// every data source attached to the KB. Errors on individual sources are logged
// but do not fail KB deletion — the KB record is already soft-deleted.
func (s *knowledgeBaseService) deleteDataSourcesForKnowledgeBase(ctx context.Context, kbID string) []string {
	if s.dsRepo == nil {
		return nil
	}

	dataSources, err := s.dsRepo.FindByKnowledgeBase(ctx, kbID)
	if err != nil {
		logger.Warnf(ctx, "Failed to list data sources for deleted KB %s: %v", kbID, err)
		return nil
	}
	dataSourceIDs := make([]string, 0, len(dataSources))
	for _, ds := range dataSources {
		if ds == nil || ds.ID == "" {
			continue
		}
		dataSourceIDs = append(dataSourceIDs, ds.ID)
		if err := s.dsRepo.Delete(ctx, ds.ID); err != nil {
			logger.Warnf(ctx, "Failed to delete data source %s for KB %s: %v", ds.ID, kbID, err)
			continue
		}
		if s.dsScheduler != nil {
			s.dsScheduler.Remove(ds.ID)
		}
		if s.syncLogRepo != nil {
			if err := s.syncLogRepo.CancelPendingByDataSource(ctx, ds.ID); err != nil {
				logger.Warnf(ctx, "Failed to cancel pending sync logs for ds=%s (kb=%s): %v", ds.ID, kbID, err)
			}
		}
		logger.Infof(ctx, "Data source deleted with knowledge base: ds=%s kb=%s", ds.ID, kbID)
	}
	return dataSourceIDs
}

// SetEmbeddingModel sets the embedding model for a knowledge base
func (s *knowledgeBaseService) SetEmbeddingModel(ctx context.Context, id string, modelID string) error {
	if id == "" {
		logger.Error(ctx, "Knowledge base ID is empty")
		return errors.New("knowledge base ID cannot be empty")
	}

	if modelID == "" {
		logger.Error(ctx, "Model ID is empty")
		return errors.New("model ID cannot be empty")
	}

	logger.Infof(ctx, "Setting embedding model for knowledge base, knowledge base ID: %s, model ID: %s", id, modelID)

	// Get the knowledge base
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id": id,
		})
		return err
	}

	// Update the knowledge base's embedding model
	kb.EmbeddingModelID = modelID
	kb.UpdatedAt = time.Now()

	logger.Info(ctx, "Saving knowledge base embedding model update")
	err = s.repo.UpdateKnowledgeBase(ctx, kb)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_id":  id,
			"embedding_model_id": modelID,
		})
		return err
	}

	logger.Infof(
		ctx,
		"Knowledge base embedding model set successfully, knowledge base ID: %s, model ID: %s",
		id,
		modelID,
	)
	return nil
}

func (s *knowledgeBaseService) CopyKnowledgeBase(ctx context.Context,
	srcKB string, dstKB string,
) (*types.KnowledgeBase, *types.KnowledgeBase, error) {
	tenantID := types.MustTenantIDFromContext(ctx)
	// Load source KB with tenant scope to prevent cross-tenant cloning
	sourceKB, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, srcKB, tenantID)
	if err != nil {
		logger.Errorf(ctx, "Get source knowledge base failed: %v", err)
		return nil, nil, err
	}
	sourceKB.EnsureDefaults()
	var targetKB *types.KnowledgeBase
	if dstKB != "" {
		// Load target KB with tenant scope so we only clone into the caller's tenant
		targetKB, err = s.repo.GetKnowledgeBaseByIDAndTenant(ctx, dstKB, tenantID)
		if err != nil {
			return nil, nil, err
		}

		// Defense 1: embedding model must match. Mixing incompatible
		// vector spaces would produce semantically broken search results.
		if sourceKB.EmbeddingModelID != targetKB.EmbeddingModelID {
			return nil, nil, apperrors.NewBadRequestError(
				"source and target knowledge bases use different embedding models; " +
					"clone into a target with the same embedding model")
		}
	} else {
		targetKB = &types.KnowledgeBase{
			ID:                    uuid.New().String(),
			Name:                  sourceKB.Name,
			Type:                  sourceKB.Type,
			Description:           sourceKB.Description,
			TenantID:              tenantID,
			DomainID:              sourceKB.DomainID,
			ChunkingConfig:        sourceKB.ChunkingConfig,
			ImageProcessingConfig: sourceKB.ImageProcessingConfig,
			EmbeddingModelID:      sourceKB.EmbeddingModelID,
			SummaryModelID:        sourceKB.SummaryModelID,
			ModelTiers:            sourceKB.ModelTiers,
			VLMConfig:             sourceKB.VLMConfig,
		}
		// Record the authenticated user as the clone's creator.
		if uid, ok := types.UserIDFromContext(ctx); ok {
			targetKB.CreatorID = uid
		}
		targetKB.EnsureDefaults()
		if err := s.persistKnowledgeBase(ctx, targetKB, true); err != nil {
			return nil, nil, err
		}
	}
	return sourceKB, targetKB, nil
}

func (s *knowledgeBaseService) DuplicateKnowledgeBase(
	ctx context.Context,
	srcKB string,
) (*types.KnowledgeBase, error) {
	srcKB = strings.TrimSpace(srcKB)
	if srcKB == "" {
		return nil, apperrors.NewBadRequestError("source knowledge base ID cannot be empty")
	}

	tenantID := types.MustTenantIDFromContext(ctx)
	sourceKB, err := s.repo.GetKnowledgeBaseByIDAndTenant(ctx, srcKB, tenantID)
	if err != nil {
		logger.Errorf(ctx, "Get source knowledge base failed: %v", err)
		return nil, err
	}
	sourceKB.EnsureDefaults()

	targetKB, err := cloneKnowledgeBaseConfiguration(sourceKB)
	if err != nil {
		return nil, err
	}
	targetKB.ID = uuid.New().String()
	targetKB.TenantID = tenantID
	targetKB.Name = s.buildDuplicateKnowledgeBaseName(ctx, tenantID, sourceKB.Name)
	targetKB.CreatorID = ""
	if uid, ok := types.UserIDFromContext(ctx); ok {
		targetKB.CreatorID = uid
	}
	now := time.Now()
	targetKB.CreatedAt = now
	targetKB.UpdatedAt = now
	targetKB.DeletedAt.Valid = false
	targetKB.DeletedAt.Time = time.Time{}
	targetKB.IsTemporary = false
	targetKB.IsPinned = false
	targetKB.PinnedAt = nil
	targetKB.KnowledgeCount = 0
	targetKB.ChunkCount = 0
	targetKB.IsProcessing = false
	targetKB.ProcessingCount = 0
	targetKB.CreatorName = ""
	targetKB.EnsureDefaults()

	if err := s.persistKnowledgeBase(ctx, targetKB, true); err != nil {
		return nil, err
	}
	return targetKB, nil
}

func duplicateKBCopySuffix(locale string) string {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "zh"):
		return " 副本"
	case strings.HasPrefix(locale, "ko"):
		return " 사본"
	case strings.HasPrefix(locale, "ru"):
		return " копия"
	default:
		return " Copy"
	}
}

func duplicateKBDefaultName(locale string) string {
	locale = strings.ToLower(locale)
	switch {
	case strings.HasPrefix(locale, "zh"):
		return "知识库"
	case strings.HasPrefix(locale, "ko"):
		return "지식베이스"
	case strings.HasPrefix(locale, "ru"):
		return "База знаний"
	default:
		return "Knowledge Base"
	}
}

func (s *knowledgeBaseService) buildDuplicateKnowledgeBaseName(
	ctx context.Context,
	tenantID uint64,
	sourceName string,
) string {
	locale := types.LanguageFromContextOrDefault(ctx)
	suffix := duplicateKBCopySuffix(locale)

	baseName := strings.TrimSpace(sourceName)
	if baseName == "" {
		baseName = duplicateKBDefaultName(locale)
	}

	kbs, err := s.repo.ListKnowledgeBasesByTenantID(ctx, tenantID)
	if err != nil {
		logger.Warnf(ctx, "List tenant knowledge bases failed while building duplicate name: %v", err)
		return baseName + suffix
	}

	existing := make(map[string]struct{}, len(kbs))
	for _, kb := range kbs {
		if kb == nil {
			continue
		}
		existing[kb.Name] = struct{}{}
	}

	candidate := baseName + suffix
	if _, ok := existing[candidate]; !ok {
		return candidate
	}
	for i := 2; ; i++ {
		candidate = fmt.Sprintf("%s%s %d", baseName, suffix, i)
		if _, ok := existing[candidate]; !ok {
			return candidate
		}
	}
}

func cloneKnowledgeBaseConfiguration(sourceKB *types.KnowledgeBase) (*types.KnowledgeBase, error) {
	if sourceKB == nil {
		return nil, apperrors.NewBadRequestError("source knowledge base cannot be empty")
	}
	data, err := json.Marshal(sourceKB)
	if err != nil {
		return nil, err
	}
	var targetKB types.KnowledgeBase
	if err := json.Unmarshal(data, &targetKB); err != nil {
		return nil, err
	}
	return &targetKB, nil
}

func (s *knowledgeBaseService) disableKnowledgeWorkflows(ctx context.Context, kbID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		scope := func(q *gorm.DB) *gorm.DB {
			return q.Where("tenant_id = ? AND knowledge_base_id = ?", types.MustTenantIDFromContext(ctx), kbID)
		}
		if err := scope(tx.Model(&types.KnowledgeWorkflow{})).Update("enabled", false).Error; err != nil {
			return err
		}
		return scope(tx.Model(&types.KnowledgeWorkflowRun{})).Where("status IN ?", []string{types.WorkflowStatusQueued, types.WorkflowStatusRunning, types.WorkflowStatusPaused}).Updates(map[string]any{"status": types.WorkflowStatusCancelled, "error": "knowledge base deleted", "updated_at": time.Now()}).Error
	})
}
func (s *knowledgeBaseService) cleanupKnowledgeFusion(ctx context.Context, kbID string) error {
	if s.db == nil {
		return nil
	}
	if s.db.Migrator().HasTable(&types.KnowledgeWorkflow{}) {
		if err := s.disableKnowledgeWorkflows(ctx, kbID); err != nil {
			return err
		}
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			for _, model := range []any{&types.KnowledgeWorkflowStepRun{}, &types.KnowledgeWorkflowRun{}, &types.KnowledgeWorkflowReport{}, &types.KnowledgeWorkflow{}} {
				if err := tx.Where("tenant_id = ? AND knowledge_base_id = ?", types.MustTenantIDFromContext(ctx), kbID).Delete(model).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if s.db.Migrator().HasTable(&types.KnowledgeEvidence{}) {
		return NewKnowledgeContextService(s.db, s.modelService).DeleteKnowledgeBase(ctx, kbID)
	}
	return nil
}

func (s *knowledgeBaseService) ModelRoutingDB() *gorm.DB { return s.db }
