package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/database"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	modellimiter "reliquary/internal/models/limiter"
	"reliquary/internal/runtime"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type runtimeKnowledgeCanceller interface {
	CancelKnowledgeParse(ctx context.Context, knowledgeID string) (*types.Knowledge, error)
}

// SystemHandler handles system-related requests
type SystemHandler struct {
	cfg              *config.Config
	tenantSvc        interfaces.TenantService
	userSvc          interfaces.UserService
	systemSettingSvc interfaces.SystemSettingService
	apiKeySvc        interfaces.TenantAPIKeyService
	taskInspector    interfaces.TaskInspector
	// knowledgeSvc supplies the domain-level cancellation path used by the
	// runtime task console. It updates business state and tracing before queue
	// records are removed, unlike a raw Redis deletion.
	knowledgeSvc runtimeKnowledgeCanceller
	// startup snapshot for GET /system/capabilities; bound in router.NewRouter.
	deploymentCapabilities DeploymentCapabilitiesData
}

// NewSystemHandler creates a new system handler
func NewSystemHandler(cfg *config.Config,
	tenantSvc interfaces.TenantService,
	userSvc interfaces.UserService,
	systemSettingSvc interfaces.SystemSettingService,
	apiKeySvc interfaces.TenantAPIKeyService,
	taskInspector interfaces.TaskInspector,
	knowledgeSvc interfaces.KnowledgeService,
) *SystemHandler {
	return &SystemHandler{
		cfg:              cfg,
		tenantSvc:        tenantSvc,
		userSvc:          userSvc,
		systemSettingSvc: systemSettingSvc,
		apiKeySvc:        apiKeySvc,
		taskInspector:    taskInspector,
		knowledgeSvc:     knowledgeSvc,
	}
}

// GetSystemInfoResponse defines the response structure for system info
type GetSystemInfoResponse struct {
	CredentialEncryptionEnabled bool   `json:"credential_encryption_enabled"`
	Version                     string `json:"version"`
	Edition                     string `json:"edition"`
	CommitID                    string `json:"commit_id,omitempty"`
	BuildTime                   string `json:"build_time,omitempty"`
	GoVersion                   string `json:"go_version,omitempty"`
	KeywordIndexEngine          string `json:"keyword_index_engine,omitempty"`
	DBVersion                   string `json:"db_version,omitempty"`
	// DBMigrationError carries the human-readable error message recorded when
	// the most recent startup migration attempt failed. Empty when migrations
	// succeeded; non-empty values let the frontend surface a troubleshooting
	// banner instead of silently hiding the DB version row (see issue #1319).
	DBMigrationError string `json:"db_migration_error,omitempty"`
	// StartedAt is the server process boot time (RFC3339, UTC).
	StartedAt string `json:"started_at,omitempty"`
	// UptimeSeconds is seconds elapsed since process start.
	UptimeSeconds int64 `json:"uptime_seconds,omitempty"`
}

// 编译时注入的版本信息
var (
	Version   = "unknown"
	Edition   = "standard"
	CommitID  = "unknown"
	BuildTime = "unknown"
	GoVersion = "unknown"
)

// GetSystemInfo godoc
func (h *SystemHandler) GetSystemInfo(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())

	// PostgreSQL provides both keyword and vector retrieval.
	keywordIndexEngine := h.getKeywordIndexEngine()

	dbMigrationErr := database.CachedMigrationError()
	var dbVersion string
	if ver, dirty, ok := database.CachedMigrationVersion(); ok {
		dbVersion = fmt.Sprintf("%d", ver)
		if dirty {
			dbVersion += " (dirty)"
		}
		if dbMigrationErr != "" {
			dbVersion += " (failed)"
		}
	} else if dbMigrationErr != "" {
		// Failure happened before m.Version() could be read (e.g. could not
		// open the database). Still emit a placeholder so the frontend renders
		// the row and shows the troubleshooting banner.
		dbVersion = "unknown"
	}

	var startedAt string
	var uptimeSec int64
	if boot := runtime.ServerStartedAt(); !boot.IsZero() {
		startedAt = boot.UTC().Format(time.RFC3339)
		uptimeSec = int64(runtime.ServerUptime().Seconds())
	}

	response := GetSystemInfoResponse{
		CredentialEncryptionEnabled: utils.GetAESKey() != nil,
		Version:                     Version,
		Edition:                     Edition,
		CommitID:                    CommitID,
		BuildTime:                   BuildTime,
		GoVersion:                   GoVersion,
		KeywordIndexEngine:          keywordIndexEngine,
		DBVersion:                   dbVersion,
		DBMigrationError:            dbMigrationErr,
		StartedAt:                   startedAt,
		UptimeSeconds:               uptimeSec,
	}

	logger.Info(ctx, "System info retrieved successfully")
	c.JSON(200, gin.H{
		"code": 0,
		"msg":  "success",
		"data": response,
	})
}

