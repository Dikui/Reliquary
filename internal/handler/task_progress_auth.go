package handler

import (
	"context"

	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// requireTaskProgressSpace ensures async task progress endpoints only
// return data for tasks created under the caller's space. Cross-space
// probes are hidden as not-found to avoid confirming task existence.
func requireTaskProgressSpace(ctx context.Context, taskID string) error {
	taskSpaceID, err := utils.TaskSpaceID(taskID)
	if err != nil {
		return apperrors.NewBadRequestError("invalid task ID")
	}
	callerSpaceID, ok := types.SpaceIDFromContext(ctx)
	if !ok || callerSpaceID == 0 {
		return apperrors.NewUnauthorizedError("Unauthorized")
	}
	if taskSpaceID != callerSpaceID {
		return apperrors.NewNotFoundError("task not found")
	}
	return nil
}
