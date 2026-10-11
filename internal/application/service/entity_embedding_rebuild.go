package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// EntityNameEmbeddingRebuilder uses the knowledge base embedding model for entity names.
type EntityNameEmbeddingRebuilder struct {
	db     *gorm.DB
	models interfaces.ModelService
	tasks  interfaces.TaskEnqueuer
}

func NewEntityNameEmbeddingRebuilder(db *gorm.DB, models interfaces.ModelService, tasks interfaces.TaskEnqueuer) *EntityNameEmbeddingRebuilder {
	return &EntityNameEmbeddingRebuilder{db: db, models: models, tasks: tasks}
}

type entityEmbeddingProfile struct {
	modelID   string
	dimension int
	hash      string
	embedder  embedding.Embedder
}

func entityEmbeddingProfileHash(modelID string, dimension int, model *types.Model) string {
	return types.EmbeddingProfileHash(modelID, dimension, model)
}
func (r *EntityNameEmbeddingRebuilder) profileMetadata(ctx context.Context, id string, tenantID uint64) (*types.Model, error) {
	var metadata types.Model
	err := r.db.WithContext(ctx).Where("id=? AND (tenant_id=? OR is_builtin=true)", id, tenantID).First(&metadata).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if metadata.Type != types.ModelTypeEmbedding || metadata.Status != types.ModelStatusActive {
		return nil, errors.New("knowledge base embedding model must be active and have Embedding type")
	}
	return &metadata, nil
}

func (r *EntityNameEmbeddingRebuilder) profile(ctx context.Context, tenantID uint64) (entityEmbeddingProfile, error) {
	ctx = context.WithValue(ctx, types.TenantIDContextKey, tenantID)
	id, err := ResolveModelTier(ctx, r.db, nil, types.ModelTierEmbedding)
	profile := entityEmbeddingProfile{modelID: id}
	if err != nil {
		return profile, err
	}
	if id == "" {
		return profile, errors.New("knowledge base embedding model is not configured")
	}
	if r.models == nil {
		return profile, errors.New("embedding model service is unavailable")
	}
	before, err := r.profileMetadata(ctx, id, tenantID)
	if err != nil {
		return profile, err
	}
	model, err := r.models.GetEmbeddingModel(types.WithModelTier(ctx, types.ModelTierEmbedding), id)
	if err != nil {
		return profile, fmt.Errorf("knowledge base embedding model is unavailable: %w", err)
	}
	if model == nil || model.GetDimensions() < 1 {
		return profile, errors.New("embedding model has no usable dimensions")
	}
	profile.dimension = model.GetDimensions()
	after, err := r.profileMetadata(ctx, id, tenantID)
	if err != nil {
		return profile, err
	}
	currentID, err := ResolveModelTier(ctx, r.db, nil, types.ModelTierEmbedding)
	if err != nil {
		return profile, err
	}
	if currentID != id || entityEmbeddingProfileHash(id, profile.dimension, before) != entityEmbeddingProfileHash(id, profile.dimension, after) || (before != nil && after != nil && !before.UpdatedAt.Equal(after.UpdatedAt)) {
		return profile, errors.New("embedding profile changed while constructing its provider client")
	}
	if after != nil && after.Parameters.EmbeddingParameters.Dimension > 0 && after.Parameters.EmbeddingParameters.Dimension != profile.dimension {
		return profile, fmt.Errorf("embedding model dimension %d does not match configured dimension %d", profile.dimension, after.Parameters.EmbeddingParameters.Dimension)
	}
	profile.embedder = model
	profile.hash = entityEmbeddingProfileHash(id, profile.dimension, after)
	return profile, nil
}

func (r *EntityNameEmbeddingRebuilder) pendingQuery(db *gorm.DB, tenantID uint64, _ entityEmbeddingProfile) *gorm.DB {
	return db.Model(&types.Entity{}).Where("tenant_id=? AND merged_into_id='' AND embedding_needs_rebuild", tenantID)
}

