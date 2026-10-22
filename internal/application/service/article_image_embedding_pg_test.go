package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reliquary/internal/models/embedding"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/utils"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
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
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=1").Update("retrieval_config", &types.RetrievalConfig{ImageVectorEnabled: true}).Error)
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
	firstImage := articleImageDataURI(t, 29)
	var large bytes.Buffer
	require.NoError(t, png.Encode(&large, image.NewNRGBA(image.Rect(0, 0, 5000, 40))))
	largeURI := "data:image/png;base64," + base64.StdEncoding.EncodeToString(large.Bytes())
	article := h.upload(t, "Image observations", "Original measurements are recorded in the illustrations.\n\n![first]("+firstImage+")\n\n![second]("+articleImageDataURI(t, 58)+")\n\n![repeat]("+firstImage+")\n\n![large]("+largeURI+")")
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	paths := articleImagePaths(article)
	require.Len(t, paths, 3, "repeated attachment is vectorized once")
	require.Len(t, e.images, 3)
	type row struct {
		ModelID, AttachmentPath, ContentHash string
		Dimension                            int
		Embedding                            pgvector.HalfVector
	}
	var rows []row
	require.NoError(t, h.db.Table("image_embeddings").Where("entry_id=?", article.ID).Order("attachment_path").Scan(&rows).Error)
	require.Len(t, rows, 3)
	var transformed bool
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
		_, payload, _ := strings.Cut(e.images[index], ",")
		sent, err := base64.StdEncoding.DecodeString(payload)
		require.NoError(t, err)
		config, _, err := image.DecodeConfig(bytes.NewReader(sent))
		require.NoError(t, err)
		require.LessOrEqual(t, max(config.Width, config.Height), 4096)
		transformed = transformed || !bytes.Equal(data, sent)
	}
	require.True(t, transformed, "large images use a derived copy while stored originals keep their hash")
	require.NoError(t, indexArticleImages(h.ctx, h.db, h.service.modelService, h.library, vault, article))
	require.Len(t, e.images, 3, "unchanged bytes and model retain vectors")
	e.err = errors.New("fixture unavailable")
	embedder, err := GetTierEmbeddingModel(h.ctx, h.db, h.service.modelService, h.library)
	require.NoError(t, err)
	err = saveArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, paths[0], true)
	require.ErrorContains(t, err, "fixture unavailable")
	var count int64
	require.NoError(t, h.db.Table("image_embeddings").Where("entry_id=?", article.ID).Count(&count).Error)
	require.EqualValues(t, 3, count, "failed replacement keeps the prior image vector")
}

func TestArticleImagePreparationRejectsChangedAttachmentBeforeWrite(t *testing.T) {
	h, vault, _ := newArticleImageHarness(t, true)
	article := h.upload(t, "Fresh image", "Original illustration records verifiable source details.\n\n![chart]("+articleImageDataURI(t, 17)+")")
	path := articleImagePaths(article)[0]
	embedder, err := GetTierEmbeddingModel(h.ctx, h.db, h.service.modelService, h.library)
	require.NoError(t, err)
	write, err := prepareArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, path, true)
	require.NoError(t, err)
	relative := filepath.Join(filepath.Dir(article.VaultPath), path)
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, relative), []byte("changed image bytes"), 0644))
	require.ErrorContains(t, write(h.db), "bytes changed")
	gifData, err := base64.StdEncoding.DecodeString("R0lGODlhAQABAIAAAAAAAP///yH5BAEAAAAALAAAAAABAAEAAAIBRAA7")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(vault.root, relative), gifData, 0644))
	_, err = prepareArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, path, true)
	require.ErrorContains(t, err, "不支持 GIF")
	require.True(t, utils.IsPermanentImageError(err))
	err = h.service.processArticleBody(h.ctx, h.library, article, article.VaultBody, nil, types.EffectiveProcessConfig{})
	require.ErrorContains(t, err, "不支持 GIF")
	require.True(t, utils.IsPermanentImageError(err), "stored GIFs fail before completed checkpoints can skip image processing")
	require.NoError(t, h.db.Model(article).Update("parse_status", types.ParseStatusPending).Error)
	payload, err := json.Marshal(types.ManualProcessPayload{SpaceID: article.SpaceID, LibraryID: article.LibraryID, EntryID: article.ID, Content: article.VaultBody, NeedCleanup: true, RebuildID: "gif-rebuild"})
	require.NoError(t, err)
	err = h.service.ProcessManualUpdate(h.ctx, taskqueue.NewTask(types.TypeManualProcess, payload))
	require.ErrorContains(t, err, "不支持 GIF")
	require.ErrorIs(t, err, taskqueue.SkipRetry)
	data, err := os.ReadFile(filepath.Join(vault.root, relative))
	require.NoError(t, err)
	require.Equal(t, gifData, data, "rejected historical GIFs remain stored")
	copy := *article
	copy.SourceKind = "mine"
	require.Equal(t, articleImagePaths(article), articleImagePaths(&copy))
	copy.SourceKind = "report"
	require.Equal(t, articleImagePaths(article), articleImagePaths(&copy))
}
