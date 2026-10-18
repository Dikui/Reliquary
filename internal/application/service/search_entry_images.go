package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"net/http"
	"slices"
	"strings"

	"reliquary/internal/models/rerank"
	"reliquary/internal/types"
)

const maxMultimodalRerankDocuments = 40

// Text and the best matched image each score the entry. Neither representation
// can hide a relevant match in the other; retain the higher relevance score.
func (s *searchService) rerankImageEntryResults(ctx context.Context, model rerank.MultimodalReranker, query string, results []*types.EntrySearchResult, text []string) ([]rerank.RankResult, error) {
	documents := []rerank.RerankDocument{}
	owners := []int{}
	references := []types.ModelCallImageReference{}
	for index, result := range results {
		documents = append(documents, rerank.RerankDocument{Text: text[index]})
		owners = append(owners, index)
		for _, snippet := range result.Snippets {
			if snippet.ImagePath == "" {
				continue
			}
			document, reference, err := s.entryRerankImage(ctx, result.EntryID, snippet.ImagePath)
			if err != nil {
				return nil, err
			}
			documents = append(documents, document)
			owners = append(owners, index)
			references = append(references, reference)
			break
		}
	}
	ctx = types.WithModelCallImages(ctx, references)
	scores := make([]float64, len(results))
	for i := range scores {
		scores[i] = math.Inf(-1)
	}
	ranked, err := model.RerankMultimodal(ctx, query, documents)
	if err != nil {
		return nil, err
	}
	if len(ranked) != len(documents) {
		return nil, fmt.Errorf("multimodal rerank returned incomplete scores")
	}
	seen := make([]bool, len(documents))
	for _, rank := range ranked {
		if rank.Index < 0 || rank.Index >= len(documents) || seen[rank.Index] || math.IsNaN(rank.RelevanceScore) || math.IsInf(rank.RelevanceScore, 0) {
			return nil, fmt.Errorf("multimodal rerank returned invalid scores")
		}
		seen[rank.Index] = true
		owner := owners[rank.Index]
		scores[owner] = max(scores[owner], rank.RelevanceScore)
	}
	ranks := make([]rerank.RankResult, len(results))
	for index, score := range scores {
		ranks[index] = rerank.RankResult{Index: index, RelevanceScore: score}
	}
	return ranks, nil
}

func (s *searchService) entryRerankImage(ctx context.Context, entryID, path string) (rerank.RerankDocument, types.ModelCallImageReference, error) {
	var document rerank.RerankDocument
	var reference types.ModelCallImageReference
	if s.vault == nil {
		return document, reference, fmt.Errorf("image reranking requires the local file library")
	}
	var entry types.Entry
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", entryID, types.MustSpaceIDFromContext(ctx)).First(&entry).Error; err != nil {
		return document, reference, err
	}
	if !slices.Contains(articleImagePaths(&entry), path) {
		return document, reference, fmt.Errorf("rerank image is not a saved attachment of the entry")
	}
	file, err := s.vault.ReadImage(&entry, path)
	if err != nil {
		return document, reference, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, types.MaxIngestInlineAssetBytes+1))
	if err != nil {
		return document, reference, err
	}
	mediaType := http.DetectContentType(data)
	if len(data) > types.MaxIngestInlineAssetBytes || !strings.HasPrefix(mediaType, "image/") {
		return document, reference, fmt.Errorf("rerank image is not a supported image within the size limit")
	}
	document.Image = "data:" + mediaType + ";base64," + base64.StdEncoding.EncodeToString(data)
	reference = types.ModelCallImageReference{Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))}
	return document, reference, nil
}
