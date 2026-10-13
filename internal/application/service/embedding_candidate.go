package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type candidateVector struct {
	RunID          string
	Kind           string
	SourceID       string
	AttachmentPath string
	EntryID        string
	LibraryID      string
	ModelID        string
	Dimension      int
	Embedding      pgvector.HalfVector
	ContentHash    string
}

func (candidateVector) TableName() string { return "embedding_rebuild_vectors" }

func (r *EmbeddingRebuilder) rebuildProfile(ctx context.Context, spaceID uint64, target string) (entityEmbeddingProfile, error) {
	if target == "" {
		return r.profiles.profile(ctx, spaceID)
	}
	return r.profiles.profileForModel(ctx, spaceID, target, false)
}
func (r *EmbeddingRebuilder) checkRebuildProfile(ctx context.Context, db *gorm.DB, spaceID uint64, p entityEmbeddingProfile, candidate bool) error {
	if !candidate {
		return r.profiles.checkProfile(db.WithContext(ctx), spaceID, p)
	}
	var model types.Model
	if err := db.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND (space_id=? OR is_builtin=true)", p.modelID, spaceID).First(&model).Error; err != nil {
		return err
	}
	if model.Type != types.ModelTypeEmbedding || model.Status != types.ModelStatusActive || types.EmbeddingProfileHash(p.modelID, p.dimension, &model) != p.hash {
		return errors.New("candidate embedding profile changed")
	}
	return nil
}
func (r *EmbeddingRebuilder) StartCandidateAndEnqueue(ctx context.Context, spaceID uint64, modelID string) (*types.EmbeddingRebuildRun, error) {
	copy := *r
	copy.targetModelID = modelID
	return copy.StartAndEnqueue(ctx, spaceID)
}

func (r *EmbeddingRebuilder) candidateContent(ctx context.Context, db *gorm.DB, spaceID uint64, item types.EmbeddingRebuildItem) (string, *types.Entry, error) {
	if item.Kind == "entity" {
		var entity types.Entity
		if err := db.WithContext(ctx).Where("space_id=? AND id=? AND merged_into_id=''", spaceID, item.SourceID).First(&entity).Error; err != nil {
			return "", nil, err
		}
		return entityNameEmbeddingText(entity.CanonicalName, entity.EntityType, entity.Description), nil, nil
	}
	entryID := item.SourceID
	var chunk types.Chunk
	if strings.HasSuffix(item.Kind, "_chunk") {
		if err := db.WithContext(ctx).Where("space_id=? AND id=?", spaceID, item.SourceID).First(&chunk).Error; err != nil {
			return "", nil, err
		}
		entryID = chunk.EntryID
	}
	var entry types.Entry
	if err := db.WithContext(ctx).Where("space_id=? AND id=? AND parse_status=?", spaceID, entryID, types.ParseStatusCompleted).First(&entry).Error; err != nil {
		return "", nil, err
	}
	if item.Kind == "article" {
		if entry.StructuredSummary == nil {
			return "", nil, errors.New("summary removed during rebuild")
		}
		return strings.TrimSpace(entry.Title) + "\n" + entry.StructuredSummary.RenderText(), &entry, nil
	}
	if item.Kind == "image" {
		if r.vault == nil {
			return "", nil, errors.New("image store unavailable")
		}
		f, err := r.vault.ReadImage(&entry, item.AttachmentPath)
		if err != nil {
			return "", nil, err
		}
		defer f.Close()
		data, err := io.ReadAll(f)
		if err != nil {
			return "", nil, err
		}
		return "data:" + http.DetectContentType(data) + ";base64," + base64.StdEncoding.EncodeToString(data), &entry, nil
	}
	return buildEntryIndexContent(&entry, chunk.Content), &entry, nil
}
func (r *EmbeddingRebuilder) prepareCandidateItem(ctx context.Context, run *types.EmbeddingRebuildRun, item types.EmbeddingRebuildItem, p entityEmbeddingProfile) (embeddingItemWrite, error) {
	content, entry, err := r.candidateContent(ctx, r.db, run.SpaceID, item)
	if err != nil {
		return nil, err
	}
	var vector []float32
	if item.Kind == "image" {
		imageModel, ok := p.embedder.(embedding.ImageEmbedder)
		if !ok || !imageModel.SupportsImages() {
			return nil, errors.New("candidate model cannot embed images")
		}
		vector, err = imageModel.EmbedImage(ctx, content)
	} else {
		vector, err = rebuildTextVector(ctx, p, content)
	}
	if err != nil {
		return nil, err
	}
	if len(vector) != p.dimension {
		return nil, errors.New("candidate dimension mismatch")
	}
	row := candidateVector{RunID: run.ID, Kind: item.Kind, SourceID: item.SourceID, AttachmentPath: item.AttachmentPath, ModelID: run.ModelID, Dimension: run.Dimension, Embedding: pgvector.NewHalfVector(vector), ContentHash: vaultContentHash(content)}
	if entry != nil {
		row.EntryID, row.LibraryID = entry.ID, entry.LibraryID
	}
	return func(tx *gorm.DB) error {
		current, _, err := r.candidateContent(ctx, tx, run.SpaceID, item)
		if err != nil {
			return err
		}
		if vaultContentHash(current) != row.ContentHash {
			return errors.New("candidate source changed; retry item")
		}
		return tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "run_id"}, {Name: "kind"}, {Name: "source_id"}, {Name: "attachment_path"}}, UpdateAll: true}).Create(&row).Error
	}, nil
}

