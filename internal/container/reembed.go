package container

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/panjf2000/ants/v2"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
)

// Reembed runs synchronously for CLI use with the same persistent checkpoints
// as the background settings task. It does not start HTTP or background workers.
func Reembed(ctx context.Context, action, runID string, progress func(*types.EmbeddingRebuildRun)) (result *types.EmbeddingRebuildStatus, resultErr error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, err
	}
	db, err := initDatabase(cfg)
	if err != nil {
		return nil, err
	}
	pool, err := db.DB()
	if err != nil {
		return nil, err
	}
	defer pool.Close()
	workers, err := ants.NewPool(2)
	if err != nil {
		return nil, err
	}
	defer workers.Release()
	ollamaClient, err := initOllamaService()
	if err != nil {
		return nil, err
	}
	models := service.NewModelServiceWithRoutingDB(repository.NewModelRepository(db), repository.NewLibraryRepository(db), ollamaClient,
		embedding.NewBatchEmbedder(workers), service.NewSpaceService(repository.NewSpaceRepository(db)), db)
	vault, err := service.NewVaultService(db, cfg)
	if err != nil {
		return nil, err
	}
	defer func() {
		// Offline commands have no running replica writer. Persist every audit
		// checkpoint before the process exits, including failure and Ctrl+C.
		flushCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		resultErr = errors.Join(resultErr, vault.Flush(flushCtx), vault.Close())
	}()
	rebuilder := service.NewEmbeddingRebuilder(db, models, nil, vault)
	if action == "maintenance_start" {
		rebuilder.PreserveEntityProfileTimestamps(true)
		action = "start"
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, types.SingleSpaceID)
	switch action {
	case "start":
		run, startErr := rebuilder.Start(ctx, types.SingleSpaceID)
		if startErr != nil {
			return nil, startErr
		}
		runID = run.ID
		if progress != nil {
			progress(run)
		}
	case "resume", "retry_failed":
		if err := rebuilder.Continue(ctx, types.SingleSpaceID, runID, action == "retry_failed"); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported reembed action %q", action)
	}
	runErr := rebuilder.Run(ctx, types.SingleSpaceID, runID, progress)
	// Signal cancellation still returns the durable paused status to the CLI.
	status, statusErr := rebuilder.StatusForRun(context.WithoutCancel(ctx), types.SingleSpaceID, runID)
	if statusErr != nil {
		return nil, statusErr
	}
	return status, runErr
}
