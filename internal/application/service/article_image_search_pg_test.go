package service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"reliquary/internal/models/rerank"
	"reliquary/internal/types"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
)

func enableImageSearchFixture(t *testing.T, h *entrySearchFixture) {
	t.Helper()
	require.NoError(t, h.db.Create(&types.Model{ID: "fixture-model", SpaceID: 1, Name: "Shared image space",
		Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3, SupportsImages: true, MultimodalFormat: "jina"}}}).Error)
	h.config.ImageVectorEnabled = true
	h.config.VectorThreshold = 0.9
	var space types.Space
	require.NoError(t, h.db.First(&space, "id=1").Error)
	space.RetrievalConfig = h.config
	require.NoError(t, h.db.Save(&space).Error)
}

func addImageSearchVector(t *testing.T, h *entrySearchFixture, article *types.Entry, path, modelID string, vector []float32) {
	t.Helper()
	require.NoError(t, h.db.Exec(`INSERT INTO image_embeddings
		(entry_id,space_id,library_id,attachment_path,model_id,dimension,embedding,content_hash)
		VALUES (?,1,?,?,?,?,?,'fixture')`, article.ID, h.library.ID, path, modelID, len(vector), pgvector.NewHalfVector(vector)).Error)
}

func TestArticleImageSearchRecallsImageOnlyInformationAndReturnsThumbnail(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	article := h.add(t, "image-only", "article", "2026-10-01", "research", "unrelated text observation", 1)
	path := "../../attachments/image-only/image-chart.png"
	addImageSearchVector(t, h, article, path, "fixture-model", []float32{0, 1, 0})
	addImageSearchVector(t, h, article, "../../attachments/image-only/image-second.png", "fixture-model", []float32{0, 1, 0})
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic", CollapseSimilar: entrySearchBool(false)})
	require.Equal(t, []string{"image-only"}, entrySearchIDs(response))
	result := response.Results[0]
	require.Len(t, result.Snippets, 2)
	require.Equal(t, path, result.Snippets[0].ImagePath)
	require.Equal(t, "image", result.Snippets[0].MatchType)
	require.Empty(t, result.Snippets[0].ChunkID)
	require.Equal(t, -1, result.Snippets[0].ChunkIndex)
	require.InDelta(t, 0.2/61, result.Score, 1e-10, "multiple matching images do not multiply one article's channel score")
	for _, snippet := range result.Snippets {
		require.Empty(t, snippet.Content, "no fabricated OCR text enters a visual match")
	}
	var model types.Model
	require.NoError(t, h.db.First(&model, "id='fixture-model'").Error)
	model.Parameters.EmbeddingParameters.SupportsImages = false
	require.NoError(t, h.db.Save(&model).Error)
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Empty(t, response.Results, "text-only model disables the image channel")
}

func TestArticleImageSearchRejectsOldModelAndWrongDimensionAndHonorsFilters(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	for _, test := range []struct {
		id, model         string
		vector            []float32
		kind, contentType string
	}{
		{"current", "fixture-model", []float32{0, 1, 0}, "article", "research"},
		{"old-model", "previous-model", []float32{0, 1, 0}, "article", "research"},
		{"old-dimension", "fixture-model", []float32{0, 1}, "article", "research"},
		{"summary", "fixture-model", []float32{0, 1, 0}, "article", "tutorial"},
		{"personal", "fixture-model", []float32{0, 1, 0}, "mine", ""},
	} {
		article := h.add(t, test.id, test.kind, "2026-10-01", test.contentType, "unrelated text", 0)
		addImageSearchVector(t, h, article, fmt.Sprintf("../../attachments/%s/image.png", article.ID), test.model, test.vector)
	}
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic", CollapseSimilar: entrySearchBool(false)})
	require.Equal(t, []string{"current", "personal", "summary"}, entrySearchIDs(response))
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", ContentTypes: []string{"event"}})
	require.Empty(t, response.Results)
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", Sources: []string{"mine"}})
	require.Equal(t, []string{"personal"}, entrySearchIDs(response))
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id='current'").Update("enable_status", "disabled").Error)
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Equal(t, []string{"personal", "summary"}, entrySearchIDs(response))
}

func TestArticleImageSearchKeepsMatchedImageAlongsideHigherScoringText(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	h.config.RRFImageWeight = 0.1
	article := h.add(t, "combined", "article", "2026-10-01", "research", "othersemantic", 4)
	path := "../../attachments/combined/image.png"
	addImageSearchVector(t, h, article, path, "fixture-model", []float32{0, 1, 0})
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Len(t, response.Results, 1)
	require.Len(t, response.Results[0].Snippets, 3)
	require.True(t, slices.ContainsFunc(response.Results[0].Snippets, func(snippet types.EntrySearchSnippet) bool { return snippet.ImagePath == path }))
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", PublishedFrom: "2026-10-02"})
	require.Empty(t, response.Results, "publication filters also apply to image matches")
}

