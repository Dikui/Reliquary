package chatpipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reliquary/internal/searchutil"
	"reliquary/internal/types"
)

func (p *PluginSearchParallel) searchStructuredContext(ctx context.Context, cm *types.ChatManage, sourceOnly ...bool) {
	if p.contextReader == nil {
		return
	}
	targets := cm.SearchTargets
	if len(targets) == 0 {
		for _, id := range cm.KnowledgeBaseIDs {
			targets = append(targets, &types.SearchTarget{Type: types.SearchTargetTypeKnowledgeBase, KnowledgeBaseID: id})
		}
	}
	var envelope strings.Builder
	for _, kbID := range targets.GetAllKnowledgeBaseIDs() {
		scopeCtx, ids, err := searchutil.ResolveContextScope(ctx, targets, kbID, p.knowledgeService)
		if err != nil {
			pipelineWarn(ctx, "ContextSearch", "scope", map[string]any{"kb_id": kbID, "error": err.Error()})
			continue
		}
		onlySources := len(sourceOnly) > 0 && sourceOnly[0]
		queries := []types.KnowledgeContextQuery{}
		if !onlySources {
			queries = append(queries, cm.KnowledgeQueries...)
		}
		if len(queries) == 0 && !onlySources {
			for _, entity := range cm.Entity {
				queries = append(queries, types.KnowledgeContextQuery{Entity: entity, Limit: 30})
			}
		}
		// Structured assertions from the same evidence retrieved by RAG also
		// complement queries for which rewriting was deliberately disabled.
		var sourceIDs []string
		for _, result := range cm.SearchResult {
			if result.KnowledgeBaseID == kbID {
				sourceIDs = append(sourceIDs, result.KnowledgeID)
			}
		}
		if len(sourceIDs) > 0 {
			queries = append(queries, types.KnowledgeContextQuery{KnowledgeIDs: sourceIDs, Limit: 30})
		}
		for _, query := range queries {
			if len(query.KnowledgeIDs) == 0 {
				query.KnowledgeIDs = ids
			} else if len(ids) > 0 {
				allowed := map[string]bool{}
				for _, id := range ids {
					allowed[id] = true
				}
				filtered := []string{}
				for _, id := range query.KnowledgeIDs {
					if allowed[id] {
						filtered = append(filtered, id)
					}
				}
				if len(filtered) == 0 {
					continue
				}
				query.KnowledgeIDs = filtered
			}
			result, err := p.contextReader.Query(scopeCtx, kbID, query)
			if err != nil {
				pipelineWarn(ctx, "ContextSearch", "query", map[string]any{"kb_id": kbID, "error": err.Error()})
				continue
			}
			if result == nil || len(result.Facts) == 0 && len(result.Events) == 0 {
				continue
			}
			// Preserve evidence metadata and quotes; full snapshots are fetched on
			// demand so graph context does not duplicate every original document.
			for i := range result.Evidence {
				result.Evidence[i].Body = ""
			}
			encoded, err := json.Marshal(result)
			if err != nil {
				continue
			}
			fmt.Fprintf(&envelope, "Knowledge base %s: %s\n", kbID, encoded)
			if len(cm.SearchResult) == 0 {
				p.recallContextSources(scopeCtx, cm, result)
			}
		}
	}
	if len(sourceOnly) > 0 && sourceOnly[0] {
		cm.KnowledgeContext += envelope.String()
	} else {
		cm.KnowledgeContext = envelope.String()
	}
}

func (p *PluginSearchParallel) recallContextSources(ctx context.Context, cm *types.ChatManage, result *types.KnowledgeContextResult) {
	for _, evidence := range result.Evidence {
		if len(cm.SearchResult) >= 10 {
			break
		}
		knowledge, err := p.knowledgeService.GetKnowledgeByID(ctx, evidence.KnowledgeID)
		if err != nil || knowledge == nil {
			continue
		}
		chunks, err := p.searchPlugin.chunkService.ListChunksByKnowledgeID(ctx, knowledge.ID)
		if err != nil {
			continue
		}
		for _, chunk := range chunks {
			if chunk.ChunkType != types.ChunkTypeText {
				continue
			}
			cm.SearchResult = append(cm.SearchResult, chunk2SearchResult(chunk, knowledge))
			break
		}
	}
}

// chunk2SearchResult converts a chunk to a search result
func chunk2SearchResult(chunk *types.Chunk, knowledge *types.Knowledge) *types.SearchResult {
	return &types.SearchResult{
		ID:                chunk.ID,
		Content:           chunk.Content,
		ContentRevision:   chunk.ContentRevision,
		KnowledgeID:       chunk.KnowledgeID,
		ChunkIndex:        chunk.ChunkIndex,
		KnowledgeTitle:    knowledge.Title,
		StartAt:           chunk.StartAt,
		EndAt:             chunk.EndAt,
		Seq:               chunk.ChunkIndex,
		Score:             1.0,
		MatchType:         types.MatchTypeGraph,
		Metadata:          knowledge.GetMetadata(),
		ChunkType:         string(chunk.ChunkType),
		ParentChunkID:     chunk.ParentChunkID,
		ImageInfo:         chunk.ImageInfo,
		KnowledgeFilename: knowledge.FileName,
		KnowledgeSource:   knowledge.Source,
		KnowledgeChannel:  knowledge.Channel,
		ChunkMetadata:     chunk.Metadata,
		KnowledgeBaseID:   knowledge.KnowledgeBaseID,
	}
}
