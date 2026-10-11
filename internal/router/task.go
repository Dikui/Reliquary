package router

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"go.uber.org/dig"

	"reliquary/internal/application/service"
	"reliquary/internal/common"
	"reliquary/internal/logger"
	"reliquary/internal/middleware/asynqdl"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type AsynqTaskParams struct {
	dig.In

	// Dedicated servers provide minimum capacity for article processing,
	// enrichment and maintenance. The shared server subscribes to queues so stages can borrow
	// otherwise-idle capacity without consuming the other stage's guarantee.
	CoreServer           *asynq.Server `name:"coreAsynqServer"`
	EnrichmentServer     *asynq.Server `name:"enrichmentAsynqServer"`
	MaintenanceServer    *asynq.Server `name:"maintenanceAsynqServer"`
	SharedServer         *asynq.Server `name:"sharedAsynqServer"`
	KnowledgeService     interfaces.KnowledgeService
	KnowledgeBaseService interfaces.KnowledgeBaseService
	DataSourceService    interfaces.DataSourceService
	EntityNameEmbedding  *service.EntityNameEmbeddingRebuilder
	EmbeddingRebuild     *service.EmbeddingRebuilder
	TaskEnqueuer         interfaces.TaskEnqueuer `optional:"true"`
	Inspector            *asynq.Inspector        `optional:"true"`
	DeadLetterRepo       interfaces.TaskDeadLetterRepository
	SpanTracker          service.SpanTracker
}

// defaultRedisOpTimeout is the previous hard-coded read timeout. The 100ms
// floor was tight enough to cause spurious i/o timeout errors during bursty
// workloads (large batch uploads), so we
// raise the default to 500ms while still allowing operators to tune via env.
const defaultRedisOpTimeoutMs = 500

// readRedisOpTimeoutMs reads RELIQUARY_REDIS_OP_TIMEOUT_MS, falling back to
// defaultRedisOpTimeoutMs on missing/invalid input. Kept as a separate helper
// so both ReadTimeout and WriteTimeout share the same source of truth.
func readRedisOpTimeoutMs() int {
	if v := strings.TrimSpace(os.Getenv("RELIQUARY_REDIS_OP_TIMEOUT_MS")); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultRedisOpTimeoutMs
}

func getAsynqRedisClientOpt() *asynq.RedisClientOpt {
	db := 0
	if dbStr := os.Getenv("REDIS_DB"); dbStr != "" {
		if parsed, err := strconv.Atoi(dbStr); err == nil {
			db = parsed
		}
	}
	timeoutMs := readRedisOpTimeoutMs()
	opt := &asynq.RedisClientOpt{
		Addr:        os.Getenv("REDIS_ADDR"),
		Username:    os.Getenv("REDIS_USERNAME"),
		Password:    os.Getenv("REDIS_PASSWORD"),
		ReadTimeout: time.Duration(timeoutMs) * time.Millisecond,
		// Writes are typically more sensitive to congestion than reads
		// (RESP pipelining, BRPOPLPUSH on Asynq dequeue), so we keep
		// WriteTimeout slightly larger to absorb head-of-line stalls.
		WriteTimeout: time.Duration(timeoutMs*2) * time.Millisecond,
		DB:           db,
		TLSConfig:    common.RedisTLSConfig(),
	}
	return opt
}

func NewAsyncqClient() (*asynq.Client, error) {
	opt := getAsynqRedisClientOpt()
	client := asynq.NewClient(opt)
	err := client.Ping()
	if err != nil {
		return nil, err
	}
	return client, nil
}

// newAsynqServer builds an asynq server bound to a specific queue set and
// concurrency. Every worker pool uses it so Redis options and retry-delay
// policy stay consistent across the topology.
func newAsynqServer(concurrency int, queues map[string]int) *asynq.Server {
	opt := getAsynqRedisClientOpt()
	return asynq.NewServer(
		opt,
		asynq.Config{
			Concurrency:    concurrency,
			Queues:         queues,
			RetryDelayFunc: chatGPTTaskRetryDelay,
			IsFailure:      chatGPTTaskIsFailure,
		})
}

