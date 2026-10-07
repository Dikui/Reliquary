package service

import (
	"context"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// fetchKnowledgeData gets knowledge data in batch.
func (s *knowledgeBaseService) fetchKnowledgeData(ctx context.Context,
	tenantID uint64,
	knowledgeIDs []string,
) (map[string]*types.Knowledge, error) {
	knowledges, err := s.kgRepo.GetKnowledgeBatch(ctx, tenantID, knowledgeIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"tenant_id":     tenantID,
			"knowledge_ids": knowledgeIDs,
		})
		return nil, err
	}

	knowledgeMap := make(map[string]*types.Knowledge, len(knowledges))
	for _, knowledge := range knowledges {
		knowledgeMap[knowledge.ID] = knowledge
	}

	return knowledgeMap, nil
}
