package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"go.uber.org/dig"
	"reliquary/internal/queue"

	"reliquary/internal/application/service"
	"reliquary/internal/logger"
	"reliquary/internal/middleware/taskdl"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type TaskParams struct {
	dig.In
	Client              *queue.Client
	SystemSettings      interfaces.SystemSettingService
	EntryService        interfaces.EntryService
	LibraryService      interfaces.LibraryService
	TopicService        *service.TopicService
	EntityNameEmbedding *service.EntityNameEmbeddingRebuilder
	EmbeddingRebuild    *service.EmbeddingRebuilder
	Reports             *service.VaultDocumentService `optional:"true"`
	SpanTracker         service.SpanTracker
	Cleaner             interfaces.ResourceCleaner
}

// backgroundTaskMiddleware tags every task's context as a background worker
// execution (types.WithBackgroundTask) so the per-model generation concurrency
// governor applies to ingestion/enrichment LLM calls but not to interactive
// foreground model tests.
func backgroundTaskMiddleware() queue.MiddlewareFunc {
	return func(next queue.Handler) queue.Handler {
		return queue.HandlerFunc(func(ctx context.Context, t *queue.Task) error {
			var fields map[string]json.RawMessage
			if json.Unmarshal(t.Payload(), &fields) == nil {
				for name, raw := range fields {
					if !strings.EqualFold(name, "tenant_id") && !strings.EqualFold(name, "space_id") {
						continue
					}
					var spaceID uint64
					if json.Unmarshal(raw, &spaceID) != nil || spaceID != types.SingleSpaceID {
						return fmt.Errorf("task must belong to the space: %w", queue.SkipRetry)
					}
				}
			}
			ctx = context.WithValue(ctx, types.SpaceIDContextKey, types.SingleSpaceID)
			return next.ProcessTask(types.WithBackgroundTask(ctx), t)
		})
	}
}

func resolveWorkerPoolConcurrency(svc interfaces.SystemSettingService) types.WorkerPoolConcurrency {
	if svc == nil {
		return types.DefaultWorkerPoolConcurrency()
	}
	ctx := context.Background()
	return types.ResolveWorkerPoolConcurrency(func(key, env string, fallback int) int {
		value := svc.GetInt(ctx, key, env, int64(fallback))
		return int(value)
	})
}

func RunTaskServer(params TaskParams) *queue.ServeMux {
	mux := queue.NewServeMux()
	mux.Use(chatGPTPermanentFailureMiddleware())
	mux.Use(backgroundTaskMiddleware())
	mux.HandleFunc(types.TypeDocumentProcess, params.EntryService.ProcessDocument)
	mux.HandleFunc(types.TypeManualProcess, params.EntryService.ProcessManualUpdate)
	mux.HandleFunc(types.TypeEntryListDelete, params.EntryService.ProcessEntryListDelete)
	mux.HandleFunc(types.TypeEntryListReparse, params.EntryService.ProcessEntryListReparse)
	mux.HandleFunc(types.TypeEntityNameEmbeddingRebuild, params.EntityNameEmbedding.Handle)
	mux.HandleFunc(types.TypeEmbeddingRebuild, params.EmbeddingRebuild.Handle)
	mux.HandleFunc(types.TypeEntryVector, params.EmbeddingRebuild.HandleEntryVector)
	mux.HandleFunc(types.TypeTopicBackfill, params.TopicService.HandleBackfill)
	if params.Reports != nil {
		mux.HandleFunc(types.TypeReportIndex, params.Reports.HandleReportIndex)
	}
	if assets, ok := params.EntryService.(interface {
		HandleIngestAssetCleanup(context.Context, *queue.Task) error
	}); ok {
		mux.HandleFunc(types.TypeIngestAssetCleanup, assets.HandleIngestAssetCleanup)
	}
	config := queue.Config{Pools: resolveWorkerPoolConcurrency(params.SystemSettings), OnDeadLetter: newDeadLetterEntryFailer(params.EntryService, params.SpanTracker)}
	for name, target := range map[string]*time.Duration{"RELIQUARY_QUEUE_SWEEP_INTERVAL": &config.CheckInterval, "RELIQUARY_QUEUE_DELIVERY_TIMEOUT": &config.DeliveryTimeout} {
		if raw := os.Getenv(name); raw != "" {
			d, err := time.ParseDuration(raw)
			if err != nil || d <= 0 {
				panic(name + " must be a positive Go duration")
			}
			*target = d
		}
	}
	if raw := os.Getenv("RELIQUARY_QUEUE_RECONNECT_ALERT_AFTER"); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration <= 0 {
			panic("RELIQUARY_QUEUE_RECONNECT_ALERT_AFTER must be a positive Go duration")
		}
		config.ReconnectAlertAfter = duration
	}
	if err := params.Client.Start(context.Background(), mux, config); err != nil {
		panic(fmt.Errorf("start durable operation workers: %w", err))
	}
	params.Cleaner.RegisterWithName("RedisTaskQueue", params.Client.Close)
	log.Printf("Redis task workers started: core=%d enrichment=%d maintenance=%d shared=%d", config.Pools.Core, config.Pools.Enrichment, config.Pools.Maintenance, config.Pools.Shared)
	return mux
}