// SwitchCandidate publishes all channels in one transaction. Readers holding a
// shared space lock finish on the old set before this transaction can publish.
func (r *EmbeddingRebuilder) SwitchCandidate(ctx context.Context, spaceID uint64, runID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var space types.Space
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", spaceID).First(&space).Error; err != nil {
			return err
		}
		var run types.EmbeddingRebuildRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", runID, spaceID).First(&run).Error; err != nil {
			return err
		}
		if !run.Candidate || run.Status != "completed" || run.SwitchedAt != nil || run.CompletedCount != run.TotalCount || run.FailedCount != 0 {
			return apperrors.NewBadRequestError("candidate vectors must be complete and not yet switched")
		}
		// Exclude concurrent source edits and index writes during coverage validation.
		if err := tx.Exec("LOCK TABLE entries,chunks,embeddings,article_embeddings,image_embeddings,entities IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var model types.Model
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id=?", run.ModelID).First(&model).Error; err != nil {
			return err
		}
		if types.EmbeddingProfileHash(run.ModelID, run.Dimension, &model) != run.ProfileHash {
			return apperrors.NewBadRequestError("candidate model changed")
		}
		var vectors []candidateVector
		if err := tx.Where("run_id=?", run.ID).Find(&vectors).Error; err != nil {
			return err
		}
		if int64(len(vectors)) != run.TotalCount {
			return apperrors.NewBadRequestError("candidate vectors are incomplete")
		}
		var count int64
		if err := tx.Table("chunks c").Joins("JOIN entries k ON k.id=c.entry_id AND k.space_id=c.space_id").Where("k.space_id=? AND k.deleted_at IS NULL AND k.parse_status='completed' AND c.deleted_at IS NULL AND c.chunk_type IN ('text','summary','table_summary','table_column')", spaceID).Count(&count).Error; err != nil {
			return err
		}
		chunkCount := int64(0)
		for _, v := range vectors {
			if strings.HasSuffix(v.Kind, "_chunk") {
				chunkCount++
			}
			content, _, err := r.candidateContent(ctx, tx, spaceID, types.EmbeddingRebuildItem{Kind: v.Kind, SourceID: v.SourceID, AttachmentPath: v.AttachmentPath})
			if err != nil || vaultContentHash(content) != v.ContentHash {
				return apperrors.NewBadRequestError("sources changed during rebuild; rebuild before switching")
			}
		}
		if count != chunkCount {
			return apperrors.NewBadRequestError("chunks changed during rebuild; rebuild before switching")
		}
		if err := tx.Exec(`INSERT INTO embedding_vector_archives(run_id,space_id,payload)
   SELECT ?,?,jsonb_build_object(
   'embeddings',COALESCE((SELECT jsonb_agg(to_jsonb(e)) FROM embeddings e JOIN entries k ON k.id=e.entry_id WHERE k.space_id=?),'[]'::jsonb),
   'article_embeddings',COALESCE((SELECT jsonb_agg(to_jsonb(a)) FROM article_embeddings a WHERE a.space_id=?),'[]'::jsonb),
   'image_embeddings',COALESCE((SELECT jsonb_agg(to_jsonb(i)) FROM image_embeddings i WHERE i.space_id=?),'[]'::jsonb),
   'entities',COALESCE((SELECT jsonb_agg(jsonb_build_object('id',id,'name_embedding',name_embedding,'embedding_model_id',embedding_model_id,'embedding_dimension',embedding_dimension)) FROM entities WHERE space_id=?),'[]'::jsonb))`, run.ID, spaceID, spaceID, spaceID, spaceID, spaceID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM embeddings WHERE entry_id IN (SELECT id FROM entries WHERE space_id=?)", spaceID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM article_embeddings WHERE space_id=?", spaceID).Error; err != nil {
			return err
		}
		if err := tx.Exec("DELETE FROM image_embeddings WHERE space_id=?", spaceID).Error; err != nil {
			return err
		}
		for _, v := range vectors {
			var err error
			switch {
			case strings.HasSuffix(v.Kind, "_chunk"):
				err = tx.Exec(`INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,library_id,model_id,dimension,embedding,is_enabled)
    SELECT id,0,id,entry_id,library_id,?,?,?,is_enabled FROM chunks WHERE id=?`, v.ModelID, v.Dimension, v.Embedding, v.SourceID).Error
			case v.Kind == "article":
				err = tx.Exec(`INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash) VALUES (?,?,?,?,?,?,?)`, v.EntryID, spaceID, v.LibraryID, v.ModelID, v.Dimension, v.Embedding, v.ContentHash).Error
			case v.Kind == "image":
				err = tx.Exec(`INSERT INTO image_embeddings(entry_id,space_id,library_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES (?,?,?,?,?,?,?,?)`, v.EntryID, spaceID, v.LibraryID, v.AttachmentPath, v.ModelID, v.Dimension, v.Embedding, v.ContentHash).Error
			case v.Kind == "entity":
				err = tx.Model(&types.Entity{}).Where("id=? AND space_id=?", v.SourceID, spaceID).Updates(map[string]any{"name_embedding": types.EntityNameEmbedding(v.Embedding.Slice()), "embedding_model_id": v.ModelID, "embedding_dimension": v.Dimension, "embedding_needs_rebuild": false}).Error
			default:
				return fmt.Errorf("unknown vector kind %s", v.Kind)
			}
			if err != nil {
				return err
			}
		}
		if err := tx.Model(&types.Space{}).Where("id=?", spaceID).Update("embedding_model_id", run.ModelID).Error; err != nil {
			return err
		}
		if err := tx.Model(&types.Entry{}).Where("space_id=?", spaceID).Update("embedding_model_id", run.ModelID).Error; err != nil {
			return err
		}
		return tx.Model(&run).Update("switched_at", time.Now()).Error
	})
}
func (r *EmbeddingRebuilder) DeleteOldVectors(ctx context.Context, spaceID uint64, runID string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run types.EmbeddingRebuildRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", runID, spaceID).First(&run).Error; err != nil {
			return err
		}
		if run.SwitchedAt == nil {
			return apperrors.NewBadRequestError("switch candidate vectors before deleting old vectors")
		}
		return tx.Exec("DELETE FROM embedding_vector_archives WHERE run_id=? AND space_id=?", runID, spaceID).Error
	})
}

// CancelCandidate fences queued and in-flight attempts before a fresh snapshot.
func (r *EmbeddingRebuilder) CancelCandidate(ctx context.Context, spaceID uint64, runID string) error {
	result := r.db.WithContext(ctx).Model(&types.EmbeddingRebuildRun{}).Where("id=? AND space_id=? AND candidate=true AND switched_at IS NULL AND status IN ('pending','running','paused')", runID, spaceID).Updates(map[string]any{"status": "cancelled", "attempt_id": "", "lease_until": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.NewBadRequestError("candidate is not pending, running or paused")
	}
	return nil
}
