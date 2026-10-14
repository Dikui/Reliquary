package container

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
	"reliquary/internal/application/service"
	"reliquary/internal/logger"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types/interfaces"
)

func startSearchRecordCleanup(db *gorm.DB, cleaner interfaces.ResourceCleaner) error {
	retention, err := service.SearchRecordRetention()
	if err != nil {
		return err
	}
	runner := cron.New()
	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		paused, err := maintenanceops.SnapshotPaused(ctx, db)
		if err != nil || paused {
			return
		}
		if _, err := service.NewSearchRecorder(db).Cleanup(ctx, time.Now(), retention); err != nil {
			logger.Warnf(ctx, "[SearchRecords] cleanup: %v", err)
		}
	}
	if _, err := runner.AddFunc("@hourly", cleanup); err != nil {
		return err
	}
	runner.Start()
	cleaner.RegisterWithName("SearchRecordCleanup", func() error { <-runner.Stop().Done(); return nil })
	return nil
}