// ListParserEngines returns available document parser engines.
// Lists the four retained parser engines with their availability.
func (h *SystemHandler) ListParserEngines(c *gin.Context) {
	var overrides map[string]string
	if v, exists := c.Get(types.TenantInfoContextKey.String()); exists {
		if tenant, ok := v.(*types.Tenant); ok && tenant != nil {
			if tenant.ParserEngineConfig != nil {
				overrides = tenant.ParserEngineConfig.ToOverridesMap()
			}
		}
	}

	engines := docparser.ListAllEngines(overrides)
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": engines})
}

// CheckParserEngines runs availability check with the given config overrides (e.g. current form values).
// Used to test engine availability without saving; body shape matches ParserEngineConfig.
func (h *SystemHandler) CheckParserEngines(c *gin.Context) {
	var body types.ParserEngineConfig
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"code": 1, "msg": "请求体格式错误"})
		return
	}
	var existing *types.ParserEngineConfig
	if v, exists := c.Get(types.TenantInfoContextKey.String()); exists {
		if t, ok := v.(*types.Tenant); ok && t != nil {
			existing = t.ParserEngineConfig
		}
	}
	merged := types.MergeParserEngineConfigForUpdate(&body, existing)
	overrides := merged.ToOverridesMap()
	engines := docparser.ListAllEngines(overrides)
	c.JSON(200, gin.H{"code": 0, "msg": "success", "data": engines})
}

// getKeywordIndexEngine returns the application's PostgreSQL keyword engine.
func (h *SystemHandler) getKeywordIndexEngine() string {
	return string(types.PostgresRetrieverEngineType)
}

func aggregateRuntimeWorkerPools(pools []RuntimeWorkerPool, servers []types.WorkerServerStat) {
	queueConfigs := map[string]map[string]int{
		types.WorkerPoolCore:        types.QueueWeightsForPool(types.WorkerPoolCore),
		types.WorkerPoolPostProcess: types.QueueWeightsForPool(types.WorkerPoolPostProcess),
		types.WorkerPoolEnrichment:  types.QueueWeightsForPool(types.WorkerPoolEnrichment),
		types.WorkerPoolMaintenance: types.QueueWeightsForPool(types.WorkerPoolMaintenance),
		types.WorkerPoolShared:      types.QueueWeightsForSharedPool(),
	}
	indexes := make(map[string]int, len(pools))
	for i := range pools {
		indexes[pools[i].Name] = i
	}
	for _, server := range servers {
		if server.Status != "active" {
			continue
		}
		for poolName, expectedQueues := range queueConfigs {
			if !sameQueueWeights(server.Queues, expectedQueues) {
				continue
			}
			idx, ok := indexes[poolName]
			if !ok {
				break
			}
			pools[idx].Instances++
			pools[idx].ClusterCapacity += server.Concurrency
			pools[idx].Active += server.Active
			break
		}
	}
	for i := range pools {
		if pools[i].ClusterCapacity > 0 {
			pools[i].Utilization = float64(pools[i].Active) / float64(pools[i].ClusterCapacity)
		}
	}
}

func sameQueueWeights(left, right map[string]int) bool {
	if len(left) != len(right) {
		return false
	}
	for name, weight := range left {
		if right[name] != weight {
			return false
		}
	}
	return true
}

