package service

import (
	"context"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// fetchEntryData gets entry data in batch.
func (s *chunkSearchService) fetchEntryData(ctx context.Context,
	spaceID uint64,
	entryIDs []string,
) (map[string]*types.Entry, error) {
	entries, err := s.entryRepo.GetEntryBatch(ctx, spaceID, entryIDs)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id":  spaceID,
			"entry_ids": entryIDs,
		})
		return nil, err
	}

	entryMap := make(map[string]*types.Entry, len(entries))
	for _, entry := range entries {
		entryMap[entry.ID] = entry
	}

	return entryMap, nil
}
