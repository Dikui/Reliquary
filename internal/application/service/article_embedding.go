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
// in mine and reports only have chunk vectors; they have no article summary.
func indexArticleEmbedding(ctx context.Context, db *gorm.DB, models interfaces.ModelService, kb *types.KnowledgeBase, article *types.Knowledge) error {
	if db == nil || article == nil || article.SourceKind == "mine" || article.SourceKind == "report" {
		return nil
	}
	if article.StructuredSummary == nil || kb == nil || !kb.IndexingStrategy.VectorEnabled {
		return db.WithContext(ctx).Exec("DELETE FROM article_embeddings WHERE tenant_id=? AND knowledge_id=?", article.TenantID, article.ID).Error
	}
	embedder, err := GetTierEmbeddingModel(ctx, db, models, kb)
	if err != nil {
		return fmt.Errorf("get article embedding model: %w", err)
	}
	return saveArticleEmbedding(ctx, db, embedder, article)
}

func saveArticleEmbedding(ctx context.Context, db *gorm.DB, embedder embedding.Embedder, article *types.Knowledge) error {
	summary := ""
	if article.StructuredSummary != nil {
		summary = article.StructuredSummary.RenderText()
	}
	content := strings.TrimSpace(article.Title) + "\n" + summary
	hash := vaultContentHash(content)
	var count int64
	if err := db.WithContext(ctx).Table("article_embeddings").
		Where("tenant_id=? AND knowledge_id=? AND model_id=? AND dimension=? AND content_hash=?",
			article.TenantID, article.ID, embedder.GetModelID(), embedder.GetDimensions(), hash).Count(&count).Error; err != nil {
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
		if err := checkEmbeddingProfile(ctx, tx, embedder, article.TenantID); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO article_embeddings
		(tenant_id, knowledge_id, knowledge_base_id, model_id, dimension, embedding, content_hash)
		VALUES (?, ?, ?, ?, ?, ?::halfvec, ?)
		ON CONFLICT (knowledge_id) DO UPDATE SET
		model_id=EXCLUDED.model_id, dimension=EXCLUDED.dimension,
		embedding=EXCLUDED.embedding, content_hash=EXCLUDED.content_hash, updated_at=NOW()`,
			article.TenantID, article.ID, article.KnowledgeBaseID, embedder.GetModelID(), len(vector), pgvector.NewHalfVector(vector), hash).Error
	})
}
