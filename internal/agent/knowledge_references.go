package agent

import (
	"encoding/json"

	agenttools "reliquary/internal/agent/tools"
	"reliquary/internal/types"
)

// appendKnowledgeRefs collects one short preview per authorized knowledge message,
// keeping full tool output out of SSE and persisted answer references.
func appendKnowledgeRefs(state *types.AgentState, step types.AgentStep) []*types.SearchResult {
	seen := make(map[string]bool, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		seen[ref.KnowledgeID] = true
	}
	var added []*types.SearchResult
	for _, call := range step.ToolCalls {
		if call.Result == nil || !call.Result.Success || call.Result.Data == nil {
			continue
		}
		switch call.Name {
		case agenttools.ToolKnowledgeSearch, agenttools.ToolGrepChunks, agenttools.ToolListKnowledgeChunks:
		default:
			continue
		}
		var payload struct {
			KnowledgeID     string            `json:"knowledge_id"`
			KnowledgeTitle  string            `json:"knowledge_title"`
			KnowledgeBaseID string            `json:"knowledge_base_id"`
			Results         []knowledgeRefRow `json:"results"`
			ChunkResults    []knowledgeRefRow `json:"chunk_results"`
			Chunks          []knowledgeRefRow `json:"chunks"`
		}
		raw, err := json.Marshal(call.Result.Data)
		if err != nil || json.Unmarshal(raw, &payload) != nil {
			continue
		}
		rows := append(payload.Results, payload.ChunkResults...)
		rows = append(rows, payload.Chunks...)
		for _, row := range rows {
			if row.KnowledgeID == "" {
				row.KnowledgeID = payload.KnowledgeID
			}
			if row.KnowledgeID == "" {
				continue
			}
			if row.KnowledgeBaseID == "" {
				row.KnowledgeBaseID = payload.KnowledgeBaseID
			}
			if row.KnowledgeBaseID == "" {
				row.KnowledgeBaseID = row.KnowledgeBase
			}
			if row.KnowledgeTitle == "" {
				row.KnowledgeTitle = payload.KnowledgeTitle
			}
			id := row.ChunkID
			if id == "" {
			}
			if id == "" {
				id = row.KnowledgeID
			}
			if seen[row.KnowledgeID] {
				continue
			}
			seen[row.KnowledgeID] = true
			ref := &types.SearchResult{
				ID:              id,
				KnowledgeID:     row.KnowledgeID,
				KnowledgeBaseID: row.KnowledgeBaseID,
				KnowledgeTitle:  row.KnowledgeTitle,
				ChunkIndex:      row.ChunkIndex,
				ChunkType:       row.ChunkType,
				Content:         row.Content,
			}
			if ref.Content == "" {
				ref.Content = row.MatchSnippet
			}
			ref.Content = truncateRunes(ref.Content, 300)
			state.KnowledgeRefs = append(state.KnowledgeRefs, ref)
			added = append(added, ref)
		}
	}
	return added
}

type knowledgeRefRow struct {
	ChunkID         string `json:"chunk_id"`
	KnowledgeID     string `json:"knowledge_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeBase   string `json:"knowledge_base"`
	KnowledgeTitle  string `json:"knowledge_title"`
	ChunkIndex      int    `json:"chunk_index"`
	ChunkType       string `json:"chunk_type"`
	Content         string `json:"content"`
	MatchSnippet    string `json:"match_snippet"`
}
