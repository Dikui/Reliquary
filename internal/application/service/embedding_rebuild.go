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
	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

var errRebuildStopped = errors.New("embedding rebuild paused or superseded")

// EmbeddingRebuilder checkpoints every vector together with its destination
// write. Restarting or retrying never repeats successful checkpoint items.
type EmbeddingRebuilder struct {
	db                              *gorm.DB
	profiles                        *EntityNameEmbeddingRebuilder
	tasks                           interfaces.TaskEnqueuer
	vault                           *VaultService
	preserveEntityProfileTimestamps bool
}

func NewEmbeddingRebuilder(db *gorm.DB, models interfaces.ModelService, tasks interfaces.TaskEnqueuer, vault *VaultService) *EmbeddingRebuilder {
	return &EmbeddingRebuilder{db: db, profiles: NewEntityNameEmbeddingRebuilder(db, models, nil), tasks: tasks, vault: vault}
}

// PreserveEntityProfileTimestamps is configured before starting a maintenance
// rebuild. Vector regeneration must retain the portable entity profile values.
func (r *EmbeddingRebuilder) PreserveEntityProfileTimestamps(preserve bool) {
	r.preserveEntityProfileTimestamps = preserve
}

func (r *EmbeddingRebuilder) Status(ctx context.Context, spaceID uint64) (*types.EmbeddingRebuildStatus, error) {
	var run types.EmbeddingRebuildRun
	err := r.db.WithContext(ctx).Where("space_id=?", spaceID).Order("created_at DESC,id DESC").First(&run).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &types.EmbeddingRebuildStatus{FailedItems: []types.EmbeddingRebuildItem{}}, nil
	}
	if err != nil {
		return nil, err
	}
	return r.statusForRun(ctx, spaceID, &run)
}

// StatusForRun keeps CLI resumes tied to their requested run even when newer
// completed rebuilds exist. The settings API continues using latest Status.
func (r *EmbeddingRebuilder) StatusForRun(ctx context.Context, spaceID uint64, runID string) (*types.EmbeddingRebuildStatus, error) {
	var run types.EmbeddingRebuildRun
	if err := r.db.WithContext(ctx).Where("id=? AND space_id=?", runID, spaceID).First(&run).Error; err != nil {
		return nil, err
	}
	return r.statusForRun(ctx, spaceID, &run)
}

func (r *EmbeddingRebuilder) statusForRun(ctx context.Context, spaceID uint64, run *types.EmbeddingRebuildRun) (*types.EmbeddingRebuildStatus, error) {
	status := &types.EmbeddingRebuildStatus{LatestRun: run, FailedItems: []types.EmbeddingRebuildItem{}}
	currentID, profileErr := ResolveModelTier(context.WithValue(ctx, types.SpaceIDContextKey, spaceID), r.db, nil, types.ModelTierEmbedding)
	if profileErr == nil && currentID == run.ModelID {
		metadata, metadataErr := r.profiles.profileMetadata(ctx, currentID, spaceID)
		status.CurrentModelMatches = metadataErr == nil && entityEmbeddingProfileHash(currentID, run.Dimension, metadata) == run.ProfileHash
	}
	err := r.db.WithContext(ctx).Where("run_id=? AND status='failed'", run.ID).Order("kind,source_id,attachment_path").Limit(100).Find(&status.FailedItems).Error
	return status, err
}

