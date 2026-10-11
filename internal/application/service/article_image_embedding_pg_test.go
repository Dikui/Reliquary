package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type articleImageFixtureEmbedder struct {
	chunkSearchEmbedder
	enabled bool
	images  []string
	err     error
}

func (e *articleImageFixtureEmbedder) SupportsImages() bool { return e.enabled }
func (e *articleImageFixtureEmbedder) EmbedImage(_ context.Context, image string) ([]float32, error) {
	e.images = append(e.images, image)
	return []float32{0, 1, 0}, e.err
}

type articleImageFixtureModels struct {
	articleProcessingModels
	image *articleImageFixtureEmbedder
}

func (m articleImageFixtureModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.image, nil
}

func newArticleImageHarness(t *testing.T, enabled bool) (*articleProcessingHarness, *VaultService, *articleImageFixtureEmbedder) {
	t.Helper()
	h, vault := newVaultArticleHarness(t)
	e := &articleImageFixtureEmbedder{enabled: enabled}
	h.service.modelService = articleImageFixtureModels{h.service.modelService.(articleProcessingModels), e}
	h.service.chunkService.(*chunkService).modelService = h.service.modelService
	var model types.Model
	require.NoError(t, h.db.First(&model, "id='embedding'").Error)
	model.Parameters.EmbeddingParameters.SupportsImages = enabled
	model.Parameters.EmbeddingParameters.MultimodalFormat = "jina"
	model.Source = types.ModelSourceRemote
	require.NoError(t, h.db.Save(&model).Error)
	return h, vault, e
}

func TestArticleImagesEmbedAfterFullProcessingAndKeepModelIdentity(t *testing.T) {
	h, vault, e := newArticleImageHarness(t, true)
	image := articleImageDataURI(t, 29)
	article := h.upload(t, "Image observations", "Original measurements are recorded in the illustrations.\n\n![first]("+image+")\n\n![second]("+articleImageDataURI(t, 58)+")\n\n![repeat]("+image+")")
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	paths := articleImagePaths(article)
	require.Len(t, paths, 2, "repeated attachment is vectorized once")
	require.Len(t, e.images, 2)
	type row struct {
		ModelID, AttachmentPath, ContentHash string
		Dimension                            int
		Embedding                            pgvector.HalfVector
	}
	var rows []row
	require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Order("attachment_path").Scan(&rows).Error)
	require.Len(t, rows, 2)
	for index, stored := range rows {
		require.Equal(t, "embedding", stored.ModelID)
		require.Equal(t, 3, stored.Dimension)
		require.Equal(t, []float32{0, 1, 0}, stored.Embedding.Slice())
		require.Equal(t, paths[index], stored.AttachmentPath)
		file, err := vault.ReadImage(article, stored.AttachmentPath)
		require.NoError(t, err)
		data, err := io.ReadAll(file)
		require.NoError(t, err)
		require.NoError(t, file.Close())
		require.Equal(t, fmt.Sprintf("%x", sha256.Sum256(data)), stored.ContentHash)
	}
	require.NoError(t, indexArticleImages(h.ctx, h.db, h.service.modelService, h.kb, vault, article))
	require.Len(t, e.images, 2, "unchanged bytes and model retain vectors")
	e.err = errors.New("fixture unavailable")
	embedder, err := GetTierEmbeddingModel(h.ctx, h.db, h.service.modelService, h.kb)
	require.NoError(t, err)
	err = saveArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, paths[0], true)
	require.ErrorContains(t, err, "fixture unavailable")
	var count int64
	require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.EqualValues(t, 2, count, "failed replacement keeps the prior image vector")
}

func TestArticleImagesSkipSummaryOnlyAndUnsupportedModels(t *testing.T) {
	for _, test := range []struct {
		name, contentType string
		supported         bool
	}{
		{"summary", "tutorial", true}, {"text model", "event", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _, e := newArticleImageHarness(t, test.supported)
			h.processor.output = validProcessingFixture(test.contentType)
			article := h.upload(t, "Image skip", "Verifiable source measurements are illustrated below.\n\n![chart]("+articleImageDataURI(t, 42)+")")
			require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
			require.Empty(t, e.images)
			var count int64
			require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestArticleImagePreparationRejectsChangedAttachmentBeforeWrite(t *testing.T) {
	h, vault, _ := newArticleImageHarness(t, true)
	article := h.upload(t, "Fresh image", "Original illustration records verifiable source details.\n\n![chart]("+articleImageDataURI(t, 17)+")")
	path := articleImagePaths(article)[0]
	embedder, err := GetTierEmbeddingModel(h.ctx, h.db, h.service.modelService, h.kb)
	require.NoError(t, err)
	write, err := prepareArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, path, true)
	require.NoError(t, err)
	relative := filepath.Join(filepath.Dir(article.VaultPath), path)
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, relative), []byte("changed image bytes"), 0644))
	require.ErrorContains(t, write(h.db), "bytes changed")
	copy := *article
	copy.SourceKind = "mine"
	require.Empty(t, articleImagePaths(&copy))
	copy.SourceKind = "report"
	require.Empty(t, articleImagePaths(&copy))
}

func TestArticleImageVectorsCleanOnSummaryOnlyAndSoftDeletion(t *testing.T) {
	h, _, _ := newArticleImageHarness(t, true)
	article := h.upload(t, "Image cleanup", "Original illustration records verifiable source details.\n\n![chart]("+articleImageDataURI(t, 21)+")")
	var count int64
	require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, h.db.Model(article).Update("processing_mode", types.ProcessingModeSummaryOnly).Error)
	require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
	article = h.upload(t, "Image delete", "Different original illustration contains complete measurements.\n\n![chart]("+articleImageDataURI(t, 67)+")")
	require.NoError(t, h.db.Delete(article).Error)
	require.NoError(t, h.db.Table("image_embeddings").Where("knowledge_id=?", article.ID).Count(&count).Error)
	require.Zero(t, count)
}
