package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

type profileRaceModels struct {
	articleProcessingModels
	client embedding.Embedder
}

func (m profileRaceModels) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return m.client, nil
}

func TestQueryEmbeddingRejectsClientFromChangedProfile(t *testing.T) {
	h, _, original := newArticleImageHarness(t, true)
	var model types.Model
	require.NoError(t, h.db.First(&model, "id='embedding'").Error)
	client := &changingEmbeddingClient{articleImageFixtureEmbedder: original, hash: types.EmbeddingProfileHash(model.ID, 3, &model)}
	svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(h.db), nil, nil, nil, nil, h.db)
	client.change = func() {
		model.Name = "Changed during query generation"
		require.NoError(t, svc.UpdateModel(h.ctx, &model))
	}
	bases := &libraryService{db: h.db, repo: repository.NewLibraryRepository(h.db), modelService: profileRaceModels{client: client}}
	_, err := bases.GetQueryEmbedding(h.ctx, h.library.ID, "a query from the earlier space")
	require.ErrorContains(t, err, "profile changed")
}

type changingEntrySearchBases struct {
	entrySearchFixtureBases
	change func()
}

func (b changingEntrySearchBases) GetQueryEmbedding(ctx context.Context, libraryID, query string) ([]float32, error) {
	vector, err := b.entrySearchFixtureBases.GetQueryEmbedding(ctx, libraryID, query)
	b.change()
	return vector, err
}

func TestEntrySearchRejectsProfileChangedBetweenQueryAndRecall(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	base := changingEntrySearchBases{entrySearchFixtureBases: entrySearchFixtureBases{library: h.library}, change: func() {
		var model types.Model
		require.NoError(t, h.db.First(&model, "id='fixture-model'").Error)
		model.Name = "A new model space during query generation"
		svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(h.db), nil, nil, nil, nil, h.db)
		require.NoError(t, svc.UpdateModel(h.ctx, &model))
	}}
	svc := NewSearchService(base, nil, nil, h.db)
	_, err := svc.Search(h.ctx, types.EntrySearchRequest{Query: "othersemantic"})
	require.ErrorContains(t, err, "profile changed during query generation")
}

func TestBuiltinYAMLChangesRetireEmbeddingIdentity(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	article := h.add(t, "yaml-article", "article", "2026-10-01", "research", "unrelated observations", 1)
	addImageSearchVector(t, h, article, "../../attachments/yaml-article/chart.png", "fixture-model", []float32{0, 1, 0})
	entity := &types.Entity{ID: "yaml-entity", SpaceID: 1, CanonicalName: "YAML entity", NormalizedName: "yaml entity", EntityType: "company",
		NameEmbedding: types.EntityNameEmbedding{0, 1, 0}, EmbeddingModelID: "fixture-model", EmbeddingDimension: 3, EmbeddingNeedsRebuild: false}
	require.NoError(t, h.db.Create(entity).Error)
	require.NoError(t, h.db.Model(&types.Model{}).Where("id='fixture-model'").Updates(map[string]any{"is_builtin": true, "managed_by": types.BuiltinModelManagedBy}).Error)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "builtin_models.yaml"), []byte(`builtin_models:
  - id: fixture-model
    space_id: 1
    name: Different YAML weights with the same dimensions
    type: Embedding
    source: remote
    parameters:
      embedding_parameters:
        dimension: 3
        supports_images: true
        multimodal_format: jina
`), 0644))
	require.NoError(t, types.LoadBuiltinModelsConfig(h.ctx, h.db, dir))
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
		var identity string
		require.NoError(t, h.db.Table(table).Where("entry_id=?", article.ID).Pluck("model_id", &identity).Error)
		require.True(t, strings.HasPrefix(identity, "stale:"), table)
	}
	require.NoError(t, h.db.First(entity, "id=?", entity.ID).Error)
	require.True(t, entity.EmbeddingNeedsRebuild)
	require.True(t, strings.HasPrefix(entity.EmbeddingModelID, "stale:"))
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Empty(t, response.Results)
}

type changingEmbeddingClient struct {
	*articleImageFixtureEmbedder
	hash   string
	change func()
}

func (e *changingEmbeddingClient) GetProfileHash() string { return e.hash }
func (e *changingEmbeddingClient) callChange() {
	if e.change != nil {
		change := e.change
		e.change = nil
		change()
	}
}
func (e *changingEmbeddingClient) Embed(ctx context.Context, text string) ([]float32, error) {
	e.callChange()
	return e.chunkSearchEmbedder.Embed(ctx, text)
}
func (e *changingEmbeddingClient) EmbedImage(ctx context.Context, image string) ([]float32, error) {
	e.callChange()
	return e.articleImageFixtureEmbedder.EmbedImage(ctx, image)
}
func (e *changingEmbeddingClient) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	e.callChange()
	return e.chunkSearchEmbedder.BatchEmbed(ctx, texts)
}
func (e *changingEmbeddingClient) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}