// backgroundTaskMiddleware tags every task's context as a background worker
// execution (types.WithBackgroundTask) so the per-model generation concurrency
// governor applies to ingestion/enrichment LLM calls but not to interactive
// foreground model tests.
func backgroundTaskMiddleware() asynq.MiddlewareFunc {
	return func(next asynq.Handler) asynq.Handler {
		return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
			var fields map[string]json.RawMessage
			if json.Unmarshal(t.Payload(), &fields) == nil {
				for name, raw := range fields {
					if !strings.EqualFold(name, "tenant_id") {
						continue
					}
					var tenantID uint64
					if json.Unmarshal(raw, &tenantID) != nil || tenantID != types.SingleTenantID {
						return fmt.Errorf("task must belong to the space: %w", asynq.SkipRetry)
					}
				}
			}
			ctx = context.WithValue(ctx, types.TenantIDContextKey, types.SingleTenantID)
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

// NewCoreAsynqServer runs document and manual parsing with guaranteed capacity.
func NewCoreAsynqServer(svc interfaces.SystemSettingService) *asynq.Server {
	allocation := resolveWorkerPoolConcurrency(svc)
	log.Printf("asynq core-pool server starting with concurrency=%d total_upstream=%d redis_op_timeout=%dms",
		allocation.Core, allocation.UpstreamTotal(), readRedisOpTimeoutMs())
	return newAsynqServer(allocation.Core, types.QueueWeightsForPool(types.WorkerPoolCore))
}

// NewEnrichmentAsynqServer rebuilds entity name vectors without consuming
// article processing capacity.
func NewEnrichmentAsynqServer(svc interfaces.SystemSettingService) *asynq.Server {
	allocation := resolveWorkerPoolConcurrency(svc)
	log.Printf("asynq enrichment-pool server starting with concurrency=%d total_upstream=%d",
		allocation.Enrichment, allocation.UpstreamTotal())
	return newAsynqServer(allocation.Enrichment, types.QueueWeightsForPool(types.WorkerPoolEnrichment))
}

// NewMaintenanceAsynqServer runs connector sync, cleanup, deletion, and batch
// dispatch work. QueueMaintenance keeps the legacy Redis name "low", so old
// tasks are drained safely during rolling upgrades.
func NewMaintenanceAsynqServer(svc interfaces.SystemSettingService) *asynq.Server {
	allocation := resolveWorkerPoolConcurrency(svc)
	log.Printf("asynq maintenance-pool server starting with concurrency=%d total_upstream=%d",
		allocation.Maintenance, allocation.UpstreamTotal())
	return newAsynqServer(allocation.Maintenance, types.QueueWeightsForPool(types.WorkerPoolMaintenance))
}

// NewSharedAsynqServer is the elastic tier. Asynq dequeue is atomic, so it is
// safe for this server and the dedicated servers to subscribe to the same
// queues: every task is still executed by exactly one worker.
func NewSharedAsynqServer(svc interfaces.SystemSettingService) *asynq.Server {
	allocation := resolveWorkerPoolConcurrency(svc)
	log.Printf("asynq shared-pool server starting with concurrency=%d total_upstream=%d",
		allocation.Shared, allocation.UpstreamTotal())
	return newAsynqServer(allocation.Shared, types.QueueWeightsForSharedPool())
}

func RunAsynqServer(params AsynqTaskParams) *asynq.ServeMux {
	// Create a new mux and register all handlers
	mux := asynq.NewServeMux()

	// Install the dead-letter middleware FIRST so it sees the raw error
	// returned by the handler, before any other middleware that might
	// transform it. The middleware records one task_dead_letters row per
	// task that exhausts its retry budget — operators can then SQL-query
	// failures by task type, scope, or tenant without scraping logs.
	// Best-effort: a DB failure is logged and swallowed; the original task
	// error always propagates upstream to asynq for retry/archival.
	//
	// The callback flips Knowledge.parse_status to "failed" the moment a
	// document-related task exhausts its retry budget. Without this hook,
	// a permanently-failing task left its parent knowledge stranded in
	// "processing" until housekeeping cron caught it minutes later — the
	// UI signal users actually see.
	knowledgeFailer := newDeadLetterKnowledgeFailer(params.KnowledgeService, params.SpanTracker)
	mux.Use(chatGPTFinalDeferralMiddleware(params.TaskEnqueuer, params.Inspector))
	mux.Use(asynqdl.MiddlewareWithCallback(params.DeadLetterRepo, knowledgeFailer))

	// Mark every asynq worker execution as a background task so the generation
	// concurrency governor throttles ingestion/enrichment LLM traffic while
	// leaving foreground model tests ungated. Installed early so all
	// downstream handlers (and their model calls) inherit the flag.
	mux.Use(backgroundTaskMiddleware())

	// Register document processing handler
	mux.HandleFunc(types.TypeDocumentProcess, params.KnowledgeService.ProcessDocument)

	// Register manual knowledge processing handler (cleanup + re-indexing)
	mux.HandleFunc(types.TypeManualProcess, params.KnowledgeService.ProcessManualUpdate)

	// Register knowledge list delete handler
	mux.HandleFunc(types.TypeKnowledgeListDelete, params.KnowledgeService.ProcessKnowledgeListDelete)

	// Register knowledge list reparse handler
	mux.HandleFunc(types.TypeKnowledgeListReparse, params.KnowledgeService.ProcessKnowledgeListReparse)

	// Register entity name vector rebuild handler
	mux.HandleFunc(types.TypeEntityNameEmbeddingRebuild, params.EntityNameEmbedding.Handle)
	mux.HandleFunc(types.TypeEmbeddingRebuild, params.EmbeddingRebuild.Handle)

	// Register data source sync handler
	mux.HandleFunc(types.TypeDataSourceSync, params.DataSourceService.ProcessSync)

	// Run the same mux on every pool. Shared and dedicated servers intentionally
	// overlap, but Redis dequeue is atomic, so each task still executes once.
	runPool := func(name string, srv *asynq.Server) {
		go func() {
			if err := srv.Run(mux); err != nil {
				log.Fatalf("could not run %s asynq server: %v", name, err)
			}
		}()
	}
	runPool("core-pool", params.CoreServer)
	runPool("enrichment-pool", params.EnrichmentServer)
	runPool("maintenance-pool", params.MaintenanceServer)
	runPool("shared-pool", params.SharedServer)
	return mux
}

// deadLetterKnowledgePayload extracts only the field we need from any
// document-related asynq payload. Kept narrow so we don't accidentally
// depend on the full payload schema and survive future field churn.
type deadLetterKnowledgePayload struct {
	KnowledgeID string `json:"knowledge_id,omitempty"`
	// Attempt threads through DocumentProcess / ManualProcess payloads
	// so the dead-letter callback can also close the
	// matching root span as failed. Older in-flight payloads without
	// this field decode as 0 and the tracker call no-ops.
	Attempt int `json:"attempt,omitempty"`
}

// Processing failures must mark the article failed when retries are exhausted.
var taskTypesAffectingKnowledgeStatus = map[string]struct{}{
	types.TypeDocumentProcess: {},
	types.TypeManualProcess:   {},
}

type deadLetterKnowledgeListDeletePayload struct {
	KnowledgeIDs []string `json:"knowledge_ids,omitempty"`
}

// newDeadLetterKnowledgeFailer returns the callback wired into the asynq
// dead-letter middleware. When a document-related task exhausts its retry
// budget, this callback marks the corresponding Knowledge row as failed so
// the UI surfaces the error instead of a perpetual spinner.
//
// All work is best-effort: missing payload, missing knowledge_id, or DB
// errors are logged and swallowed. The dead-letter record is the source of
// truth — this is purely a UX shortcut so users don't wait for the
// housekeeping cron's next sweep.
func newDeadLetterKnowledgeFailer(ks interfaces.KnowledgeService, tracker service.SpanTracker) asynqdl.OnDeadLetter {
	if ks == nil {
		return nil
	}
	repo := ks.GetRepository()
	if repo == nil {
		return nil
	}
	return func(ctx context.Context, t *asynq.Task, taskErr error) {
		if t == nil {
			return
		}
		if t.Type() == types.TypeKnowledgeListDelete {
			markKnowledgeListDeleteFailed(ctx, repo, t, taskErr)
			return
		}
		if _, ok := taskTypesAffectingKnowledgeStatus[t.Type()]; !ok {
			return
		}
		var probe deadLetterKnowledgePayload
		if err := json.Unmarshal(t.Payload(), &probe); err != nil || probe.KnowledgeID == "" {
			return
		}
		errMsg := "task " + t.Type() + " exhausted retries: " + taskErr.Error()
		// 8KB is the same cap the dead-letter row uses for last_error.
		if len(errMsg) > 8192 {
			errMsg = errMsg[:8192]
		}
		// Single UPDATE so we never end up with parse_status=failed but
		// stale error_message (or vice versa) when the second write
		// fails.
		if err := repo.UpdateKnowledgeColumns(ctx, probe.KnowledgeID, map[string]interface{}{
			"parse_status":  types.ParseStatusFailed,
			"error_message": errMsg,
		}); err != nil {
			logger.Warnf(ctx, "dead-letter callback: failed to mark knowledge %s as failed: %v", probe.KnowledgeID, err)
			return
		}
		// Close the matching root span so the timeline stops showing
		// "进行中" after dead-letter exhaustion. Best-effort: nil
		// tracker / missing attempt / missing root all no-op cleanly.
		if tracker != nil && probe.Attempt > 0 {
			tracker.FinalizeAttempt(ctx, probe.KnowledgeID, probe.Attempt,
				types.SpanStatusFailed, nil, "TASK_TIMEOUT", errMsg)
		}
		logger.Infof(ctx, "dead-letter callback: marked knowledge %s as failed (task=%s)", probe.KnowledgeID, t.Type())
	}
}

func markKnowledgeListDeleteFailed(
	ctx context.Context,
	repo interfaces.KnowledgeRepository,
	t *asynq.Task,
	taskErr error) {
	var payload deadLetterKnowledgeListDeletePayload
	if err := json.Unmarshal(t.Payload(), &payload); err != nil || len(payload.KnowledgeIDs) == 0 {
		return
	}
	errMsg := "delete task exhausted retries: " + taskErr.Error()
	if len(errMsg) > 8192 {
		errMsg = errMsg[:8192]
	}
	for _, knowledgeID := range payload.KnowledgeIDs {
		if knowledgeID == "" {
			continue
		}
		updated, err := repo.UpdateActiveDeletingKnowledgeColumns(ctx, knowledgeID, map[string]interface{}{
			"parse_status":  types.ParseStatusFailed,
			"error_message": errMsg,
		})
		if err != nil {
			logger.Warnf(ctx, "dead-letter callback: failed to mark delete failure for knowledge %s: %v", knowledgeID, err)
			continue
		}
		if !updated {
			logger.Infof(ctx, "dead-letter callback: skipped marking knowledge %s after delete task exhaustion because it is no longer active deleting", knowledgeID)
			continue
		}
		logger.Infof(ctx, "dead-letter callback: marked knowledge %s as failed after delete task exhausted retries", knowledgeID)
	}
}

func chatGPTTaskRetryDelay(n int, err error, task *asynq.Task) time.Duration {
	var deferred *types.ChatGPTDeferredError
	if errors.As(err, &deferred) {
		return max(time.Minute, time.Until(deferred.RetryAt))
	}
	return asynq.DefaultRetryDelayFunc(n, err, task)
}

func chatGPTTaskIsFailure(err error) bool { return !types.IsChatGPTDeferred(err) }
