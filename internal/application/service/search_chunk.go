package service

import (
	"context"
	"gorm.io/gorm"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

const maxRetrievalPoolSize = 500

// GetQueryEmbedding computes the query vector with the sole knowledge base model.
func (s *chunkSearchService) GetQueryEmbedding(ctx context.Context, kbID string, queryText string) ([]float32, error) {
	kb, err := s.repo.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		return nil, err
	}

	var embeddingModel embedding.Embedder

	embeddingModel, err = GetTierEmbeddingModel(ctx, s.db, s.modelService, kb)

	if err != nil {
		logger.Errorf(ctx, "GetQueryEmbedding: failed to get embedding model %s: %v", kb.EmbeddingModelID, err)
		return nil, err
	}

	vector, err := embeddingModel.Embed(ctx, queryText)
	if err != nil {
		return nil, err
	}
	if s.db != nil {
		if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			return checkEmbeddingProfile(ctx, tx, embeddingModel, kb.SpaceID)
		}); err != nil {
			return nil, err
		}
	}
	return vector, nil
}

// ResolveEmbeddingModelKeys resolves embedding model IDs to their actual model
// identity key (name + endpoint). KBs using the same underlying model across
// different spaces will share the same key, enabling optimal grouping.
func (s *chunkSearchService) ResolveEmbeddingModelKeys(ctx context.Context, kbs []*types.KnowledgeBase) map[string]string {
	type modelRef struct {
		ModelID string
		SpaceID uint64
	}

	// Deduplicate model references
	uniqueRefs := make(map[modelRef]struct{})
	kbRefs := make(map[string]modelRef, len(kbs))
	for _, kb := range kbs {
		modelID, err := ResolveModelTier(ctx, s.db, kb, types.ModelTierEmbedding)
		if err != nil {
			logger.Warnf(ctx, "Cannot resolve embedding tier for %s: %v", kb.ID, err)
			modelID = "unavailable:" + kb.ID
		}
		ref := modelRef{ModelID: modelID, SpaceID: kb.SpaceID}
		uniqueRefs[ref] = struct{}{}
		kbRefs[kb.ID] = ref
	}

	// Resolve each unique (modelID, spaceID) to a model identity key
	resolvedKeys := make(map[modelRef]string, len(uniqueRefs))
	for ref := range uniqueRefs {
		spaceCtx := context.WithValue(ctx, types.SpaceIDContextKey, ref.SpaceID)
		model, err := s.modelService.GetModelByID(spaceCtx, ref.ModelID)
		if err != nil || model == nil {
			logger.Warnf(ctx, "ResolveEmbeddingModelKeys: cannot resolve model %s for space %d: %v", ref.ModelID, ref.SpaceID, err)
			resolvedKeys[ref] = ref.ModelID
			continue
		}
		resolvedKeys[ref] = model.Name + "|" + model.Parameters.BaseURL
	}

	result := make(map[string]string, len(kbs))
	for _, kb := range kbs {
		result[kb.ID] = resolvedKeys[kbRefs[kb.ID]]
	}
	return result
}

