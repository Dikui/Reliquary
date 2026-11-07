package service

import (
	"slices"

	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func filterOperationSource(query *gorm.DB, filters []types.OperationSource) (*gorm.DB, error) {
	if len(filters) == 0 {
		return query, nil
	}
	filter := filters[0]
	if filter.Type != "" && !slices.Contains([]string{"web", "agent", "import"}, filter.Type) {
		return nil, apperrors.NewBadRequestError("invalid operation source")
	}
	if filter.Type != "" {
		query = query.Where("source->>'type'=?", filter.Type)
	}
	if filter.AgentName != "" {
		query = query.Where("source->>'agent_name'=?", filter.AgentName)
	}
	if filter.ImportBatchID != "" {
		query = query.Where("source->>'import_batch_id'=?", filter.ImportBatchID)
	}
	return query, nil
}