// Profile-wide marking happens only at explicit lifecycle triggers, never in
// mention resolution. Unavailable profiles preserve existing vectors and flags.
func (r *EntityNameEmbeddingRebuilder) MarkAndEnqueue(ctx context.Context, tenantID uint64, reason string) error {
	if r != nil && r.db != nil {
		if _, bound := r.tasks.(*transactionTaskEnqueuer); !bound {
			return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				copy, err := r.WithTransaction(ctx, tx)
				if err != nil {
					return err
				}
				return copy.MarkAndEnqueue(ctx, tenantID, reason)
			})
		}
	}

	if r == nil || r.db == nil || tenantID == 0 {
		return errors.New("embedding rebuild scope or database is unavailable")
	}
	profile, profileErr := r.profile(ctx, tenantID)
	force := reason == "embedding_model_updated"
	if profileErr == nil && !force {
		var previous types.EntityNameEmbeddingRebuildRun
		err := r.db.WithContext(ctx).Where("tenant_id=? AND status='completed' AND profile_hash<>''", tenantID).Order("created_at DESC,id DESC").First(&previous).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		force = (err == nil && previous.ModelID == profile.modelID && previous.ProfileHash != profile.hash) || (errors.Is(err, gorm.ErrRecordNotFound) && reason == "startup")
	}
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockEntitySpaceShared(tx, tenantID); err != nil {
			return err
		}
		query := tx.Model(&types.Entity{}).Where("tenant_id=? AND merged_into_id='' AND NOT embedding_needs_rebuild", tenantID)
		if profileErr == nil && !force {
			query = query.Where("name_embedding IS NULL OR embedding_model_id<>? OR embedding_dimension<>? OR vector_dims(name_embedding)<>?", profile.modelID, profile.dimension, profile.dimension)
		}
		return query.Update("embedding_needs_rebuild", true).Error
	}); err != nil {
		return err
	}
	return r.Enqueue(ctx, tenantID, reason)
}
func (r *EntityNameEmbeddingRebuilder) Enqueue(ctx context.Context, tenantID uint64, reason string) error {
	if r != nil && r.db != nil {
		if _, bound := r.tasks.(*transactionTaskEnqueuer); !bound {
			return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				copy, err := r.WithTransaction(ctx, tx)
				if err != nil {
					return err
				}
				return copy.Enqueue(ctx, tenantID, reason)
			})
		}
	}

	if r == nil || r.db == nil || tenantID == 0 {
		return errors.New("embedding rebuild scope or database is unavailable")
	}
	profile, _ := r.profile(ctx, tenantID)
	var pending int64
	if err := r.pendingQuery(r.db.WithContext(ctx), tenantID, profile).Count(&pending).Error; err != nil {
		return err
	}
	if pending == 0 {
		return nil
	}
	run := types.EntityNameEmbeddingRebuildRun{ID: uuid.NewString(), TenantID: tenantID, Status: "pending", Reason: strings.TrimSpace(reason), ModelID: profile.modelID, Dimension: profile.dimension, ProfileHash: profile.hash, TotalCount: pending, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	if err := r.db.WithContext(ctx).Create(&run).Error; err != nil {
		return err
	}
	payload, _ := json.Marshal(types.EntityNameEmbeddingRebuildPayload{TenantID: tenantID, RunID: run.ID})
	var queueErr error
	if r.tasks == nil {
		queueErr = errors.New("embedding rebuild task queue is unavailable")
	} else {
		_, queueErr = r.tasks.Enqueue(taskqueue.NewTask(types.TypeEntityNameEmbeddingRebuild, payload), taskqueue.Queue(types.QueueEntity), taskqueue.MaxRetry(3), taskqueue.Timeout(30*time.Minute))
	}
	if queueErr != nil {
		if err := r.finish(ctx, &run, "failed", queueErr); err != nil {
			return errors.Join(queueErr, err)
		}
		return queueErr
	}
	return nil
}
func (r *EntityNameEmbeddingRebuilder) Status(ctx context.Context, tenantID uint64) (*types.EntityNameEmbeddingRebuildStatus, error) {
	if r == nil || r.db == nil || tenantID == 0 {
		return nil, errors.New("embedding rebuild scope or database is unavailable")
	}
	profile, _ := r.profile(ctx, tenantID)
	status := &types.EntityNameEmbeddingRebuildStatus{}
	if err := r.pendingQuery(r.db.WithContext(ctx), tenantID, profile).Count(&status.PendingCount).Error; err != nil {
		return nil, err
	}
	var run types.EntityNameEmbeddingRebuildRun
	err := r.db.WithContext(ctx).Where("tenant_id=?", tenantID).Order("created_at DESC,id DESC").First(&run).Error
	if err == nil {
		status.LatestRun = &run
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return status, nil
}
func (r *EntityNameEmbeddingRebuilder) Startup(ctx context.Context) error {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok {
		tenantID = 1
	}
	var space types.Tenant
	if err := r.db.WithContext(ctx).Select("id").Where("id=?", tenantID).First(&space).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	return r.MarkAndEnqueue(ctx, tenantID, "startup")
}
func (r *EntityNameEmbeddingRebuilder) finish(ctx context.Context, run *types.EntityNameEmbeddingRebuildRun, status string, cause error) error {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	now := time.Now()
	detail := ""
	if cause != nil {
		detail = string([]rune(cause.Error())[:min(1000, len([]rune(cause.Error())))])
	}
	query := r.db.WithContext(persistCtx).Model(&types.EntityNameEmbeddingRebuildRun{}).Where("id=? AND tenant_id=?", run.ID, run.TenantID)
	if run.AttemptID != "" {
		query = query.Where("attempt_id=?", run.AttemptID)
	}
	return query.Updates(map[string]any{"status": status, "error": detail, "processed_count": run.ProcessedCount, "model_calls": run.ModelCalls, "finished_at": now, "updated_at": now}).Error
}
func (r *EntityNameEmbeddingRebuilder) Handle(ctx context.Context, task *taskqueue.Task) (handleErr error) {
	var payload types.EntityNameEmbeddingRebuildPayload
	if task == nil {
		return errors.New("embedding rebuild task is unavailable")
	}
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("embedding rebuild payload: %w", err)
	}
	if payload.TenantID == 0 || payload.RunID == "" {
		return errors.New("embedding rebuild task scope is invalid")
	}
	ctx = types.WithBackgroundTask(context.WithValue(ctx, types.TenantIDContextKey, payload.TenantID))
	var run types.EntityNameEmbeddingRebuildRun
	completed := false
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND tenant_id=?", payload.RunID, payload.TenantID).First(&run).Error; err != nil {
			return err
		}
		if run.Status == "completed" {
			completed = true
			return nil
		}
		now := time.Now()
		run.Status = "running"
		run.StartedAt = &now
		run.FinishedAt = nil
		run.ProcessedCount = 0
		run.ModelCalls = 0
		run.Attempts++
		run.AttemptID = uuid.NewString()
		return tx.Model(&run).Updates(map[string]any{"status": "running", "error": "", "started_at": now, "finished_at": nil, "attempts": run.Attempts, "attempt_id": run.AttemptID, "processed_count": 0, "model_calls": 0}).Error
	}); err != nil {
		return err
	}
	if completed {
		return nil
	}
	defer func() {
		status := "completed"
		if handleErr != nil {
			status = "failed"
		}
		if err := r.finish(ctx, &run, status, handleErr); err != nil {
			handleErr = errors.Join(handleErr, err)
		}
	}()
	profile, err := r.profile(ctx, payload.TenantID)
	if err != nil {
		return err
	}
	run.ModelID = profile.modelID
	run.Dimension = profile.dimension
	run.ProfileHash = profile.hash
	if err := r.pendingQuery(r.db.WithContext(ctx), payload.TenantID, profile).Count(&run.TotalCount).Error; err != nil {
		return err
	}
	if err := r.db.WithContext(ctx).Model(&types.EntityNameEmbeddingRebuildRun{}).Where("id=? AND attempt_id=?", run.ID, run.AttemptID).Updates(map[string]any{"model_id": run.ModelID, "dimension": run.Dimension, "profile_hash": run.ProfileHash, "total_count": run.TotalCount}).Error; err != nil {
		return err
	}
	batchSize := min(500, entityConfiguredPositive("RELIQUARY_ENTITY_EMBEDDING_BATCH_SIZE", 64))
	cursor := ""
	for {
		var entities []types.Entity
		query := r.pendingQuery(r.db.WithContext(ctx), payload.TenantID, profile).Order("id").Limit(batchSize)
		if cursor != "" {
			query = query.Where("id>?", cursor)
		}
		if err := query.Find(&entities).Error; err != nil {
			return err
		}
		if len(entities) == 0 {
			break
		}
		cursor = entities[len(entities)-1].ID
		texts := make([]string, len(entities))
		for i, entity := range entities {
			texts[i] = entityNameEmbeddingText(entity.CanonicalName, entity.EntityType, entity.Description)
		}
		embedCtx := types.WithLLMCallMetadata(types.WithModelTier(ctx, types.ModelTierEmbedding), "entity_embedding_rebuild", "")
		types.RecordModelCall(embedCtx, profile.modelID)
		run.ModelCalls++
		vectors, err := profile.embedder.BatchEmbed(embedCtx, texts)
		if err != nil {
			return fmt.Errorf("name embedding batch failed: %w", err)
		}
		if len(vectors) != len(entities) {
			return fmt.Errorf("name embedding batch returned %d vectors for %d entities", len(vectors), len(entities))
		}
		for i, vector := range vectors {
			if len(vector) != profile.dimension {
				return fmt.Errorf("name embedding dimension %d does not match profile dimension %d for %s", len(vector), profile.dimension, entities[i].ID)
			}
			for _, value := range vector {
				if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
					return errors.New("name embedding returned a non-finite vector")
				}
			}
		}
		var written int64
		if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := lockEntitySpaceShared(tx, payload.TenantID); err != nil {
				return err
			}
			if err := r.checkProfile(tx, payload.TenantID, profile); err != nil {
				return err
			}
			// A concurrent retry cannot commit results after a newer attempt takes over.
			var currentRun types.EntityNameEmbeddingRebuildRun
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "attempt_id").Where("id=? AND tenant_id=?", run.ID, run.TenantID).First(&currentRun).Error; err != nil {
				return err
			}
			if currentRun.AttemptID != run.AttemptID {
				return errors.New("embedding rebuild attempt was superseded")
			}
			for i, entity := range entities {
				result := tx.Model(&types.Entity{}).Where("tenant_id=? AND id=? AND merged_into_id='' AND canonical_name=? AND entity_type=? AND description=? AND updated_at=?", payload.TenantID, entity.ID, entity.CanonicalName, entity.EntityType, entity.Description, entity.UpdatedAt).
					Updates(map[string]any{"name_embedding": types.EntityNameEmbedding(vectors[i]), "embedding_model_id": profile.modelID, "embedding_dimension": profile.dimension, "embedding_needs_rebuild": false, "updated_at": time.Now()})
				if result.Error != nil {
					return result.Error
				}
				written += result.RowsAffected
			}
			run.ProcessedCount += written
			return tx.Model(&types.EntityNameEmbeddingRebuildRun{}).Where("id=? AND attempt_id=?", run.ID, run.AttemptID).Updates(map[string]any{"processed_count": run.ProcessedCount, "model_calls": run.ModelCalls}).Error
		}); err != nil {
			return err
		}
	}
	return nil
}
func (r *EntityNameEmbeddingRebuilder) checkProfile(tx *gorm.DB, tenantID uint64, profile entityEmbeddingProfile) error {
	var kb types.KnowledgeBase
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "model_tiers", "embedding_model_id").Where("tenant_id=?", tenantID).First(&kb).Error; err != nil {
		return err
	}
	id, err := ResolveModelTier(context.Background(), nil, &kb, types.ModelTierEmbedding)
	if err != nil || id != profile.modelID {
		return errors.New("knowledge base embedding profile changed during rebuild")
	}
	var metadata types.Model
	err = tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (tenant_id=? OR is_builtin=true)", profile.modelID, tenantID).First(&metadata).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var stored *types.Model
	if err == nil {
		stored = &metadata
	}
	if entityEmbeddingProfileHash(profile.modelID, profile.dimension, stored) != profile.hash {
		return errors.New("embedding model configuration changed during rebuild")
	}
	return nil
}

