package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"path/filepath"
	"slices"
	"strings"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/embedding"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// articleImagePaths uses the saved body rather than parser output. Only images
// copied into this article's attachment directory are eligible for indexing.
// Reports keep their separate indexing path.
func articleImagePaths(article *types.Entry) []string {
	paths := []string{}
	if !isIngestArticle(article) || article.ProcessingMode != types.ProcessingModeFull || article.VaultPath == "" {
		return paths
	}
	add := func(reference string) {
		if reference == "" || filepath.IsAbs(reference) || strings.Contains(reference, "\\") {
			return
		}
		relative := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(filepath.FromSlash(article.VaultPath)), filepath.FromSlash(reference))))
		if strings.HasPrefix(relative, "attachments/"+article.ID+"/") && !slices.Contains(paths, reference) {
			paths = append(paths, reference)
		}
	}
	for _, reference := range docparser.MarkdownImageReferences(article.VaultBody) {
		add(reference.URL)
	}
	for _, match := range searchutil.HTMLImageSrcRegex.FindAllStringSubmatch(article.VaultBody, -1) {
		if len(match) > searchutil.HTMLImageSrcURLGroup {
			add(match[searchutil.HTMLImageSrcURLGroup])
		}
	}
	slices.Sort(paths)
	return paths
}

func indexArticleImages(ctx context.Context, db *gorm.DB, models interfaces.ModelService, kb *types.KnowledgeBase, vault *VaultService, article *types.Entry) error {
	if db == nil || article == nil {
		return nil
	}
	paths := articleImagePaths(article)
	query := db.WithContext(ctx).Table("image_embeddings").Where("space_id=? AND entry_id=?", article.SpaceID, article.ID)
	if len(paths) == 0 || kb == nil || !kb.IsVectorEnabled() || vault == nil {
		return query.Delete(nil).Error
	}
	embedder, err := GetTierEmbeddingModel(ctx, db, models, kb)
	if err != nil {
		return fmt.Errorf("get image embedding model: %w", err)
	}
	images, ok := embedder.(embedding.ImageEmbedder)
	if !ok || !images.SupportsImages() {
		return query.Delete(nil).Error
	}
	for _, path := range paths {
		if err := saveArticleImageEmbedding(ctx, db, vault, embedder, article, path, false); err != nil {
			return err
		}
	}
	return query.Where("attachment_path NOT IN ?", paths).Delete(nil).Error
}

// saveArticleImageEmbedding supports both ingestion and explicit rebuild. A
// rebuild sets force so every item calls the current model even if its bytes
// have not changed. The existing vector is replaced only after success.
func saveArticleImageEmbedding(ctx context.Context, db *gorm.DB, vault *VaultService, embedder embedding.Embedder, article *types.Entry, attachmentPath string, force bool) error {
	write, err := prepareArticleImageEmbedding(ctx, db, vault, embedder, article, attachmentPath, force)
	if err != nil {
		return err
	}
	return db.WithContext(ctx).Transaction(write)
}

// prepareArticleImageEmbedding performs file reads and the model call before
// a rebuild checkpoint transaction acquires its progress and profile locks.
func prepareArticleImageEmbedding(ctx context.Context, db *gorm.DB, vault *VaultService, embedder embedding.Embedder, article *types.Entry, attachmentPath string, force bool) (func(*gorm.DB) error, error) {
	source := *article
	article = &source
	images, ok := embedder.(embedding.ImageEmbedder)
	if !ok || !images.SupportsImages() {
		return nil, fmt.Errorf("embedding model does not support images")
	}
	if !slices.Contains(articleImagePaths(article), attachmentPath) {
		return nil, fmt.Errorf("image attachment is not referenced by the saved article")
	}
	file, err := vault.ReadImage(article, attachmentPath)
	if err != nil {
		return nil, fmt.Errorf("read article image %s: %w", attachmentPath, err)
	}
	data, err := io.ReadAll(file)
	file.Close()
	if err != nil {
		return nil, fmt.Errorf("read article image bytes: %w", err)
	}
	mediaType := http.DetectContentType(data)
	if !strings.HasPrefix(mediaType, "image/") {
		return nil, fmt.Errorf("article image is not a supported image: %s", attachmentPath)
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(data))
	if !force {
		var count int64
		if err := db.WithContext(ctx).Table("image_embeddings").Where("space_id=? AND entry_id=? AND attachment_path=? AND model_id=? AND dimension=? AND content_hash=?",
			article.SpaceID, article.ID, attachmentPath, embedder.GetModelID(), embedder.GetDimensions(), hash).Count(&count).Error; err != nil {
			return nil, err
		}
		if count > 0 {
			return func(*gorm.DB) error { return nil }, nil
		}
	}
	ctx = types.WithLLMCallMetadata(ctx, "image_embedding", "")
	vector, err := images.EmbedImage(ctx, "data:"+mediaType+";base64,"+base64.StdEncoding.EncodeToString(data))
	if err != nil {
		return nil, fmt.Errorf("embed article image %s: %w", attachmentPath, err)
	}
	if len(vector) == 0 || len(vector) != embedder.GetDimensions() {
		return nil, fmt.Errorf("image embedding dimension mismatch: got %d, expected %d", len(vector), embedder.GetDimensions())
	}
	var norm float64
	for _, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, fmt.Errorf("image embedding contains a non-finite value")
		}
		norm += float64(value) * float64(value)
	}
	if norm == 0 {
		return nil, fmt.Errorf("image embedding has zero length")
	}
	return func(tx *gorm.DB) error {
		if err := checkEmbeddingProfile(ctx, tx, embedder, article.SpaceID); err != nil {
			return err
		}
		var current types.Entry
		if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "SHARE"}).Where("id=? AND space_id=?", article.ID, article.SpaceID).First(&current).Error; err != nil {
			return err
		}
		if current.VaultPath != article.VaultPath || current.VaultBody != article.VaultBody || !slices.Contains(articleImagePaths(&current), attachmentPath) {
			return fmt.Errorf("article image source changed during embedding")
		}
		file, err := vault.ReadImage(&current, attachmentPath)
		if err != nil {
			return err
		}
		content, err := io.ReadAll(file)
		file.Close()
		if err != nil {
			return err
		}
		if fmt.Sprintf("%x", sha256.Sum256(content)) != hash {
			return fmt.Errorf("article image bytes changed during embedding")
		}
		return tx.WithContext(ctx).Exec(`INSERT INTO image_embeddings
		(space_id,entry_id,knowledge_base_id,attachment_path,model_id,dimension,embedding,content_hash)
		VALUES (?,?,?,?,?,?,?::halfvec,?)
		ON CONFLICT (entry_id,attachment_path) DO UPDATE SET
		model_id=EXCLUDED.model_id,dimension=EXCLUDED.dimension,embedding=EXCLUDED.embedding,
		content_hash=EXCLUDED.content_hash,updated_at=NOW()`, article.SpaceID, article.ID,
			article.KnowledgeBaseID, attachmentPath, embedder.GetModelID(), len(vector), pgvector.NewHalfVector(vector), hash).Error
	}, nil
}
