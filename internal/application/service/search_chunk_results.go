package service

import (
	"context"
	"encoding/json"
	"slices"

	"reliquary/internal/logger"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
)

// processSearchResults handles the processing of search results, optimizing database queries.
func (s *chunkSearchService) processSearchResults(ctx context.Context,
	chunks []*types.IndexWithScore,
	skipEnrichment bool,
) ([]*types.SearchResult, error) {
	if len(chunks) == 0 {
		return nil, nil
	}

	spaceID := types.MustSpaceIDFromContext(ctx)

	// Collect all entry and chunk IDs, track scores and match info
	index := s.buildChunkIndex(chunks)

	logger.Infof(ctx, "Fetching entry data for %d IDs", len(index.entryIDs))
	entryMap, err := s.fetchEntryData(ctx, spaceID, index.entryIDs)
	if err != nil {
		return nil, err
	}
	for id, entry := range entryMap {
		if entry.ParseStatus == types.ParseStatusRejected {
			delete(entryMap, id)
		}
	}

	logger.Infof(ctx, "Fetching chunk data for %d IDs", len(index.chunkIDs))
	allChunks, err := s.chunkRepo.ListChunksByID(ctx, spaceID, index.chunkIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id":  spaceID,
			"chunk_ids": index.chunkIDs,
		})
		return nil, err
	}
	logger.Infof(ctx, "Chunk data fetched successfully, count: %d", len(allChunks))

	// Build chunk map and collect enrichment IDs (parent, related, nearby)
	chunkMap := make(map[string]*types.Chunk, len(allChunks))
	for _, chunk := range allChunks {
		chunkMap[chunk.ID] = chunk
	}
	// Summary keyword hits represent article metadata and have no persisted chunk.
	for _, input := range chunks {
		entry := entryMap[input.EntryID]
		if entry != nil && input.ChunkID == types.EntrySummarySearchID(entry.ID) {
			chunkMap[input.ChunkID] = &types.Chunk{
				ID: input.ChunkID, SpaceID: spaceID, EntryID: entry.ID,
				LibraryID: entry.LibraryID, Content: entry.Description,
				ChunkType: types.ChunkTypeSummary, IsEnabled: true,
			}
		}
	}

	if !skipEnrichment {
		additionalChunkIDs := s.collectEnrichmentChunkIDs(ctx, allChunks, index)
		if len(additionalChunkIDs) > 0 {
			logger.Infof(ctx, "Fetching %d additional chunks", len(additionalChunkIDs))
			additionalChunks, err := s.chunkRepo.ListChunksByID(ctx, spaceID, additionalChunkIDs)
			if err != nil {
				logger.Warnf(ctx, "Failed to fetch some additional chunks: %v", err)
			} else {
				for _, chunk := range additionalChunks {
					chunkMap[chunk.ID] = chunk
				}

			}
		}
	}

	// Build final search results
	searchResults := s.assembleSearchResults(ctx, chunks, chunkMap, entryMap, index, skipEnrichment)

	searchutil.EnrichSearchResultsImageInfo(ctx, s.chunkRepo, spaceID, searchResults)

	logger.Infof(ctx, "Search results processed, total: %d", len(searchResults))
	return searchResults, nil
}

// chunkIndex holds pre-computed lookup structures for processing search results.
type chunkIndex struct {
	entryIDs        []string
	chunkIDs        []string
	scores          map[string]float64
	matchTypes      map[string]types.MatchType
	matchedContents map[string]string
	processedIDs    map[string]bool // tracks all IDs (chunk + enrichment) to avoid duplicates
}

// buildChunkIndex collects entry/chunk IDs and builds score/matchType maps
// from the raw retrieval results.
func (s *chunkSearchService) buildChunkIndex(chunks []*types.IndexWithScore) *chunkIndex {
	idx := &chunkIndex{
		scores:          make(map[string]float64, len(chunks)),
		matchTypes:      make(map[string]types.MatchType, len(chunks)),
		matchedContents: make(map[string]string, len(chunks)),
		processedIDs:    make(map[string]bool, len(chunks)*2),
	}

	processedEntryIDs := make(map[string]bool)
	for _, chunk := range chunks {
		if !processedEntryIDs[chunk.EntryID] {
			idx.entryIDs = append(idx.entryIDs, chunk.EntryID)
			processedEntryIDs[chunk.EntryID] = true
		}
		idx.chunkIDs = append(idx.chunkIDs, chunk.ChunkID)
		idx.scores[chunk.ChunkID] = chunk.Score
		idx.matchTypes[chunk.ChunkID] = chunk.MatchType
		idx.matchedContents[chunk.ChunkID] = chunk.Content
	}
	return idx
}

