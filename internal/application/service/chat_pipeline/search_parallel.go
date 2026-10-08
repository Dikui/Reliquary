package chatpipeline

import (
	"context"

	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// PluginSearchParallel implements structured context and chunk search
type PluginSearchParallel struct {
	// Chunk search dependencies
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	config               *config.Config

	contextReader interfaces.KnowledgeContextReader
	searchPlugin  *PluginSearch
}

// NewPluginSearchParallel creates a new parallel search plugin
func NewPluginSearchParallel(
	eventManager *EventManager,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	config *config.Config,
	contextReader interfaces.KnowledgeContextReader,
) *PluginSearchParallel {
	// Create internal plugins without registering them
	searchPlugin := &PluginSearch{
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		chunkService:         chunkService,
		config:               config,
	}

	res := &PluginSearchParallel{
		knowledgeBaseService: knowledgeBaseService,
		knowledgeService:     knowledgeService,
		config:               config,
		contextReader:        contextReader,
		searchPlugin:         searchPlugin,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginSearchParallel) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH_PARALLEL}
}

// OnEvent handles search events using structured context and chunk retrieval
func (p *PluginSearchParallel) OnEvent(ctx context.Context,
	eventType types.EventType, chatManage *types.ChatManage, next func() *PluginError,
) *PluginError {
	// Intent-based skip: query-understand step determined KB retrieval is unnecessary
	if !chatManage.NeedsRetrieval() {
		pipelineInfo(ctx, "SearchParallel", "skip", map[string]interface{}{
			"session_id": chatManage.SessionID,
			"reason":     "intent_no_search",
		})
		return next()
	}

	pipelineInfo(ctx, "SearchParallel", "start", map[string]interface{}{
		"session_id":    chatManage.SessionID,
		"has_entities":  len(chatManage.Entity) > 0,
		"rewrite_query": chatManage.RewriteQuery,
	})

	// Resolve structured lookups first. Original-text retrieval then supplies
	// explanations, missing fields and conflicting evidence.
	p.searchStructuredContext(ctx, chatManage)
	structuredSources := append([]*types.SearchResult{}, chatManage.SearchResult...)
	// Deep-copy to avoid concurrent read/write on shared slice fields
	chunkCM := chatManage.Clone()
	chunkCM.SearchResult = nil
	noop := func() *PluginError { return nil }
	err := p.searchPlugin.OnEvent(ctx, types.CHUNK_SEARCH, chunkCM, noop)
	if err == ErrSearchNothing {
		err = nil
	}
	if err != nil {
		logger.Warnf(ctx, "[SearchParallel] chunk search error: %v", err.Err)
	}

	// Merge chunk results with structured evidence sources
	chatManage.SearchResult = append(chunkCM.SearchResult, structuredSources...)
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)
	p.searchStructuredContext(ctx, chatManage, true)

	pipelineInfo(ctx, "SearchParallel", "complete", map[string]interface{}{
		"session_id":    chatManage.SessionID,
		"chunk_results": len(chunkCM.SearchResult),
		"total_results": len(chatManage.SearchResult),
		"has_error":     err != nil,
	})
	if len(chatManage.SearchResult) == 0 && chatManage.KnowledgeContext == "" {
		if err != nil {
			return err
		}
		return ErrSearchNothing
	}

	return next()
}
