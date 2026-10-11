package service

import (
	"context"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// fetchKnowledgeData gets knowledge data in batch.
func (s *knowledgeBaseService) fetchKnowledgeData(ctx context.Context,
	spaceID uint64,
	knowledgeIDs []string,
) (map[string]*types.Knowledge, error) {
	knowledges, err := s.kgRepo.GetKnowledgeBatch(ctx, spaceID, knowledgeIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id":      spaceID,
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