// collectEnrichmentChunkIDs gathers IDs for parent, related, and nearby chunks
// that should be fetched to enrich the search results.
func (s *chunkSearchService) collectEnrichmentChunkIDs(
	ctx context.Context,
	allChunks []*types.Chunk,
	idx *chunkIndex,
) []string {
	// Mark all primary chunks as processed
	for _, chunk := range allChunks {
		idx.processedIDs[chunk.ID] = true
	}

	var additionalIDs []string

	for _, chunk := range allChunks {
		// Collect parent chunks
		if chunk.ParentChunkID != "" && !idx.processedIDs[chunk.ParentChunkID] {
			additionalIDs = append(additionalIDs, chunk.ParentChunkID)
			idx.processedIDs[chunk.ParentChunkID] = true
			idx.scores[chunk.ParentChunkID] = idx.scores[chunk.ID]
			idx.matchTypes[chunk.ParentChunkID] = types.MatchTypeParentChunk
		}

		// Collect related chunks
		relationChunkIDs := s.collectRelatedChunkIDs(chunk, idx.processedIDs)
		for _, chunkID := range relationChunkIDs {
			additionalIDs = append(additionalIDs, chunkID)
			idx.matchTypes[chunkID] = types.MatchTypeRelationChunk
		}

		// Add nearby chunks (prev and next) for text chunks
		if slices.Contains([]string{types.ChunkTypeText}, chunk.ChunkType) {
			if chunk.NextChunkID != "" && !idx.processedIDs[chunk.NextChunkID] {
				additionalIDs = append(additionalIDs, chunk.NextChunkID)
				idx.processedIDs[chunk.NextChunkID] = true
				idx.matchTypes[chunk.NextChunkID] = types.MatchTypeNearByChunk
			}
			if chunk.PreChunkID != "" && !idx.processedIDs[chunk.PreChunkID] {
				additionalIDs = append(additionalIDs, chunk.PreChunkID)
				idx.processedIDs[chunk.PreChunkID] = true
				idx.matchTypes[chunk.PreChunkID] = types.MatchTypeNearByChunk
			}
		}
	}

	return additionalIDs
}

// collectParentChunkIDs returns unprocessed parent IDs from the given chunks.
// Unlike collectEnrichmentChunkIDs, this only resolves parent links without
// expanding nearby or related chunks, making it suitable for second-round
// parent chain resolution (e.g., text → parent_text after image → text).
func (s *chunkSearchService) collectParentChunkIDs(
	chunks []*types.Chunk,
	idx *chunkIndex,
) []string {
	var ids []string
	for _, chunk := range chunks {
		if chunk.ParentChunkID != "" && !idx.processedIDs[chunk.ParentChunkID] {
			ids = append(ids, chunk.ParentChunkID)
			idx.processedIDs[chunk.ParentChunkID] = true
			idx.scores[chunk.ParentChunkID] = idx.scores[chunk.ID]
			idx.matchTypes[chunk.ParentChunkID] = types.MatchTypeParentChunk
		}
	}
	return ids
}

