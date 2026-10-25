package container

import (
	"context"
	"reliquary/internal/application/service"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"time"

	"github.com/robfig/cron/v3"
)

func startStoryConsolidation(svc *service.StoryConsolidator, cleaner interfaces.ResourceCleaner) error {
	runner := cron.New(cron.WithChain(cron.Recover(cron.DefaultLogger), cron.SkipIfStillRunning(cron.DefaultLogger)))
	ctx, cancel := context.WithCancel(context.Background())
	if _, err := runner.AddFunc("@hourly", func() {
		runCtx, stop := context.WithTimeout(ctx, 30*time.Minute)
		defer stop()
		runCtx = context.WithValue(runCtx, types.SpaceIDContextKey, uint64(types.SingleSpaceID))
		if err := svc.Consolidate(runCtx); err != nil {
			logger.Warnf(runCtx, "[StoryConsolidation] sweep failed: %v", err)
		}
	}); err != nil {
		cancel()
		return err
	}
	runner.Start()
	cleaner.RegisterWithName("StoryConsolidation", func() error {
		cancel()
		<-runner.Stop().Done()
		return nil
	})
	return nil
}