// deadLetterEntryPayload extracts only the field we need from any
// document-related PostgreSQL queue payload. Kept narrow so we don't accidentally
// depend on the full payload schema and survive future field churn.
type deadLetterEntryPayload struct {
	EntryID string `json:"knowledge_id,omitempty"`
	// Attempt threads through DocumentProcess / ManualProcess payloads
	// so the dead-letter callback can also close the
	// matching root span as failed. Older in-flight payloads without
	// this field decode as 0 and the tracker call no-ops.
	Attempt int `json:"attempt,omitempty"`
}

// Processing failures must mark the article failed when retries are exhausted.
var taskTypesAffectingEntryStatus = map[string]struct{}{
	types.TypeDocumentProcess: {},
	types.TypeManualProcess:   {},
	types.TypeReportIndex:     {},
}

type deadLetterEntryListDeletePayload struct {
	EntryIDs []string `json:"knowledge_ids,omitempty"`
}

// newDeadLetterEntryFailer returns the callback wired into the PostgreSQL queue
// dead-letter archive callback. When a document-related task exhausts its retry
// budget, this callback marks the corresponding Entry row as failed so
// the UI surfaces the error instead of a perpetual spinner.
//
// All work is best-effort: missing payload, missing entry_id, or DB
// errors are logged and swallowed. The dead-letter record is the source of
// truth ; this is purely a UX shortcut so users don't wait for the
// housekeeping cron's next sweep.
func newDeadLetterEntryFailer(ks interfaces.EntryService, tracker service.SpanTracker) taskdl.OnDeadLetter {
	if ks == nil {
		return nil
	}
	repo := ks.GetRepository()
	if repo == nil {
		return nil
	}
	return func(ctx context.Context, t *queue.Task, taskErr error) {
		if t == nil {
			return
		}
		if t.Type() == types.TypeEntryListDelete {
			markEntryListDeleteFailed(ctx, repo, t, taskErr)
			return
		}
		if _, ok := taskTypesAffectingEntryStatus[t.Type()]; !ok {
			return
		}
		var probe deadLetterEntryPayload
		if err := json.Unmarshal(t.Payload(), &probe); err != nil || probe.EntryID == "" {
			return
		}
		errMsg := queue.NormalizeError("task " + t.Type() + " exhausted retries: " + taskErr.Error())
		// Single UPDATE so we never end up with parse_status=failed but
		// stale error_message (or vice versa) when the second write
		// fails.
		if err := repo.UpdateEntryColumns(ctx, probe.EntryID, map[string]interface{}{
			"parse_status":  types.ParseStatusFailed,
			"error_message": errMsg,
		}); err != nil {
			logger.Warnf(ctx, "dead-letter callback: failed to mark entry %s as failed: %v", probe.EntryID, err)
			return
		}
		// Close the matching root span so the timeline stops showing
		// "进行中" after dead-letter exhaustion. Best-effort: nil
		// tracker / missing attempt / missing root all no-op cleanly.
		if tracker != nil && probe.Attempt > 0 {
			tracker.FinalizeAttempt(ctx, probe.EntryID, probe.Attempt,
				types.SpanStatusFailed, nil, "TASK_TIMEOUT", errMsg)
		}
		logger.Infof(ctx, "dead-letter callback: marked entry %s as failed (task=%s)", probe.EntryID, t.Type())
	}
}

func markEntryListDeleteFailed(
	ctx context.Context,
	repo interfaces.EntryRepository,
	t *queue.Task,
	taskErr error) {
	var payload deadLetterEntryListDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || len(payload.EntryIDs) == 0 {
		return
	}
	errMsg := queue.NormalizeError("delete task exhausted retries: " + taskErr.Error())
	for _, entryID := range payload.EntryIDs {
		if entryID == "" {
			continue
		}
		updated, err := repo.UpdateActiveDeletingEntryColumns(ctx, entryID, map[string]interface{}{
			"parse_status":  types.ParseStatusFailed,
			"error_message": errMsg,
		})
		if err != nil {
			logger.Warnf(ctx, "dead-letter callback: failed to mark delete failure for entry %s: %v", entryID, err)
			continue
		}
		if !updated {
			logger.Infof(ctx, "dead-letter callback: skipped marking entry %s after delete task exhaustion because it is no longer active deleting", entryID)
			continue
		}
		logger.Infof(ctx, "dead-letter callback: marked entry %s as failed after delete task exhausted retries", entryID)
	}
}