// GetRuntimeQueues godoc
func (h *SystemHandler) GetRuntimeQueues(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	var allocation types.WorkerPoolConcurrency
	if h.systemSettingSvc != nil {
		allocation = types.ResolveWorkerPoolConcurrency(func(key, env string, fallback int) int {
			configured := h.systemSettingSvc.GetInt(ctx, key, env, int64(fallback))
			return int(configured)
		})
	} else {
		allocation = types.DefaultWorkerPoolConcurrency()
	}
	queueCounts := make(map[string]int)
	for _, definition := range types.QueueDefinitions() {
		queueCounts[definition.Pool]++
	}
	resp := RuntimeQueuesResponse{
		UpstreamConcurrency: allocation.UpstreamTotal(),
		ParseConcurrency:    allocation.UpstreamTotal(),
		Pools: []RuntimeWorkerPool{
			{Name: types.WorkerPoolCore, Concurrency: allocation.Core, QueueCount: queueCounts[types.WorkerPoolCore]},
			{Name: types.WorkerPoolPostProcess, Concurrency: allocation.PostProcess, QueueCount: queueCounts[types.WorkerPoolPostProcess]},
			{Name: types.WorkerPoolEnrichment, Concurrency: allocation.Enrichment, QueueCount: queueCounts[types.WorkerPoolEnrichment]},
			{Name: types.WorkerPoolMaintenance, Concurrency: allocation.Maintenance, QueueCount: queueCounts[types.WorkerPoolMaintenance]},
			{Name: types.WorkerPoolShared, Concurrency: allocation.Shared, QueueCount: len(types.QueueWeightsForSharedPool())},
		},
		Timestamp: time.Now().Unix(),
	}

	if h.taskInspector != nil {
		stats, supported, err := h.taskInspector.QueueStats(ctx)
		if err != nil {
			logger.Errorf(ctx, "get queue stats failed: %v", err)
		}
		resp.Available = supported
		if stats != nil {
			resp.Queues = stats
		}
		serverStats, _, serverErr := h.taskInspector.WorkerServerStats(ctx)
		if serverErr != nil {
			logger.Errorf(ctx, "get worker server stats failed: %v", serverErr)
		} else {
			aggregateRuntimeWorkerPools(resp.Pools, serverStats)
		}
	}
	// Always emit a non-nil array so the JSON serialises to `[]` rather
	// than `null`, keeping the frontend iteration simple.
	if resp.Queues == nil {
		resp.Queues = []types.QueueStat{}
	}
	modelStats, modelSupported, modelErr := modellimiter.RuntimeStats(ctx)
	if modelErr != nil {
		logger.Errorf(ctx, "get model concurrency stats failed: %v", modelErr)
	}
	resp.ModelLimiterAvailable = modelSupported
	resp.Models = modelStats

	c.JSON(http.StatusOK, resp)
}

func isKnownRuntimeQueue(name string) bool {
	for _, definition := range types.QueueDefinitions() {
		if definition.Name == name {
			return true
		}
	}
	return false
}

func runtimeTaskPageSize(c *gin.Context) int {
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return pageSize
}

func (h *SystemHandler) listRuntimeTasks(c *gin.Context, state types.RuntimeTaskState) {
	queue := c.Param("queue")
	if !isKnownRuntimeQueue(queue) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown task queue"})
		return
	}
	if !state.Valid() {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown task state"})
		return
	}
	pageSize := runtimeTaskPageSize(c)
	cursor := c.Query("cursor")
	inspector, ok := h.taskInspector.(interfaces.RuntimeTaskInspector)
	if !ok {
		c.JSON(http.StatusOK, RuntimeTasksResponse{
			Available: false,
			Tasks:     []types.RuntimeTaskInfo{},
			PageSize:  pageSize,
		})
		return
	}
	page, supported, err := inspector.ListRuntimeTasks(c.Request.Context(), queue, state, cursor, pageSize)
	if err != nil {
		if errors.Is(err, types.ErrInvalidRuntimeTaskCursor) {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid task list cursor",
				"code":  "runtime_task_cursor_invalid",
			})
			return
		}
		if errors.Is(err, types.ErrExpiredRuntimeTaskCursor) {
			c.JSON(http.StatusConflict, gin.H{
				"error": "Task list changed; refresh from the beginning",
				"code":  "runtime_task_cursor_expired",
			})
			return
		}
		logger.Errorf(c.Request.Context(), "list runtime queue tasks queue=%s state=%s: %v", queue, state, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list queue tasks"})
		return
	}
	if page.Tasks == nil {
		page.Tasks = []types.RuntimeTaskInfo{}
	}
	c.JSON(http.StatusOK, RuntimeTasksResponse{
		Available:  supported,
		Tasks:      page.Tasks,
		PageSize:   pageSize,
		HasMore:    page.HasMore,
		NextCursor: page.NextCursor,
	})
}