// Start snapshots current articles, documents, entities, and body images. It
// does not change source files or remove vectors before replacements exist.
func (r *EmbeddingRebuilder) Start(ctx context.Context, spaceID uint64) (*types.EmbeddingRebuildRun, error) {
	profile, err := r.profiles.profile(ctx, spaceID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	run := &types.EmbeddingRebuildRun{ID: uuid.NewString(), SpaceID: spaceID, Status: "pending", ModelID: profile.modelID,
		ModelName: profile.embedder.GetModelName(), Dimension: profile.dimension, ProfileHash: profile.hash, CreatedAt: now, UpdatedAt: now}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Serialize creation in the personal library while retaining its
		// normal keyword and vector queries throughout the rebuild.
		var library types.Library
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=?", spaceID).First(&library).Error; err != nil {
			return err
		}
		if err := r.profiles.checkProfile(tx, spaceID, profile); err != nil {
			return err
		}
		if err := tx.Model(&types.EmbeddingRebuildRun{}).Where("space_id=? AND profile_hash<>? AND (status IN ('pending','paused') OR (status='running' AND (lease_until IS NULL OR lease_until<=?)))", spaceID, profile.hash, time.Now()).
			Updates(map[string]any{"status": "failed", "error": "embedding model changed; superseded by a new rebuild"}).Error; err != nil {
			return err
		}
		var active int64
		if err := tx.Model(&types.EmbeddingRebuildRun{}).Where("space_id=? AND status IN ('pending','running','paused')", spaceID).Count(&active).Error; err != nil {
			return err
		}
		if active > 0 {
			return apperrors.NewBadRequestError("resume or finish the existing embedding rebuild first")
		}
		if err := tx.Create(run).Error; err != nil {
			return err
		}
		var chunks []struct{ ID, SourceKind string }
		if err := tx.Table("chunks c").Joins("JOIN entries k ON k.id=c.entry_id AND k.space_id=c.space_id").
			Where("k.space_id=? AND k.deleted_at IS NULL AND k.parse_status=? AND k.processing_mode='full' AND c.deleted_at IS NULL AND c.chunk_type IN ('text','summary','table_summary','table_column')", spaceID, types.ParseStatusCompleted).
			Select("c.id,k.source_kind").Order("c.id").Scan(&chunks).Error; err != nil {
			return err
		}
		items := make([]types.EmbeddingRebuildItem, 0, len(chunks))
		add := func(kind, sourceID, path string) {
			items = append(items, types.EmbeddingRebuildItem{ID: uuid.NewString(), RunID: run.ID, Kind: kind,
				SourceID: sourceID, AttachmentPath: path, Status: "pending", CreatedAt: now, UpdatedAt: now})
		}
		for _, chunk := range chunks {
			kind := "article_chunk"
			if chunk.SourceKind == "mine" || chunk.SourceKind == "report" {
				kind = chunk.SourceKind + "_chunk"
			}
			add(kind, chunk.ID, "")
		}
		var articles []types.Entry
		if err := tx.Where("space_id=? AND source_kind IN ('article','mine') AND parse_status=?", spaceID, types.ParseStatusCompleted).Order("id").Find(&articles).Error; err != nil {
			return err
		}
		imageModel, imagesSupported := profile.embedder.(embedding.ImageEmbedder)
		for i := range articles {
			article := &articles[i]
			if article.StructuredSummary != nil {
				add("article", article.ID, "")
			}
			if imagesSupported && imageModel.SupportsImages() && article.ProcessingMode == types.ProcessingModeFull {
				for _, path := range articleImagePaths(article) {
					add("image", article.ID, path)
				}
			}
		}
		var entities []types.Entity
		if err := tx.Select("id").Where("space_id=? AND merged_into_id=''", spaceID).Order("id").Find(&entities).Error; err != nil {
			return err
		}
		for _, entity := range entities {
			add("entity", entity.ID, "")
		}
		if len(items) > 0 {
			if err := tx.CreateInBatches(items, 500).Error; err != nil {
				return err
			}
		}
		run.TotalCount = int64(len(items))
		return tx.Model(run).Update("total_count", run.TotalCount).Error
	})
	return run, err
}