func TestRunningEmbeddingClientCannotRepublishRetiredIdentity(t *testing.T) {
	for _, kind := range []string{"article", "image", "chunk", "chunk_batch"} {
		t.Run(kind, func(t *testing.T) {
			h, vault, original := newArticleImageHarness(t, true)
			article := h.upload(t, "Profile race", "Original illustration records verifiable measured source details.\n\n![chart]("+articleImageDataURI(t, 73)+")")
			var model types.Model
			require.NoError(t, h.db.First(&model, "id='embedding'").Error)
			client := &changingEmbeddingClient{articleImageFixtureEmbedder: original, hash: types.EmbeddingProfileHash(model.ID, 3, &model)}
			embedder := &tierEmbedder{inner: client, modelID: model.ID}
			svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(h.db), nil, nil, nil, nil, h.db)
			client.change = func() {
				model.Name = "Changed while the old client is generating"
				require.NoError(t, svc.UpdateModel(h.ctx, &model))
			}
			var err error
			switch kind {
			case "article":
				copy := *article
				copy.Title += " modified input"
				err = saveArticleEmbedding(h.ctx, h.db, embedder, &copy)
			case "image":
				err = saveArticleImageEmbedding(h.ctx, h.db, vault, embedder, article, articleImagePaths(article)[0], true)
			default:
				var chunk types.Chunk
				require.NoError(t, h.db.First(&chunk, "entry_id=?", article.ID).Error)
				index := &types.IndexInfo{SourceID: chunk.ID, SourceType: types.ChunkSourceType, ChunkID: chunk.ID,
					EntryID: article.ID, LibraryID: h.library.ID, Content: buildEntryIndexContent(article, chunk.Content)}
				if kind == "chunk" {
					err = h.service.retrieveEngine.Index(h.ctx, embedder, index)
				} else {
					err = h.service.retrieveEngine.BatchIndex(h.ctx, embedder, []*types.IndexInfo{index})
				}
			}
			require.ErrorContains(t, err, "profile changed")
			for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
				var count int64
				require.NoError(t, h.db.Table(table).Where("entry_id=? AND model_id=?", article.ID, model.ID).Count(&count).Error)
				require.Zero(t, count, "old in-flight client cannot restore the active identity")
			}
		})
	}
}

func TestModelEditInvalidatesSameIDAndDimensionVectors(t *testing.T) {
	for _, field := range []string{"name", "base_url", "multimodal_format"} {
		t.Run(field, func(t *testing.T) {
			h := newEntrySearchFixture(t)
			enableImageSearchFixture(t, h)
			article := h.add(t, "profile-article", "article", "2026-10-01", "research", "unrelated measurements", 1)
			for _, table := range []string{"embeddings", "article_embeddings"} {
				require.NoError(t, h.db.Table(table).Where("entry_id=?", article.ID).Update("embedding", pgvector.NewHalfVector([]float32{0, 1, 0})).Error)
			}
			path := "../../attachments/profile-article/chart.png"
			addImageSearchVector(t, h, article, path, "fixture-model", []float32{0, 1, 0})
			response := h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
			require.Len(t, response.Results, 1)
			var before pgvector.HalfVector
			require.NoError(t, h.db.Table("image_embeddings").Where("entry_id=?", article.ID).Pluck("embedding", &before).Error)
			var model types.Model
			require.NoError(t, h.db.First(&model, "id='fixture-model'").Error)
			switch field {
			case "name":
				model.Name = "Different weights with the same dimensions"
			case "base_url":
				model.Parameters.BaseURL = "https://another.example.org/v1"
			case "multimodal_format":
				model.Parameters.EmbeddingParameters.MultimodalFormat = ""
			}
			svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(h.db), nil, nil, nil, nil, h.db)
			require.NoError(t, svc.UpdateModel(h.ctx, &model))
			stale := ""
			for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
				var identity string
				require.NoError(t, h.db.Table(table).Where("entry_id=?", article.ID).Pluck("model_id", &identity).Error)
				require.True(t, strings.HasPrefix(identity, "stale:"), table)
				if stale == "" {
					stale = identity
				}
				require.Equal(t, stale, identity, "the old text and images keep one retired identity")
			}
			var after pgvector.HalfVector
			require.NoError(t, h.db.Table("image_embeddings").Where("entry_id=?", article.ID).Pluck("embedding", &after).Error)
			require.Equal(t, before.Slice(), after.Slice(), "old vector bytes remain available until successful rebuild")
			response = h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
			require.Empty(t, response.Results, "matching dimensions cannot re-enable vectors from the prior profile")
			response = h.search(t, types.EntrySearchRequest{Query: "Searchfixture"})
			require.Len(t, response.Results, 1, "full text continues serving while vector identity is retired")
			for _, snippet := range response.Results[0].Snippets {
				require.Empty(t, snippet.ImagePath, "the retired image vector does not contribute")
				require.Equal(t, "keyword", snippet.MatchType)
			}
		})
	}
}

func TestModelCosmeticAndCredentialEditsKeepVectorIdentity(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	article := h.add(t, "stable-article", "article", "2026-10-01", "research", "unrelated observations", 1)
	addImageSearchVector(t, h, article, "../../attachments/stable-article/chart.png", "fixture-model", []float32{0, 1, 0})
	var model types.Model
	require.NoError(t, h.db.First(&model, "id='fixture-model'").Error)
	model.DisplayName = "Friendly display name"
	model.Parameters.APIKey = "changed-credential"
	model.Parameters.MaxConcurrency = 1
	svc := NewModelServiceWithRoutingDB(repository.NewModelRepository(h.db), nil, nil, nil, nil, h.db)
	require.NoError(t, svc.UpdateModel(h.ctx, &model))
	var identity string
	require.NoError(t, h.db.Table("image_embeddings").Where("entry_id=?", article.ID).Pluck("model_id", &identity).Error)
	require.Equal(t, "fixture-model", identity)
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Len(t, response.Results, 1)
	require.Equal(t, "image", response.Results[0].Snippets[0].MatchType)
}
