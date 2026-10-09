package handler

import "reliquary/internal/types"

// KnowledgeContextQueryResponse includes event coverage independently of pages.
type KnowledgeContextQueryResponse struct {
	Success bool                          `json:"success"`
	Data    *types.KnowledgeContextResult `json:"data"`
}