func entrySearchBool(value bool) *bool { return &value }

type imageSearchReranker struct {
	target         string
	images         []string
	documentCounts []int
	err            error
}

func (r *imageSearchReranker) GetModelID() string   { return "image-rerank" }
func (r *imageSearchReranker) GetModelName() string { return "qwen3-vl-rerank" }
func (r *imageSearchReranker) SupportsImages() bool { return true }
func (r *imageSearchReranker) Rerank(context.Context, string, []string) ([]rerank.RankResult, error) {
	return nil, errors.New("image matches must use multimodal reranking")
}
func (r *imageSearchReranker) RerankMultimodal(_ context.Context, _ string, documents []rerank.RerankDocument) ([]rerank.RankResult, error) {
	r.documentCounts = append(r.documentCounts, len(documents))
	if r.err != nil {
		return nil, r.err
	}
	ranks := []rerank.RankResult{}
	for index, document := range documents {
		score := 0.1
		if document.Image != "" {
			r.images = append(r.images, document.Image)
			score = 0.2
			if document.Image == r.target {
				score = 0.95
			}
		}
		ranks = append(ranks, rerank.RankResult{Index: index, RelevanceScore: score})
	}
	slices.Reverse(ranks)
	return ranks, nil
}

func TestArticleImageSearchReranksOriginalAttachmentsWithoutDescriptions(t *testing.T) {
	h := newEntrySearchFixture(t)
	enableImageSearchFixture(t, h)
	root := t.TempDir()
	imageURIs := []string{articleImageDataURI(t, 29), articleImageDataURI(t, 58)}
	for index, id := range []string{"a-unrelated", "z-target"} {
		entry := h.add(t, id, "article", "2026-10-01", "research", "unrelated observation", 0)
		path := "../attachments/" + id + "/original.png"
		require.NoError(t, h.db.Model(entry).Update("vault_body", "![source]("+path+")").Error)
		addImageSearchVector(t, h, entry, path, "fixture-model", []float32{0, 1, 0})
		_, encoded, _ := strings.Cut(imageURIs[index], ",")
		data, err := base64.StdEncoding.DecodeString(encoded)
		require.NoError(t, err)
		directory := filepath.Join(root, "attachments", id)
		require.NoError(t, os.MkdirAll(directory, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(directory, "original.png"), data, 0644))
	}
	baseline := h.search(t, types.EntrySearchRequest{Query: "othersemantic", CollapseSimilar: entrySearchNoCollapse()})
	ranker := &imageSearchReranker{target: imageURIs[1]}
	h.library.ModelTiers.Rerank = ranker.GetModelID()
	require.NoError(t, h.db.Save(h.library).Error)
	svc := h.svc.(*searchService)
	svc.vault = &VaultService{root: root}
	svc.modelService = ikb08Models{ranker: ranker}
	response := h.search(t, types.EntrySearchRequest{Query: "othersemantic", CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, "applied", response.RerankStatus)
	require.Equal(t, "z-target", response.Results[0].EntryID)
	require.InDelta(t, 0.95, response.Results[0].Score, 1e-9)
	require.ElementsMatch(t, imageURIs, ranker.images, "send original image bytes rather than generated descriptions")
	require.Empty(t, response.Results[0].Snippets[0].Content)
	var count int64
	require.NoError(t, h.db.Table("chunks").Where("chunk_type='image_description'").Count(&count).Error)
	require.Zero(t, count)

	window := make([]*types.EntrySearchResult, 21)
	for index := range window {
		candidate := *baseline.Results[index%len(baseline.Results)]
		window[index] = &candidate
	}
	tail := window[20]
	tailScore := tail.Score
	ranker.documentCounts = nil
	window, status := svc.rerankEntryResults(h.ctx, h.library, "othersemantic", window)
	require.Equal(t, "applied", status)
	require.Equal(t, []int{40}, ranker.documentCounts, "all comparable scores come from one request")
	require.Same(t, tail, window[20], "candidates outside the window retain their position")
	require.Equal(t, tailScore, tail.Score)

	ranker.err = errors.New("image model unavailable")
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", CollapseSimilar: entrySearchNoCollapse()})
	require.Equal(t, "unavailable", response.RerankStatus)
	for index, result := range baseline.Results {
		require.Equal(t, result.EntryID, response.Results[index].EntryID)
		require.Equal(t, result.Score, response.Results[index].Score, "model failure preserves the complete fused ranking")
	}
}
