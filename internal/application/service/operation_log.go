package service

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type OperationLogService struct{ db *gorm.DB }

func NewOperationLogService(db *gorm.DB) *OperationLogService { return &OperationLogService{db: db} }

func (s *OperationLogService) Record(ctx context.Context, operationType, objectID string, before, after any) error {
	return s.RecordTx(ctx, s.db, operationType, objectID, before, after)
}

// RecordTx does not infer user actions from a principal. Callers invoke it only
// from explicit editing operations; processing code never calls this method.
func (s *OperationLogService) RecordTx(ctx context.Context, tx *gorm.DB, operationType, objectID string, before, after any) error {
	actor, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return nil
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return werrors.NewUnauthorizedError("personal space is required")
	}
	if !slices.Contains(types.OperationTypes, operationType) {
		return werrors.NewBadRequestError("invalid operation type")
	}
	oldValue, err := json.Marshal(before)
	if err != nil {
		return err
	}
	newValue, err := json.Marshal(after)
	if err != nil {
		return err
	}
	if tx == nil {
		return werrors.NewInternalServerError("operation log database is unavailable")
	}
	return tx.WithContext(ctx).Create(&types.OperationLog{ID: uuid.NewString(), TenantID: tenantID,
		Actor: actor.StorageID(), OperationType: operationType, ObjectID: objectID,
		Before: types.JSON(oldValue), After: types.JSON(newValue), CreatedAt: time.Now()}).Error
}

func (s *OperationLogService) List(ctx context.Context, operationType string, page, pageSize int) (*types.OperationLogPage, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, werrors.NewUnauthorizedError("personal space is required")
	}
	if operationType != "" && !slices.Contains(types.OperationTypes, operationType) {
		return nil, werrors.NewBadRequestError("invalid operation type")
	}
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, werrors.NewBadRequestError("invalid pagination")
	}
	query := s.db.WithContext(ctx).Model(&types.OperationLog{}).Where("tenant_id=?", tenantID)
	if operationType != "" {
		query = query.Where("operation_type=?", operationType)
	}
	result := &types.OperationLogPage{Items: []types.OperationLog{}, Page: page, PageSize: pageSize}
	if err := query.Count(&result.Total).Error; err != nil {
		return nil, err
	}
	err := query.Order("created_at DESC,id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&result.Items).Error
	if err != nil {
		return nil, err
	}
	actors := []string{}
	for _, item := range result.Items {
		if strings.HasPrefix(item.Actor, "api_key:") {
			actors = append(actors, item.Actor)
		}
	}
	if len(actors) > 0 {
		var keys []struct{ Actor, Name string }
		// Read only names, including revoked keys, without loading credentials.
		err = s.db.WithContext(ctx).Table("tenant_api_keys").
			Select("'api_key:' || id::text AS actor, name").
			Where("tenant_id = ? AND 'api_key:' || id::text IN ?", tenantID, actors).Scan(&keys).Error
		if err != nil {
			return nil, err
		}
		names := make(map[string]string, len(keys))
		for _, key := range keys {
			names[key.Actor] = key.Name
		}
		for i := range result.Items {
			result.Items[i].ActorName = names[result.Items[i].Actor]
		}
	}
	return result, nil
}
