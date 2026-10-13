package service

import (
	"fmt"
	"slices"
	"testing"

	"github.com/pgvector/pgvector-go"
	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func enableImageSearchFixture(t *testing.T, h *entrySearchFixture) {
	t.Helper()
	require.NoError(t, h.db.Create(&types.Model{ID: "fixture-model", SpaceID: 1, Name: "Shared image space",
		Type: types.ModelTypeEmbedding, Source: types.ModelSourceRemote, Status: types.ModelStatusActive,
		Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: 3, SupportsImages: true, MultimodalFormat: "jina"}}}).Error)
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
	require.Equal(t, []string{"current", "personal"}, entrySearchIDs(response))
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", ContentTypes: []string{"event"}})
	require.Empty(t, response.Results)
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic", Sources: []string{"mine"}})
	require.Equal(t, []string{"personal"}, entrySearchIDs(response))
	require.NoError(t, h.db.Model(&types.Entry{}).Where("id='current'").Update("enable_status", "disabled").Error)
	response = h.search(t, types.EntrySearchRequest{Query: "othersemantic"})
	require.Equal(t, []string{"personal"}, entrySearchIDs(response))
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