// assembleSearchResults builds the final []*types.SearchResult from chunk data and entry data.
// Primary results (from input chunks) are added first in order, then enrichment results.
func (s *chunkSearchService) assembleSearchResults(
	ctx context.Context,
	inputChunks []*types.IndexWithScore,
	chunkMap map[string]*types.Chunk,
	entryMap map[string]*types.Entry,
	idx *chunkIndex,
	skipEnrichment bool,
) []*types.SearchResult {
	var searchResults []*types.SearchResult
	addedChunkIDs := make(map[string]bool)
	const maxInvalidChunkLog = 8
	invalidChunkCnt := 0
	invalidChunkSamples := make([]string, 0, maxInvalidChunkLog)

	// First pass: Add results in the original order from input chunks
	for _, inputChunk := range inputChunks {
		chunk, exists := chunkMap[inputChunk.ChunkID]
		if !exists {
			logger.Debugf(ctx, "Chunk not found in chunkMap: %s", inputChunk.ChunkID)
			continue
		}
		if !s.isSearchableChunk(chunk) {
			invalidChunkCnt++
			if len(invalidChunkSamples) < maxInvalidChunkLog {
				invalidChunkSamples = append(invalidChunkSamples, chunk.ID+":"+chunk.ChunkType)
			}
			continue
		}
		if addedChunkIDs[chunk.ID] {
			continue
		}

		score := idx.scores[chunk.ID]
		if entry, ok := entryMap[chunk.EntryID]; ok {
			matchType := idx.matchTypes[chunk.ID]
			matchedContent := idx.matchedContents[chunk.ID]
			searchResults = append(searchResults, s.buildSearchResult(chunk, entry, score, matchType, matchedContent))
			addedChunkIDs[chunk.ID] = true
		} else {
			logger.Warnf(ctx, "Entry not found for chunk: %s, entry_id: %s", chunk.ID, chunk.EntryID)
		}
	}
	if invalidChunkCnt > 0 {
		logger.Debugf(ctx,
			"Skip non-searchable chunks in search results: total=%d sampled=%d samples=%v",
			invalidChunkCnt, len(invalidChunkSamples), invalidChunkSamples,
		)
	}

	// Second pass: Add enrichment chunks (parent, nearby, relation)
	if !skipEnrichment {
		for chunkID, chunk := range chunkMap {
			if addedChunkIDs[chunkID] || !s.isSearchableChunk(chunk) {
				continue
			}

			score, hasScore := idx.scores[chunkID]
			if !hasScore || score <= 0 {
				score = 0.0
			}

			if entry, ok := entryMap[chunk.EntryID]; ok {
				matchType := types.MatchTypeParentChunk
				if specificType, exists := idx.matchTypes[chunkID]; exists {
					matchType = specificType
				} else {
					logger.Warnf(ctx, "Unkonwn match type for chunk: %s", chunkID)
					continue
				}
				matchedContent := idx.matchedContents[chunkID]
				searchResults = append(searchResults, s.buildSearchResult(chunk, entry, score, matchType, matchedContent))
			}
		}
	}

	return searchResults
}

// collectRelatedChunkIDs extracts related chunk IDs from a chunk.
func (s *chunkSearchService) collectRelatedChunkIDs(chunk *types.Chunk, processedIDs map[string]bool) []string {
	var relatedIDs []string
	if len(chunk.RelationChunks) > 0 {
		var relations []string
		if err := json.Unmarshal(chunk.RelationChunks, &relations); err == nil {
			for _, id := range relations {
				if !processedIDs[id] {
					relatedIDs = append(relatedIDs, id)
					processedIDs[id] = true
				}
			}
		}
	}
	return relatedIDs
}

// buildSearchResult creates a search result from chunk and entry.
func (s *chunkSearchService) buildSearchResult(chunk *types.Chunk,
	entry *types.Entry,
	score float64,
	matchType types.MatchType,
	matchedContent string,
) *types.SearchResult {
	return &types.SearchResult{
		ID:                  chunk.ID,
		Content:             chunk.Content,
		EntryID:             chunk.EntryID,
		ChunkIndex:          chunk.ChunkIndex,
		EntryTitle:          entry.Title,
		StartAt:             chunk.StartAt,
		EndAt:               chunk.EndAt,
		Seq:                 chunk.ChunkIndex,
		Score:               score,
		MatchType:           matchType,
		Metadata:            entry.GetMetadata(),
		ChunkType:           string(chunk.ChunkType),
		ParentChunkID:       chunk.ParentChunkID,
		ImageInfo:           chunk.ImageInfo,
		EntryFilename:       entry.FileName,
		EntrySource:         entry.Source,
		EntryChannel:        entry.Channel,
		EntryDescription:    entry.Description,
		EntryCustomMetadata: entry.CustomMetadataText(),
		ChunkMetadata:       chunk.Metadata,
		MatchedContent:      matchedContent,
		LibraryID:           entry.LibraryID,
	}
}

// isSearchableChunk checks if a chunk type should be included in search results.
func (s *chunkSearchService) isSearchableChunk(chunk *types.Chunk) bool {
	if chunk == nil || !chunk.IsEnabled {
		return false
	}
	return slices.Contains([]types.ChunkType{
		types.ChunkTypeText, types.ChunkTypeSummary,
		types.ChunkTypeTableColumn, types.ChunkTypeTableSummary, types.ChunkTypeImageDescription,
	}, chunk.ChunkType)
}