// ListRuntimeTasks returns one live task-state page for the unified operator
// drawer. Raw task payloads are never included.
func (h *SystemHandler) ListRuntimeTasks(c *gin.Context) {
	h.listRuntimeTasks(c, types.RuntimeTaskState(c.Query("state")))
}

func runtimeTaskParams(c *gin.Context) (string, string, bool) {
	queue := c.Param("queue")
	taskID := c.Param("task_id")
	if !isKnownRuntimeQueue(queue) || taskID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid queue or task ID"})
		return "", "", false
	}
	return queue, taskID, true
}

func (h *SystemHandler) mutateRuntimeTask(c *gin.Context, action types.RuntimeTaskAction) {
	queue, taskID, ok := runtimeTaskParams(c)
	if !ok {
		return
	}
	inspector, supported := h.taskInspector.(interfaces.RuntimeTaskInspector)
	if !supported {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
		return
	}
	task, available, err := inspector.GetRuntimeTask(c.Request.Context(), queue, taskID)
	if !available {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
		return
	}
	if err != nil || task == nil {
		c.JSON(http.StatusConflict, gin.H{"error": "Task is no longer available"})
		return
	}
	if task.TenantID != 0 && task.TenantID != types.SingleTenantID {
		c.JSON(http.StatusForbidden, gin.H{"error": "Task does not belong to the personal space"})
		return
	}
	if !task.Allows(action) {
		c.JSON(http.StatusConflict, gin.H{"error": "Action is not allowed for the current task state"})
		return
	}

	switch action {
	case types.RuntimeTaskActionCancel:
		if h.knowledgeSvc == nil || task.KnowledgeID == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Business cancellation is unavailable"})
			return
		}
		cancelCtx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.SingleTenantID)
		if _, err = h.knowledgeSvc.CancelKnowledgeParse(cancelCtx, task.KnowledgeID); err != nil {
			if isKnowledgeGone(err) {
				if h.purgeOrphanRuntimeTask(c.Request.Context(), inspector, queue, taskID, task.KnowledgeID) {
					logger.Warnf(c.Request.Context(),
						"cancel runtime queue task queue=%s task=%s: knowledge gone, purged queue record",
						queue, taskID)
					break
				}
			}
			logger.Errorf(c.Request.Context(), "cancel runtime queue task queue=%s task=%s: %v", queue, taskID, err)
			c.JSON(http.StatusConflict, gin.H{"error": "Task can no longer be cancelled"})
			return
		}
	case types.RuntimeTaskActionRunNow:
		available, err = inspector.RunRuntimeTask(c.Request.Context(), queue, taskID)
		if !available {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
			return
		}
		if err != nil {
			logger.Errorf(c.Request.Context(), "run queue task now queue=%s task=%s: %v", queue, taskID, err)
			c.JSON(http.StatusConflict, gin.H{"error": "Task is no longer available to run"})
			return
		}
	case types.RuntimeTaskActionDelete:
		available, err = inspector.DeleteRuntimeTask(c.Request.Context(), queue, taskID)
		if !available {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
			return
		}
		if err != nil {
			logger.Errorf(c.Request.Context(), "delete queue task queue=%s task=%s: %v", queue, taskID, err)
			c.JSON(http.StatusConflict, gin.H{"error": "Task is no longer available for deletion"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unknown task action"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func isKnowledgeGone(err error) bool {
	if errors.Is(err, repository.ErrKnowledgeNotFound) {
		return true
	}
	var appErr *apperrors.AppError
	return errors.As(err, &appErr) && appErr.Code == apperrors.ErrNotFound
}

// purgeOrphanRuntimeTask removes queue records for a task whose knowledge row
// is already gone. CancelTasksForKnowledge sweeps sibling tasks best-effort;
// success is reported only once the requested task ID is gone.
func (h *SystemHandler) purgeOrphanRuntimeTask(
	ctx context.Context,
	inspector interfaces.RuntimeTaskInspector,
	queue, taskID, knowledgeID string,
) bool {
	if h.taskInspector != nil && knowledgeID != "" {
		if _, _, err := h.taskInspector.CancelTasksForKnowledge(ctx, knowledgeID); err != nil {
			logger.Warnf(ctx, "purge orphan tasks for knowledge %s: %v", knowledgeID, err)
		}
	}
	supported, err := inspector.ForceDeleteRuntimeTask(ctx, queue, taskID)
	if !supported {
		return false
	}
	if err == nil {
		return true
	}
	// The knowledge-wide sweep may have already removed this task ID.
	if _, available, getErr := inspector.GetRuntimeTask(ctx, queue, taskID); available && getErr != nil {
		return true
	}
	logger.Warnf(ctx, "force delete orphan task queue=%s task=%s: %v", queue, taskID, err)
	return false
}

// MutateRuntimeTask executes a backend-advertised action after re-reading the
// current task state to close click/race windows.
func (h *SystemHandler) MutateRuntimeTask(c *gin.Context) {
	h.mutateRuntimeTask(c, types.RuntimeTaskAction(c.Param("action")))
}

// PurgeArchivedRuntimeTasks clears the space's archived (finally-failed) tasks in one
// queue. It only touches the archived dead-letter set — live pending/active/
// scheduled/retry tasks are untouched — and mirrors single-record delete
// semantics by leaving business state as-is (archived tasks already had their
// document status flipped to "failed" on their last retry).
func (h *SystemHandler) PurgeArchivedRuntimeTasks(c *gin.Context) {
	queue := c.Param("queue")
	if !isKnownRuntimeQueue(queue) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid queue"})
		return
	}
	inspector, supported := h.taskInspector.(interfaces.RuntimeTaskInspector)
	if !supported {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
		return
	}
	deleted, available, err := inspector.PurgeArchivedRuntimeTasks(c.Request.Context(), queue)
	if !available {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Task queue is unavailable"})
		return
	}
	if err != nil {
		logger.Errorf(c.Request.Context(), "purge archived queue tasks queue=%s: %v", queue, err)
		c.JSON(http.StatusConflict, gin.H{"error": "Archived tasks could not be purged"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "deleted": deleted})
}

// ListSystemSettings godoc
func (h *SystemHandler) ListSystemSettings(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	rows, err := h.systemSettingSvc.List(ctx)
	if err != nil {
		logger.Errorf(ctx, "list system settings failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to list system settings"})
		return
	}
	if rows == nil {
		// Always emit a non-nil array so the JSON serialises to `[]`
		// rather than `null` on an empty table — front-end iteration
		// is safer.
		rows = []*types.SystemSetting{}
	}
	h.enrichSettingsModifiedBy(ctx, rows)
	c.JSON(http.StatusOK, rows)
}

// enrichSettingsModifiedBy resolves LastModifiedBy (UUID) → display name
// in a single batch lookup. Failures degrade silently: the UI already
// falls back to the UUID prefix when the name is empty, so a transient
// userSvc error must not break the settings page entirely.
func (h *SystemHandler) enrichSettingsModifiedBy(ctx context.Context, rows []*types.SystemSetting) {
	if len(rows) == 0 || h.userSvc == nil {
		return
	}
	idSet := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r != nil && strings.TrimSpace(r.LastModifiedBy) != "" {
			idSet[r.LastModifiedBy] = struct{}{}
		}
	}
	if len(idSet) == 0 {
		return
	}
	ids := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
	}
	users, err := h.userSvc.GetUsersByIDs(ctx, ids)
	if err != nil {
		logger.Warnf(ctx, "enrichSettingsModifiedBy: GetUsersByIDs failed: %v", err)
		return
	}
	for _, r := range rows {
		if r == nil {
			continue
		}
		u, ok := users[r.LastModifiedBy]
		if !ok || u == nil {
			continue
		}
		// Username is the canonical display label; fall back to email
		// for older rows where username may be empty. If both are empty,
		// leave LastModifiedByName empty so the
		// UI's UUID-prefix fallback kicks in.
		switch {
		case strings.TrimSpace(u.Username) != "":
			r.LastModifiedByName = u.Username
		case strings.TrimSpace(u.Email) != "":
			r.LastModifiedByName = u.Email
		}
	}
}

