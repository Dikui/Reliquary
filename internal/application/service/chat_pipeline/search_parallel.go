package chatpipeline

import (
	"context"

	"reliquary/internal/config"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// PluginSearchParallel implements document chunk search
type PluginSearchParallel struct {
	// Chunk search dependencies
	knowledgeBaseService interfaces.KnowledgeBaseService
	knowledgeService     interfaces.KnowledgeService
	config               *config.Config

	searchPlugin *PluginSearch
}

// NewPluginSearchParallel creates a new parallel search plugin
func NewPluginSearchParallel(
	eventManager *EventManager,
	knowledgeBaseService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
	config *config.Config,
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
		searchPlugin:         searchPlugin,
	}
	eventManager.Register(res)
	return res
}

// ActivationEvents returns the event types this plugin handles
func (p *PluginSearchParallel) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHUNK_SEARCH_PARALLEL}
}

// OnEvent handles search events using document chunk retrieval
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

	chatManage.SearchResult = chunkCM.SearchResult
	chatManage.SearchResult = removeDuplicateResults(chatManage.SearchResult)

	pipelineInfo(ctx, "SearchParallel", "complete", map[string]interface{}{
		"session_id":    chatManage.SessionID,
		"chunk_results": len(chunkCM.SearchResult),
		"total_results": len(chatManage.SearchResult),
		"has_error":     err != nil,
	})
	if len(chatManage.SearchResult) == 0 {
		if err != nil {
			return err
		}
		return ErrSearchNothing
	}

	return next()
}