// Continue validates model identity before keeping successful checkpoints and
// optionally making only failed items pending again.
func (r *EmbeddingRebuilder) Continue(ctx context.Context, spaceID uint64, runID string, retryFailed bool) error {
	profile, err := r.profiles.profile(ctx, spaceID)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run types.EmbeddingRebuildRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", runID, spaceID).First(&run).Error; err != nil {
			return err
		}
		if run.ModelID != profile.modelID || run.Dimension != profile.dimension || run.ProfileHash != profile.hash {
			return apperrors.NewBadRequestError("embedding model changed; start a new rebuild")
		}
		if run.Status == "running" && run.LeaseUntil != nil && run.LeaseUntil.After(time.Now()) {
			return apperrors.NewBadRequestError("pause the running embedding rebuild before resuming it")
		}
		if run.Status == "completed" {
			return apperrors.NewBadRequestError("embedding rebuild is already complete")
		}
		if retryFailed {
			if err := tx.Model(&types.EmbeddingRebuildItem{}).Where("run_id=? AND status='failed'", run.ID).
				Updates(map[string]any{"status": "pending", "error": ""}).Error; err != nil {
				return err
			}
			run.FailedCount = 0
		}
		return tx.Model(&run).Updates(map[string]any{"status": "pending", "error": "", "failed_count": run.FailedCount,
			"attempt_id": "", "lease_until": nil, "finished_at": nil}).Error
	})
}

func (r *EmbeddingRebuilder) Pause(ctx context.Context, spaceID uint64, runID string) error {
	result := r.db.WithContext(ctx).Model(&types.EmbeddingRebuildRun{}).Where("space_id=? AND id=? AND status IN ('pending','running')", spaceID, runID).
		Updates(map[string]any{"status": "paused", "attempt_id": "", "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.NewBadRequestError("embedding rebuild is not pending or running")
	}
	return nil
}

func (r *EmbeddingRebuilder) Enqueue(ctx context.Context, run *types.EmbeddingRebuildRun) error {
	payload, _ := json.Marshal(types.EmbeddingRebuildPayload{SpaceID: run.SpaceID, RunID: run.ID})
	var err error
	if r.tasks == nil {
		err = errors.New("embedding rebuild task queue is unavailable")
	} else {
		_, err = r.tasks.Enqueue(taskqueue.NewTask(types.TypeEmbeddingRebuild, payload), taskqueue.Queue(types.QueueEntity), taskqueue.MaxRetry(3), taskqueue.Timeout(24*time.Hour))
	}
	if err != nil {
		persistErr := r.db.WithContext(ctx).Model(&types.EmbeddingRebuildRun{}).Where("id=? AND status='pending'", run.ID).
			Updates(map[string]any{"status": "failed", "error": rebuildErrorText(err)}).Error
		return errors.Join(err, persistErr)
	}
	return nil
}

func (r *EmbeddingRebuilder) Handle(ctx context.Context, task *taskqueue.Task) error {
	var payload types.EmbeddingRebuildPayload
	if task == nil || json.Unmarshal(task.Payload(), &payload) != nil || payload.SpaceID != types.SingleSpaceID || payload.RunID == "" {
		return fmt.Errorf("invalid embedding rebuild payload: %w", taskqueue.SkipRetry)
	}
	return r.Run(ctx, payload.SpaceID, payload.RunID, nil)
}

func (r *EmbeddingRebuilder) Run(ctx context.Context, spaceID uint64, runID string, progress func(*types.EmbeddingRebuildRun)) (runErr error) {
	ctx = types.WithLLMCallMetadata(types.WithModelTier(types.WithBackgroundTask(context.WithValue(ctx, types.SpaceIDContextKey, spaceID)), types.ModelTierEmbedding), "embedding_rebuild", "")
	var run types.EmbeddingRebuildRun
	claimed := false
	if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", runID, spaceID).First(&run).Error; err != nil {
			return err
		}
		if run.Status == "paused" || run.Status == "completed" || run.Status == "completed_with_errors" ||
			(run.Status == "running" && run.LeaseUntil != nil && run.LeaseUntil.After(time.Now())) {
			return nil
		}
		now := time.Now().UTC()
		run.AttemptID = uuid.NewString()
		if run.StartedAt == nil {
			run.StartedAt = &now
		}
		claimed = true
		return tx.Model(&run).Updates(map[string]any{"status": "running", "attempt_id": run.AttemptID, "lease_until": now.Add(2 * time.Minute),
			"started_at": run.StartedAt, "finished_at": nil, "error": ""}).Error
	}); err != nil {
		return err
	}
	if !claimed {
		return nil
	}
	defer func() {
		persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		status, detail := "completed", ""
		if runErr != nil {
			status, detail = "failed", rebuildErrorText(runErr)
			if errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				status = "paused"
			}
		} else if run.FailedCount > 0 {
			status = "completed_with_errors"
		}
		now := time.Now().UTC()
		result := r.db.WithContext(persistCtx).Model(&types.EmbeddingRebuildRun{}).Where("id=? AND space_id=? AND attempt_id=?", run.ID, spaceID, run.AttemptID).
			Updates(map[string]any{"status": status, "error": detail, "lease_until": nil, "finished_at": now})
		if result.Error != nil {
			runErr = errors.Join(runErr, result.Error)
		}
		if errors.Is(runErr, errRebuildStopped) {
			runErr = nil
		}
	}()
	profile, err := r.profiles.profile(ctx, spaceID)
	if err != nil {
		return err
	}
	if run.ModelID != profile.modelID || run.Dimension != profile.dimension || run.ProfileHash != profile.hash {
		return errors.New("embedding model changed; start a new rebuild")
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := r.checkRun(ctx, r.db, &run, profile, false); err != nil {
			return err
		}
		var item types.EmbeddingRebuildItem
		err := r.db.WithContext(ctx).Where("run_id=? AND status='pending'", run.ID).Order("kind,source_id,attachment_path").First(&item).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			break
		}
		if err != nil {
			return err
		}
		write, embedErr := r.prepareItem(ctx, &run, item, profile)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := r.checkRun(ctx, tx, &run, profile, true); err != nil {
				return err
			}
			if embedErr == nil && write != nil {
				// A savepoint keeps item-local write failures retryable without
				// losing the checkpoint after PostgreSQL aborts that statement.
				if err := tx.SavePoint("embedding_item").Error; err != nil {
					return err
				}
				if err := write(tx); err != nil {
					if rollbackErr := tx.RollbackTo("embedding_item").Error; rollbackErr != nil {
						return rollbackErr
					}
					embedErr = err
				}
			}
			item.Status, item.Error = "completed", ""
			counter := "completed_count"
			if embedErr != nil {
				item.Status, item.Error = "failed", rebuildErrorText(embedErr)
				counter = "failed_count"
			}
			if err := tx.Model(&item).Updates(map[string]any{"status": item.Status, "error": item.Error, "attempts": gorm.Expr("attempts+1")}).Error; err != nil {
				return err
			}
			return tx.Model(&types.EmbeddingRebuildRun{}).Where("id=? AND attempt_id=?", run.ID, run.AttemptID).
				Updates(map[string]any{counter: gorm.Expr(counter + "+1"), "lease_until": time.Now().Add(2 * time.Minute)}).Error
		}); err != nil {
			return err
		}
		if embedErr != nil {
			run.FailedCount++
		} else {
			run.CompletedCount++
		}
		if progress != nil {
			progress(&run)
		}
	}
	return nil
}

