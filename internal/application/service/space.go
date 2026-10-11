package service

import (
	"context"
	"errors"
	"time"

	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// spaceService implements the SpaceService interface
type spaceService struct {
	repo interfaces.SpaceRepository // Repository for space data operations
}

// NewSpaceService creates a new space service instance
func NewSpaceService(repo interfaces.SpaceRepository) interfaces.SpaceService {
	return &spaceService{repo: repo}
}

// CreateSpace creates a new space
func (s *spaceService) CreateSpace(ctx context.Context, space *types.Space) (*types.Space, error) {
	logger.Info(ctx, "Start creating space")

	if space.Name == "" {
		logger.Error(ctx, "Workspace name cannot be empty")
		return nil, errors.New("workspace name cannot be empty")
	}

	logger.Infof(ctx, "Creating space, name: %s", space.Name)

	// New spaces do not receive an API key by default. Integrations create
	// keys explicitly through api_keys.
	space.Status = "active"
	space.CreatedAt = time.Now()
	space.UpdatedAt = time.Now()

	logger.Info(ctx, "Saving space information to database")
	if err := s.repo.CreateSpace(ctx, space); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_name": space.Name,
		})
		return nil, err
	}

	logger.Infof(ctx, "Space created successfully, ID: %d, name: %s", space.ID, space.Name)
	return space, nil
}

// GetSpaceByID retrieves a space by their ID
func (s *spaceService) GetSpaceByID(ctx context.Context, id uint64) (*types.Space, error) {
	if id == 0 {
		logger.Error(ctx, "Workspace ID cannot be 0")
		return nil, errors.New("space ID cannot be 0")
	}

	space, err := s.repo.GetSpaceByID(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id": id,
		})
		return nil, err
	}

	return space, nil
}

// GetSpacesByIDs batches GetSpaceByID; returns a map keyed by space ID.
func (s *spaceService) GetSpacesByIDs(ctx context.Context, ids []uint64) (map[uint64]*types.Space, error) {
	return s.repo.GetSpacesByIDs(ctx, ids)
}

// ListSpaces retrieves a list of all spaces
func (s *spaceService) ListSpaces(ctx context.Context) ([]*types.Space, error) {
	spaces, err := s.repo.ListSpaces(ctx)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return nil, err
	}

	logger.Infof(ctx, "Space list retrieved successfully, total: %d", len(spaces))
	return spaces, nil
}

// UpdateSpace updates an existing space's information
func (s *spaceService) UpdateSpace(ctx context.Context, space *types.Space) (*types.Space, error) {
	if space.ID == 0 {
		logger.Error(ctx, "Workspace ID cannot be 0")
		return nil, errors.New("space ID cannot be 0")
	}

	logger.Infof(ctx, "Updating space, ID: %d, name: %s", space.ID, space.Name)

	space.UpdatedAt = time.Now()
	logger.Info(ctx, "Saving space information to database")

	if err := s.repo.UpdateSpace(ctx, space); err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id": space.ID,
		})
		return nil, err
	}

	logger.Infof(ctx, "Space updated successfully, ID: %d", space.ID)
	return space, nil
}

// DeleteSpace removes a space by their ID
func (s *spaceService) DeleteSpace(ctx context.Context, id uint64) error {
	logger.Info(ctx, "Start deleting space")

	if id == 0 {
		logger.Error(ctx, "Workspace ID cannot be 0")
		return errors.New("space ID cannot be 0")
	}

	logger.Infof(ctx, "Deleting space, ID: %d", id)

	// Get space information for logging
	space, err := s.repo.GetSpaceByID(ctx, id)
	if err != nil {
		if err.Error() == "record not found" {
			logger.Warnf(ctx, "Space to be deleted does not exist, ID: %d", id)
		} else {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{
				"space_id": id,
			})
			return err
		}
	} else {
		logger.Infof(ctx, "Deleting space, ID: %d, name: %s", id, space.Name)
	}

	err = s.repo.DeleteSpace(ctx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"space_id": id,
		})
		return err
	}

	logger.Infof(ctx, "Workspace deleted successfully, ID: %d", id)
	return nil
}
