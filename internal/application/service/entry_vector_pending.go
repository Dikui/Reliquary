package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"time"
)

func imageVectorsEnabled(ctx context.Context, db *gorm.DB, spaceID uint64) bool {
	if db == nil {
		return false
	}
	var space types.Space
	return db.WithContext(ctx).Select("retrieval_config").Where("id=?", spaceID).Find(&space).Error == nil && space.RetrievalConfig != nil && space.RetrievalConfig.ImageVectorEnabled
}
func (s *entryService) deferEntryVectors(ctx context.Context, entry *types.Entry, cause error) error {
	if utils.IsPermanentImageError(cause) {
		return cause
	}
	if ctx.Value(libraryRebuildContextKey{}) == true {
		return fmt.Errorf("library rebuild vectors are incomplete; retry this processing task: %w", cause)
	}
	if s.task == nil {
		return errors.New("vector pending operation queue unavailable")
	}
	data, _ := json.Marshal(struct {
		SpaceID uint64 `json:"space_id"`
		EntryID string `json:"entry_id"`
	}{entry.SpaceID, entry.ID})
	_, err := s.task.Enqueue(taskqueue.NewTask(types.TypeEntryVector, data), taskqueue.Queue(types.QueueEntity), taskqueue.ProcessAt(time.Now().Add(time.Minute)))
	return err
}
func (r *EmbeddingRebuilder) HandleEntryVector(ctx context.Context, task *taskqueue.Task) error {
	var payload struct {
		SpaceID uint64 `json:"space_id"`
		EntryID string `json:"entry_id"`
	}
	if task == nil || json.Unmarshal(task.Payload(), &payload) != nil || payload.SpaceID != types.SingleSpaceID || payload.EntryID == "" {
		return taskqueue.SkipRetry
	}
	ctx = types.WithModelTier(types.WithBackgroundTask(context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)), types.ModelTierEmbedding)
	err := r.indexEntryVectors(ctx, payload.SpaceID, payload.EntryID)
	if err != nil {
		if utils.IsPermanentImageError(err) {
			return fmt.Errorf("%w: %w", err, taskqueue.SkipRetry)
		}
		return &types.VectorDeferredError{Cause: err, RetryAt: time.Now().Add(time.Minute)}
	}
	return nil
}
func (r *EmbeddingRebuilder) indexEntryVectors(ctx context.Context, spaceID uint64, entryID string) error {
	var entry types.Entry
	if err := r.db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, entryID).First(&entry).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	profile, err := r.profiles.profile(ctx, spaceID)
	if err != nil {
		return err
	}
	var chunks []types.Chunk
	if err := r.db.WithContext(ctx).Where("space_id=? AND entry_id=? AND chunk_type IN ('text','summary','table_summary','table_column')", spaceID, entryID).Find(&chunks).Error; err != nil {
		return err
	}
	run := &types.EmbeddingRebuildRun{SpaceID: spaceID, ModelID: profile.modelID, Dimension: profile.dimension}
	for _, c := range chunks {
		write, err := r.prepareItem(ctx, run, types.EmbeddingRebuildItem{Kind: "article_chunk", SourceID: c.ID}, profile)
		if err != nil {
			return err
		}
		if write != nil {
			if err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := r.profiles.checkProfile(tx, spaceID, profile); err != nil {
					return err
				}
				return write(tx)
			}); err != nil {
				return err
			}
		}
	}
	persist := func(write embeddingItemWrite, err error) error {
		if err != nil {
			return err
		}
		if write == nil {
			return nil
		}
		return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := r.profiles.checkProfile(tx, spaceID, profile); err != nil {
				return err
			}
			return write(tx)
		})
	}
	if entry.StructuredSummary != nil {
		if err := persist(r.prepareItem(ctx, run, types.EmbeddingRebuildItem{Kind: "article", SourceID: entry.ID}, profile)); err != nil {
			return err
		}
	}
	if imageVectorsEnabled(ctx, r.db, spaceID) {
		for _, path := range articleImagePaths(&entry) {
			if err := persist(prepareArticleImageEmbedding(ctx, r.db, r.vault, profile.embedder, &entry, path, false)); err != nil {
				return err
			}
		}
	}
	return nil
}
