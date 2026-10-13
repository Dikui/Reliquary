package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// indexArticleEmbedding is also used when rebuilding from the vault. Documents
// in reports only have chunk vectors; imported user documents also have summaries.
func indexArticleEmbedding(ctx context.Context, db *gorm.DB, models interfaces.ModelService, library *types.Library, article *types.Entry) error {
	if db == nil || article == nil || article.SourceKind == "report" {
		return nil
	}
	if article.StructuredSummary == nil || library == nil || !library.IndexingStrategy.VectorEnabled {
		return db.WithContext(ctx).Exec("DELETE FROM article_embeddings WHERE space_id=? AND entry_id=?", article.SpaceID, article.ID).Error
	}
	embedder, err := GetTierEmbeddingModel(ctx, db, models, library)
	if err != nil {
		return fmt.Errorf("get article embedding model: %w", err)
	}
	return saveArticleEmbedding(ctx, db, embedder, article)
}

func saveArticleEmbedding(ctx context.Context, db *gorm.DB, embedder embedding.Embedder, article *types.Entry) error {
	summary := ""
	if article.StructuredSummary != nil {
		summary = article.StructuredSummary.RenderText()
	}
	content := strings.TrimSpace(article.Title) + "\n" + summary
	hash := vaultContentHash(content)
	var count int64
	if err := db.WithContext(ctx).Table("article_embeddings").
		Where("space_id=? AND entry_id=? AND model_id=? AND dimension=? AND content_hash=?",
			article.SpaceID, article.ID, embedder.GetModelID(), embedder.GetDimensions(), hash).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	ctx = types.WithLLMCallMetadata(ctx, "article_embedding", "")
	vector, err := embedder.Embed(ctx, content)
	if err != nil {
		return fmt.Errorf("embed article title and summary: %w", err)
	}
	if len(vector) == 0 || len(vector) != embedder.GetDimensions() {
		return fmt.Errorf("article embedding dimension mismatch: got %d, expected %d", len(vector), embedder.GetDimensions())
	}
	var norm float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("article embedding contains a non-finite value")
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return fmt.Errorf("article embedding has zero length")
	}
	return db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := checkEmbeddingProfile(ctx, tx, embedder, article.SpaceID); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO article_embeddings
		(space_id, entry_id, library_id, model_id, dimension, embedding, content_hash)
		VALUES (?, ?, ?, ?, ?, ?::halfvec, ?)
		ON CONFLICT (entry_id) DO UPDATE SET
		model_id=EXCLUDED.model_id, dimension=EXCLUDED.dimension,
		embedding=EXCLUDED.embedding, content_hash=EXCLUDED.content_hash, updated_at=NOW()`,
			article.SpaceID, article.ID, article.LibraryID, embedder.GetModelID(), len(vector), pgvector.NewHalfVector(vector), hash).Error
	})
}
