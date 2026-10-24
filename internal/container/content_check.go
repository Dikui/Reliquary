package container

import (
	"context"
	"reliquary/internal/application/service"
	"reliquary/internal/logger"
	"reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"time"

	"github.com/robfig/cron/v3"
	"gorm.io/gorm"
)

func newContentCheckService(
	db *gorm.DB,
	q *queue.Client,
	models interfaces.ModelService,
	entries interfaces.EntryService,
	search interfaces.SearchService,
	entities *service.EntityService,
) *service.ContentCheckService {
	return service.NewContentCheckService(db, q, models, entries, search, entities)
}

func startContentChecks(s *service.ContentCheckService, cleaner interfaces.ResourceCleaner) error {
	runner := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DefaultLogger), cron.Recover(cron.DefaultLogger)))
	tick := func() {
		ctx, cancel := context.WithTimeout(context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID), time.Minute)
		defer cancel()
		if err := s.Tick(ctx); err != nil {
			logger.Warnf(ctx, "[ContentCheck] schedule: %v", err)
		}
	}
	if _, err := runner.AddFunc("* * * * *", tick); err != nil {
		return err
	}
	runner.Start()
	cleaner.RegisterWithName("ContentCheckSchedule", func() error { <-runner.Stop().Done(); return nil })
	return nil
}