// GetSystemSetting godoc
func (h *SystemHandler) GetSystemSetting(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	key := c.Param("key")
	row, err := h.systemSettingSvc.Get(ctx, key)
	if err != nil {
		// Service-layer "unknown key" surfaces as a generic error here;
		// distinguish via the error string rather than typed errors so
		// we don't grow a sentinel package for a single error class.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if row == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "setting not yet persisted"})
		return
	}
	c.JSON(http.StatusOK, row)
}

// UpdateSystemSetting godoc
func (h *SystemHandler) UpdateSystemSetting(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	key := c.Param("key")

	var req UpdateSystemSettingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request: " + err.Error()})
		return
	}
	if req.Value == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "value is required"})
		return
	}

	row, err := h.systemSettingSvc.Update(ctx, key, req.Value)
	if err != nil {
		// Whether this is "unknown key" / "type mismatch" / "DB error"
		// is encoded in the error message at the service layer; surface
		// it verbatim. UI captures it as the toast text.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	h.enrichSettingsModifiedBy(ctx, []*types.SystemSetting{row})
	c.JSON(http.StatusOK, row)
}

// ResetSystemSetting godoc
func (h *SystemHandler) ResetSystemSetting(c *gin.Context) {
	ctx := logger.CloneContext(c.Request.Context())
	key := c.Param("key")
	if err := h.systemSettingSvc.Reset(ctx, key); err != nil {
		// Service surfaces "unknown key" as a plain error string; treat
		// every error as 400 so the UI captures it as a toast. A real
		// DB failure here is rare and indistinguishable from "bad key"
		// at this layer — operators see the message either way.
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

type RuntimeTasksResponse struct {
	Available  bool                    `json:"available"`
	Tasks      []types.RuntimeTaskInfo `json:"tasks"`
	PageSize   int                     `json:"page_size"`
	HasMore    bool                    `json:"has_more"`
	NextCursor string                  `json:"next_cursor,omitempty"`
}

type RuntimeQueuesResponse struct {
	Available             bool                       `json:"available"`
	UpstreamConcurrency   int                        `json:"upstream_concurrency"`
	ParseConcurrency      int                        `json:"parse_concurrency"` // compatibility alias for upstream_concurrency
	Pools                 []RuntimeWorkerPool        `json:"pools"`
	Queues                []types.QueueStat          `json:"queues"`
	ModelLimiterAvailable bool                       `json:"model_limiter_available"`
	Models                []modellimiter.RuntimeStat `json:"models"`
	Timestamp             int64                      `json:"timestamp"`
}

type RuntimeWorkerPool struct {
	Name            string  `json:"name"`
	Concurrency     int     `json:"concurrency"` // configured per instance
	QueueCount      int     `json:"queue_count"`
	Instances       int     `json:"instances"`        // live asynq server heartbeats
	ClusterCapacity int     `json:"cluster_capacity"` // sum of live server concurrency
	Active          int     `json:"active"`           // live workers assigned to this pool
	Utilization     float64 `json:"utilization"`      // Active / ClusterCapacity
}

// UpdateSystemSettingRequest is the body for PUT /system/settings/:key.
// `value` carries the new value as raw JSON — int / string / bool depending
// on the registry-declared value_type. The service validates the type
// strictly and rejects mismatches with 400.
type UpdateSystemSettingRequest struct {
	// Value is intentionally `any` (decoded as float64 / string / bool /
	// etc. by the JSON unmarshaller). Service.encodeForType normalises
	// these against the registry's declared type and rejects mismatches.
	Value any `json:"value"`
}