func (s *chunkSearchService) HybridSearch(ctx context.Context,
	id string,
	params types.SearchParams,
) ([]*types.SearchResult, error) {
	// Normalize once, before anything reads MatchCount. params is a value
	// copy, so this stays local to the call.
	params.MatchCount = normalizedMatchCount(params.MatchCount)

	searchKBIDs := []string{id}

	// QueryText is user-controlled; sanitize before logging to prevent
	// CR/LF/tab log injection. Matches the handler-layer sanitization at
	// handler/knowledgebase.go.
	logger.Infof(ctx, "Hybrid search parameters, knowledge base IDs: %v, query text: %s",
		searchKBIDs, secutils.SanitizeForLog(params.QueryText))

	spaceInfo, _ := types.SpaceInfoFromContext(ctx)
	requestSpaceID := types.MustSpaceIDFromContext(ctx)

	kbs, err := s.repo.GetKnowledgeBaseByIDs(ctx, searchKBIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_ids": searchKBIDs,
		})
		return nil, err
	}
	if len(kbs) == 0 {
		return nil, apperrors.NewNotFoundError("knowledge base not found")
	}

	if err := s.authorizeKBAccess(ctx, kbs, requestSpaceID); err != nil {
		return nil, err
	}
	for i, kb := range kbs {
		if !kb.IsVectorEnabled() {
			continue
		}
		modelID, err := ResolveModelTier(ctx, s.db, kb, types.ModelTierEmbedding)
		if err != nil {
			return nil, err
		}
		copy := *kb
		copy.EmbeddingModelID = modelID
		kbs[i] = &copy
	}

	kb := pickPrimary(kbs, id)
	if kb == nil {
		return nil, apperrors.NewNotFoundError("knowledge base not found")
	}

	// Over-retrieval (existing rule, preserved): 5x per-KB matchCount,
	// floored at DefaultRetrievalTopK, capped at maxRetrievalPoolSize across
	// the whole search.
	matchCount := max(params.MatchCount*5, types.DefaultRetrievalTopK) * len(searchKBIDs)
	if matchCount > maxRetrievalPoolSize {
		matchCount = maxRetrievalPoolSize
	}

	// Compute the query embedding once for all selected knowledge bases.
	//
	// Skip when the caller already supplied an embedding or when the primary
	// KB has no vector indexing configured.
	if len(params.QueryEmbedding) == 0 &&
		kb.IsVectorEnabled() && kb.EmbeddingModelID != "" &&
		!params.DisableVectorMatch {
		emb, embErr := s.GetQueryEmbedding(ctx, kb.ID, params.QueryText)
		if embErr != nil {
			return nil, embErr
		}
		params.QueryEmbedding = emb
	}

	baseParams, err := s.buildRetrievalParams(ctx, kb, kbs, params, matchCount)
	if err != nil {
		return nil, err
	}
	if len(baseParams) == 0 {
		logger.Infof(ctx, "No retrievable indexing pipelines across %d KBs", len(kbs))
		return nil, nil
	}
	retrieveResults, err := s.retrieveEngine.Retrieve(ctx, baseParams)

	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_base_ids": searchKBIDs,
			"query_text":         params.QueryText,
		})
		return nil, err
	}

	// Separate and fuse retrieval results.
	vectorResults, keywordResults := classifyRetrievalResults(ctx, retrieveResults)
	if len(vectorResults) == 0 && len(keywordResults) == 0 {
		logger.Info(ctx, "No search results found")
		return nil, nil
	}
	logger.Infof(ctx, "Result count before fusion: vector=%d, keyword=%d",
		len(vectorResults), len(keywordResults))

	var retrievalCfg *types.RetrievalConfig
	if spaceInfo != nil {
		retrievalCfg = spaceInfo.RetrievalConfig
	}
	deduplicatedChunks := fuseOrDeduplicate(ctx, vectorResults, keywordResults, retrievalCfg)

	kb.EnsureDefaults()

	// Truncate to the primary-match cap. MatchCount is guaranteed positive by
	// the normalization at the top of this function; the slice bound below
	// depends on that.
	if len(deduplicatedChunks) > params.MatchCount {
		deduplicatedChunks = deduplicatedChunks[:params.MatchCount]
	}

	return s.processSearchResults(ctx, deduplicatedChunks, params.SkipContextEnrichment)
}

func normalizedMatchCount(requested int) int {
	if requested <= 0 {
		return types.DefaultRetrievalTopK
	}
	return requested
}

func pickPrimary(kbs []*types.KnowledgeBase, id string) *types.KnowledgeBase {
	for _, kb := range kbs {
		if kb.ID == id {
			return kb
		}
	}
	return nil
}

// totalHits counts the IndexWithScore entries across a slice of retrieve
// results.
func totalHits(rrs []*types.RetrieveResult) int {
	n := 0
	for _, r := range rrs {
		n += len(r.Results)
	}
	return n
}

