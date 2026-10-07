package container

import (
	"context"
	"time"

	"gorm.io/gorm"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

const resetPendingStaleWindow = 30 * time.Minute

// resetPendingTasks cleans stale synchronization logs after a restart.
// Asynq persists knowledge and summary tasks. Housekeeping checks their queue
// and span activity, so startup must preserve those rows across replicas.
func resetPendingTasks(db *gorm.DB) {
	now := time.Now()
	result := db.Model(&types.SyncLog{}).
		Where("status = ? AND started_at < ?", types.SyncLogStatusRunning, now.Add(-resetPendingStaleWindow)).
		Updates(map[string]interface{}{
			"status":        types.SyncLogStatusFailed,
			"error_message": "Sync interrupted due to application restart",
			"finished_at":   &now,
		})
	if result.Error != nil {
		logger.Warnf(context.Background(), "Failed to reset pending data source sync tasks: %v", result.Error)
	} else if result.RowsAffected > 0 {
		logger.Infof(context.Background(), "Reset %d stuck data source sync tasks to failed state", result.RowsAffected)
	}
}
