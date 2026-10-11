package container

import (
	"context"
	"fmt"

	"gorm.io/gorm"
	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

func initializeRuntimeData(ctx context.Context, db *gorm.DB, configDirectory string) error {
	paused, err := maintenanceops.SnapshotPaused(ctx, db)
	if err != nil {
		return fmt.Errorf("read startup maintenance state: %w", err)
	}
	if paused {
		logger.Infof(ctx, "Automatic data reconciliation is paused for maintenance")
		return nil
	}
	resetPendingTasks(db)
	if err := types.LoadBuiltinModelsConfig(ctx, db, configDirectory); err != nil {
		logger.Warnf(ctx, "Load builtin models config failed: %v", err)
	}
	return nil
}