func (r *EmbeddingRebuilder) checkRun(ctx context.Context, db *gorm.DB, run *types.EmbeddingRebuildRun, profile entityEmbeddingProfile, lock bool) error {
	query := db.WithContext(ctx)
	if lock {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var current types.EmbeddingRebuildRun
	if err := query.Where("id=? AND space_id=?", run.ID, run.SpaceID).First(&current).Error; err != nil {
		return err
	}
	if current.Status != "running" || current.AttemptID != run.AttemptID {
		return errRebuildStopped
	}
	return r.profiles.checkProfile(db.WithContext(ctx), run.SpaceID, profile)
}

type embeddingItemWrite func(*gorm.DB) error

func (r *EmbeddingRebuilder) prepareItem(ctx context.Context, run *types.EmbeddingRebuildRun, item types.EmbeddingRebuildItem, profile entityEmbeddingProfile) (embeddingItemWrite, error) {
	if item.Kind == "entity" {
		var entity types.Entity
		if err := r.db.WithContext(ctx).Where("space_id=? AND id=? AND merged_into_id=''", run.SpaceID, item.SourceID).First(&entity).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		vector, err := rebuildTextVector(ctx, profile, entityNameEmbeddingText(entity.CanonicalName, entity.EntityType, entity.Description))
		if err != nil {
			return nil, err
		}
		return func(tx *gorm.DB) error {
			if err := lockEntitySpaceShared(tx, run.SpaceID); err != nil {
				return err
			}
			query := tx.Model(&types.Entity{}).Where("id=? AND space_id=? AND merged_into_id='' AND canonical_name=? AND entity_type=? AND description=?", entity.ID, run.SpaceID, entity.CanonicalName, entity.EntityType, entity.Description)
			updates := map[string]any{"name_embedding": types.EntityNameEmbedding(vector), "embedding_model_id": run.ModelID, "embedding_dimension": run.Dimension, "embedding_needs_rebuild": false}
			if r.preserveEntityProfileTimestamps {
				query = query.Where("updated_at=?", entity.UpdatedAt)
				updates["updated_at"] = entity.UpdatedAt
			}
			result := query.Updates(updates)
			return rebuildSourceWriteResult(result)
		}, nil
	}
	var article types.Entry
	var chunk types.Chunk
	entryID := item.SourceID
	if strings.HasSuffix(item.Kind, "_chunk") {
		if err := r.db.WithContext(ctx).Where("space_id=? AND id=?", run.SpaceID, item.SourceID).First(&chunk).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
		entryID = chunk.EntryID
	}
	if err := r.db.WithContext(ctx).Where("space_id=? AND id=? AND parse_status=?", run.SpaceID, entryID, types.ParseStatusCompleted).First(&article).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if item.Kind == "image" {
		if article.ProcessingMode != types.ProcessingModeFull {
			return nil, nil
		}
		return prepareArticleImageEmbedding(ctx, r.db, r.vault, profile.embedder, &article, item.AttachmentPath, true)
	}
	content := buildEntryIndexContent(&article, chunk.Content)
	if item.Kind == "article" {
		if article.StructuredSummary == nil {
			return nil, nil
		}
		content = strings.TrimSpace(article.Title) + "\n" + article.StructuredSummary.RenderText()
	} else if !strings.HasSuffix(item.Kind, "_chunk") {
		return nil, fmt.Errorf("unsupported embedding rebuild kind %q", item.Kind)
	}
	vector, err := rebuildTextVector(ctx, profile, content)
	if err != nil {
		return nil, err
	}
	hash := vaultContentHash(content)
	return func(tx *gorm.DB) error {
		var current types.Entry
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND id=?", run.SpaceID, article.ID).First(&current).Error; err != nil {
			return err
		}
		if item.Kind == "article" {
			if current.StructuredSummary == nil || vaultContentHash(strings.TrimSpace(current.Title)+"\n"+current.StructuredSummary.RenderText()) != hash {
				return errors.New("article changed while embedding; retry this item")
			}
			return tx.Exec(`INSERT INTO article_embeddings (space_id,entry_id,library_id,model_id,dimension,embedding,content_hash)
				VALUES (?,?,?,?,?,?::halfvec,?) ON CONFLICT (entry_id) DO UPDATE SET model_id=EXCLUDED.model_id,
				dimension=EXCLUDED.dimension,embedding=EXCLUDED.embedding,content_hash=EXCLUDED.content_hash,updated_at=NOW()`,
				run.SpaceID, article.ID, article.LibraryID, run.ModelID, run.Dimension, pgvector.NewHalfVector(vector), hash).Error
		}
		var currentChunk types.Chunk
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("space_id=? AND id=?", run.SpaceID, chunk.ID).First(&currentChunk).Error; err != nil {
			return err
		}
		if current.ProcessingMode != types.ProcessingModeFull || vaultContentHash(buildEntryIndexContent(&current, currentChunk.Content)) != hash {
			return errors.New("document chunk changed while embedding; retry this item")
		}
		if err := tx.Exec(`INSERT INTO embeddings (source_id,source_type,chunk_id,entry_id,library_id,model_id,dimension,embedding,is_enabled)
			VALUES (?,0,?,?,?,?,?,?::halfvec,?) ON CONFLICT (source_id,source_type) DO UPDATE SET model_id=EXCLUDED.model_id,
			dimension=EXCLUDED.dimension,embedding=EXCLUDED.embedding,is_enabled=EXCLUDED.is_enabled,updated_at=NOW()`,
			chunk.ID, chunk.ID, article.ID, article.LibraryID, run.ModelID, run.Dimension, pgvector.NewHalfVector(vector), chunk.IsEnabled).Error; err != nil {
			return err
		}
		// The document marker becomes current only after all its chunk vectors
		// have matching identities. Per-vector retrieval can use each earlier.
		return tx.Exec(`UPDATE entries SET embedding_model_id=? WHERE space_id=? AND id=? AND NOT EXISTS (
			SELECT 1 FROM chunks c LEFT JOIN embeddings e ON e.chunk_id=c.id AND e.source_type=0
			WHERE c.entry_id=entries.id AND c.space_id=entries.space_id AND c.deleted_at IS NULL
			AND c.chunk_type IN ('text','summary','table_summary','table_column')
			AND (e.id IS NULL OR e.model_id<>? OR e.dimension<>?))`, run.ModelID, run.SpaceID, article.ID, run.ModelID, run.Dimension).Error
	}, nil
}