func (r *EntityNameEmbeddingRebuilder) WithTransaction(ctx context.Context, tx *gorm.DB) (*EntityNameEmbeddingRebuilder, error) {
	queue, ok := r.tasks.(interfaces.TransactionalTaskEnqueuer)
	if !ok {
		return nil, errors.New("entity embedding task queue must support PostgreSQL transactions")
	}
	copy := *r
	copy.db = tx
	// Profile construction must read model edits from this transaction too.
	if models, ok := r.models.(*modelService); ok {
		boundModels := *models
		boundModels.db = tx
		boundModels.repo = repository.NewModelRepository(tx)
		boundModels.kbRepo = repository.NewKnowledgeBaseRepository(tx)
		copy.models = &boundModels
	}
	copy.tasks = &transactionTaskEnqueuer{queue: queue, ctx: ctx, tx: tx}
	return &copy, nil
}

func (r *EntityNameEmbeddingRebuilder) EnqueueTx(ctx context.Context, tx *gorm.DB, tenantID uint64, reason string) error {
	copy, err := r.WithTransaction(ctx, tx)
	if err != nil {
		return err
	}
	return copy.Enqueue(ctx, tenantID, reason)
}

func (r *EntityNameEmbeddingRebuilder) BindTransaction(ctx context.Context, tx *gorm.DB) (EntityNameEmbeddingRebuildTrigger, error) {
	return r.WithTransaction(ctx, tx)
}
