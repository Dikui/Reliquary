package service

import (
	"context"
	"errors"
	"strings"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// Sentinel errors so the handler can map cleanly to HTTP status codes
// without leaking GORM internals.
var (
	ErrFavoriteInvalidType = errors.New("invalid favorite resource type")
	ErrFavoriteEmptyID     = errors.New("favorite resource id is required")
	ErrFavoriteInvalidTag  = errors.New("only topic, object, and event tags can be starred")
)

type resourceFavoriteService struct {
	repo interfaces.ResourceFavoriteRepository
}

// NewResourceFavoriteService wraps the repository with input
// validation (allowlist of resource types, non-empty resource id).
// We keep service thin on purpose — favoriting is a non-business action
// that doesn't need audit logging or cross-aggregate side effects.
func NewResourceFavoriteService(repo interfaces.ResourceFavoriteRepository) interfaces.ResourceFavoriteService {
	return &resourceFavoriteService{repo: repo}
}

func (s *resourceFavoriteService) List(
	ctx context.Context, tenantID uint64, resourceType string,
) ([]*types.ResourceFavorite, error) {
	if !types.IsValidFavoriteResourceType(resourceType) {
		return nil, ErrFavoriteInvalidType
	}
	return s.repo.List(ctx, tenantID, resourceType)
}

func (s *resourceFavoriteService) Add(
	ctx context.Context, tenantID uint64, resourceType, resourceID string,
) error {
	if !types.IsValidFavoriteResourceType(resourceType) {
		return ErrFavoriteInvalidType
	}
	if strings.TrimSpace(resourceID) == "" {
		return ErrFavoriteEmptyID
	}
	if resourceType == types.ResourceTypeTag {
		valid, err := s.repo.IsConcreteTag(ctx, tenantID, resourceID)
		if err != nil {
			return err
		}
		if !valid {
			return ErrFavoriteInvalidTag
		}
	}
	_, err := s.repo.Add(ctx, tenantID, resourceType, resourceID)
	return err
}

func (s *resourceFavoriteService) Remove(
	ctx context.Context, tenantID uint64, resourceType, resourceID string,
) error {
	if !types.IsValidFavoriteResourceType(resourceType) {
		return ErrFavoriteInvalidType
	}
	if strings.TrimSpace(resourceID) == "" {
		return ErrFavoriteEmptyID
	}
	_, err := s.repo.Remove(ctx, tenantID, resourceType, resourceID)
	return err
}