func rebuildTextVector(ctx context.Context, profile entityEmbeddingProfile, content string) ([]float32, error) {
	types.RecordModelCall(ctx, profile.modelID)
	vector, err := profile.embedder.Embed(ctx, content)
	if err != nil {
		return nil, err
	}
	if len(vector) != profile.dimension {
		return nil, fmt.Errorf("embedding dimension mismatch: got %d, expected %d", len(vector), profile.dimension)
	}
	var norm float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, errors.New("embedding contains a non-finite value")
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return nil, errors.New("embedding has zero length")
	}
	return vector, nil
}

func rebuildSourceWriteResult(result *gorm.DB) error {
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return errors.New("embedding source changed; retry this item")
	}
	return nil
}

func rebuildErrorText(err error) string {
	if err == nil {
		return ""
	}
	value := []rune(err.Error())
	return string(value[:min(1000, len(value))])
}

// StartAndEnqueue commits the rebuild snapshot with its background task.
func (r *EmbeddingRebuilder) StartAndEnqueue(ctx context.Context, spaceID uint64) (run *types.EmbeddingRebuildRun, err error) {
	queue, ok := r.tasks.(interfaces.TransactionalTaskEnqueuer)
	if !ok {
		return nil, errors.New("embedding task queue must support PostgreSQL transactions")
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		copy := *r
		profiles := *r.profiles
		copy.db, profiles.db = tx, tx
		copy.profiles = &profiles
		copy.tasks = &transactionTaskEnqueuer{queue: queue, ctx: ctx, tx: tx}
		var err error
		run, err = copy.Start(ctx, spaceID)
		if err != nil {
			return err
		}
		return copy.Enqueue(ctx, run)
	})
	return run, err
}

// ContinueAndEnqueue makes a resume checkpoint and its task visible together.
func (r *EmbeddingRebuilder) ContinueAndEnqueue(ctx context.Context, spaceID uint64, runID string, retryFailed bool) error {
	queue, ok := r.tasks.(interfaces.TransactionalTaskEnqueuer)
	if !ok {
		return errors.New("embedding task queue must support PostgreSQL transactions")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		copy := *r
		profiles := *r.profiles
		copy.db, profiles.db = tx, tx
		copy.profiles = &profiles
		copy.tasks = &transactionTaskEnqueuer{queue: queue, ctx: ctx, tx: tx}
		if err := copy.Continue(ctx, spaceID, runID, retryFailed); err != nil {
			return err
		}
		return copy.Enqueue(ctx, &types.EmbeddingRebuildRun{ID: runID, SpaceID: spaceID})
	})
}