func (s *chunkSearchService) buildRetrievalParams(
	ctx context.Context,
	primary *types.KnowledgeBase,
	groupKBs []*types.KnowledgeBase,
	params types.SearchParams,
	matchCount int,
) ([]types.RetrieveParams, error) {
	var retrieveParams []types.RetrieveParams

	// Partition the group's KBs by index routing. A KB that does not have
	// vector indexing enabled (e.g. KBs with disabled indexing) has no
	// embeddings to retrieve from, and typically has no EmbeddingModelID
	// configured either; such KBs are skipped for vector retrieval to avoid
	// spurious "model ID cannot be empty" errors when the retrieval scope
	// includes an unindexed KB.
	var docVectorKBIDs, docKeywordKBIDs []string
	for _, kb := range groupKBs {
		if kb.IsVectorEnabled() && kb.EmbeddingModelID != "" {
			docVectorKBIDs = append(docVectorKBIDs, kb.ID)
		}
		if kb.IsKeywordEnabled() {
			docKeywordKBIDs = append(docKeywordKBIDs, kb.ID)
		}
	}

	// Add vector retrieval params if supported
	if s.retrieveEngine.SupportRetriever(types.VectorRetrieverType) && !params.DisableVectorMatch &&
		len(docVectorKBIDs) > 0 {
		logger.Info(ctx, "Vector retrieval supported, preparing vector retrieval parameters")

		queryEmbedding, err := s.resolveQueryEmbedding(ctx, primary, params, types.MustSpaceIDFromContext(ctx))
		if err != nil {
			return nil, err
		}

		appendVectorParams := func(kbIDs []string, entryType string) {
			retrieveParams = append(retrieveParams, types.RetrieveParams{
				Query:            params.QueryText,
				Embedding:        queryEmbedding,
				KnowledgeBaseIDs: kbIDs,
				TopK:             matchCount,
				Threshold:        params.VectorThreshold,
				RetrieverType:    types.VectorRetrieverType,
				EntryIDs:         params.EntryIDs,
				EntryType:        entryType,
			})
		}

		if len(docVectorKBIDs) > 0 {
			appendVectorParams(docVectorKBIDs, "")
		}
		logger.Info(ctx, "Vector retrieval parameters setup completed")
	}

	// Add keyword retrieval params if supported and any document KB has
	// keyword indexing enabled.
	if s.retrieveEngine.SupportRetriever(types.KeywordsRetrieverType) && !params.DisableKeywordsMatch &&
		len(docKeywordKBIDs) > 0 {
		logger.Info(ctx, "Keyword retrieval supported, preparing keyword retrieval parameters")
		retrieveParams = append(retrieveParams, types.RetrieveParams{
			Query:            params.QueryText,
			KnowledgeBaseIDs: docKeywordKBIDs,
			TopK:             matchCount,
			Threshold:        params.KeywordThreshold,
			RetrieverType:    types.KeywordsRetrieverType,
			EntryIDs:         params.EntryIDs,
		})
		logger.Info(ctx, "Keyword retrieval parameters setup completed")
	}

	return retrieveParams, nil
}

func (s *chunkSearchService) resolveQueryEmbedding(
	ctx context.Context,
	kb *types.KnowledgeBase,
	params types.SearchParams,
	currentSpaceID uint64,
) ([]float32, error) {
	if len(params.QueryEmbedding) > 0 {
		logger.Infof(ctx, "Using pre-computed query embedding, vector length: %d", len(params.QueryEmbedding))
		return params.QueryEmbedding, nil
	}

	logger.Infof(ctx, "Getting embedding model, model ID: %s", kb.EmbeddingModelID)

	var embeddingModel embedding.Embedder
	var err error

	embeddingModel, err = GetTierEmbeddingModel(ctx, s.db, s.modelService, kb)

	if err != nil {
		logger.Errorf(ctx, "Failed to get embedding model, model ID: %s, error: %v", kb.EmbeddingModelID, err)
		return nil, err
	}
	logger.Infof(ctx, "Embedding model retrieved: %v", embeddingModel)

	logger.Info(ctx, "Starting to generate query embedding")
	queryEmbedding, err := embeddingModel.Embed(ctx, params.QueryText)
	if err != nil {
		logger.Errorf(ctx, "Failed to embed query text, query text: %s, error: %v", params.QueryText, err)
		return nil, err
	}
	logger.Infof(ctx, "Query embedding generated successfully, embedding vector length: %d", len(queryEmbedding))
	return queryEmbedding, nil
}

func (s *chunkSearchService) authorizeKBAccess(ctx context.Context, kbs []*types.KnowledgeBase, spaceID uint64) error {
	for _, kb := range kbs {
		if kb == nil || kb.SpaceID != spaceID {
			return apperrors.NewForbiddenError("knowledge base is outside the space")
		}
	}
	return nil
}
