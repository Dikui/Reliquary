package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/repository"
	"reliquary/internal/types"
)

type runtimeTestSettings struct{}

func (runtimeTestSettings) GetInt(_ context.Context, key, _ string, def int64) int64 {
	switch key {
	case "queue.core_concurrency":
		return 8
	case "queue.enrichment_concurrency":
		return 12
	case "queue.maintenance_concurrency":
		return 4
	case "queue.shared_concurrency":
		return 6
	default:
		return def
	}
}
func (runtimeTestSettings) GetString(_ context.Context, _, _, def string) string  { return def }
func (runtimeTestSettings) GetBool(_ context.Context, _, _ string, def bool) bool { return def }
func (runtimeTestSettings) GetStringList(_ context.Context, _, _ string, def []string) []string {
	return def
}
func (runtimeTestSettings) List(context.Context) ([]*types.SystemSetting, error) { return nil, nil }
func (runtimeTestSettings) Get(context.Context, string) (*types.SystemSetting, error) {
	return nil, nil
}
func (runtimeTestSettings) Update(context.Context, string, any) (*types.SystemSetting, error) {
	return nil, nil
}
func (runtimeTestSettings) Reset(context.Context, string) error { return nil }

type runtimeInvalidSettings struct{ runtimeTestSettings }

func (runtimeInvalidSettings) GetInt(_ context.Context, _ string, _ string, _ int64) int64 {
	return 0
}

type runtimeTestInspector struct{}

func (runtimeTestInspector) CancelTasksForEntry(context.Context, string) (int, int, error) {
	return 0, 0, nil
}
func (runtimeTestInspector) HasQueuedTasksForEntry(context.Context, string) (bool, error) {
	return false, nil
}
func (runtimeTestInspector) QueueStats(context.Context) ([]types.QueueStat, bool, error) {
	return []types.QueueStat{}, true, nil
}
func (runtimeTestInspector) WorkerServerStats(context.Context) ([]types.WorkerServerStat, bool, error) {
	return []types.WorkerServerStat{
		{Concurrency: 8, Active: 4, Status: "active", Queues: types.QueueWeightsForPool(types.WorkerPoolCore)},
		{Concurrency: 12, Active: 6, Status: "active", Queues: types.QueueWeightsForPool(types.WorkerPoolEnrichment)},
		{Concurrency: 4, Active: 1, Status: "active", Queues: types.QueueWeightsForPool(types.WorkerPoolMaintenance)},
		{Concurrency: 6, Active: 3, Status: "active", Queues: types.QueueWeightsForSharedPool()},
		{Concurrency: 99, Active: 0, Status: "stopped", Queues: types.QueueWeightsForPool(types.WorkerPoolCore)},
	}, true, nil
}

type runtimeTaskTestInspector struct {
	runtimeTestInspector
	tasks               []types.RuntimeTaskInfo
	retriedTask         string
	deletedTask         string
	forceDeleted        string
	purgedQueue         string
	purgedCount         int
	purgeErr            error
	cancelEntry         string
	cancelDeleted       int
	mutatedQueue        string
	nextCursor          string
	hasMore             bool
	inputCursor         string
	inputPageSize       int
	listErr             error
	forceDeleteErr      error
	getRuntimeErr       error
	getRuntimeErrFrom   int
	getRuntimeTaskCalls int
}

func (r *runtimeTaskTestInspector) CancelTasksForEntry(
	_ context.Context, entryID string,
) (int, int, error) {
	r.cancelEntry = entryID
	if r.cancelDeleted > 0 {
		return r.cancelDeleted, 0, nil
	}
	return 0, 0, nil
}

type runtimeEntryCancelTest struct {
	spaceID uint64
	entryID string
	err     error
}

func (r *runtimeEntryCancelTest) CancelEntryParse(
	ctx context.Context, entryID string,
) (*types.Entry, error) {
	r.spaceID, _ = ctx.Value(types.SpaceIDContextKey).(uint64)
	r.entryID = entryID
	if r.err != nil {
		return nil, r.err
	}
	return &types.Entry{ID: entryID, SpaceID: r.spaceID}, nil
}

func (r *runtimeTaskTestInspector) ListRuntimeTasks(
	_ context.Context, _ string, state types.RuntimeTaskState, cursor string, pageSize int,
) (types.RuntimeTaskPage, bool, error) {
	r.inputCursor = cursor
	r.inputPageSize = pageSize
	if r.listErr != nil {
		return types.RuntimeTaskPage{}, true, r.listErr
	}
	for i := range r.tasks {
		if r.tasks[i].State == "" {
			r.tasks[i].State = state
		}
		if r.tasks[i].AllowedActions == nil && state == types.RuntimeTaskArchived {
			r.tasks[i].AllowedActions = []types.RuntimeTaskAction{
				types.RuntimeTaskActionRunNow,
				types.RuntimeTaskActionDelete,
			}
		}
	}
	return types.RuntimeTaskPage{
		Tasks: r.tasks, NextCursor: r.nextCursor, HasMore: r.hasMore,
	}, true, nil
}

func (r *runtimeTaskTestInspector) GetRuntimeTask(
	_ context.Context, queue, taskID string,
) (*types.RuntimeTaskInfo, bool, error) {
	r.getRuntimeTaskCalls++
	if r.getRuntimeErr != nil && r.getRuntimeTaskCalls >= r.getRuntimeErrFrom {
		return nil, true, r.getRuntimeErr
	}
	for i := range r.tasks {
		if r.tasks[i].ID == taskID {
			return &r.tasks[i], true, nil
		}
	}
	return &types.RuntimeTaskInfo{
		ID: taskID, Queue: queue, State: types.RuntimeTaskArchived,
		AllowedActions: []types.RuntimeTaskAction{
			types.RuntimeTaskActionRunNow,
			types.RuntimeTaskActionDelete,
		},
	}, true, nil
}

func (r *runtimeTaskTestInspector) RunRuntimeTask(
	_ context.Context, queue, taskID string,
) (bool, error) {
	r.mutatedQueue = queue
	r.retriedTask = taskID
	return true, nil
}

func (r *runtimeTaskTestInspector) DeleteRuntimeTask(
	_ context.Context, queue, taskID string,
) (bool, error) {
	r.mutatedQueue = queue
	r.deletedTask = taskID
	return true, nil
}

func (r *runtimeTaskTestInspector) ForceDeleteRuntimeTask(
	_ context.Context, queue, taskID string,
) (bool, error) {
	r.mutatedQueue = queue
	r.forceDeleted = taskID
	return true, r.forceDeleteErr
}

func (r *runtimeTaskTestInspector) PurgeArchivedRuntimeTasks(
	_ context.Context, queue string,
) (int, bool, error) {
	r.purgedQueue = queue
	if r.purgeErr != nil {
		return 0, true, r.purgeErr
	}
	return r.purgedCount, true, nil
}

func TestGetRuntimeQueuesReportsIsolatedPoolCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{
		systemSettingSvc: runtimeTestSettings{},
		taskInspector:    runtimeTestInspector{},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/queues", nil)

	handler.GetRuntimeQueues(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}

	var response RuntimeQueuesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Available {
		t.Fatal("queue inspection should be available")
	}
	if response.UpstreamConcurrency != 30 || response.ParseConcurrency != 30 {
		t.Fatalf("upstream compatibility values are wrong: %+v", response)
	}
	want := map[string]struct {
		concurrency int
		queueCount  int
	}{
		types.WorkerPoolCore:        {8, 1},
		types.WorkerPoolEnrichment:  {12, 1},
		types.WorkerPoolMaintenance: {4, 1},
		types.WorkerPoolShared:      {6, 2},
	}
	if len(response.Pools) != len(want) {
		t.Fatalf("pool count = %d, want %d", len(response.Pools), len(want))
	}
	for _, pool := range response.Pools {
		expected, ok := want[pool.Name]
		if !ok {
			t.Fatalf("unexpected pool %q", pool.Name)
		}
		if pool.Concurrency != expected.concurrency || pool.QueueCount != expected.queueCount {
			t.Fatalf("pool %q = %+v, want concurrency=%d queue_count=%d",
				pool.Name, pool, expected.concurrency, expected.queueCount)
		}
		if pool.Instances != 1 || pool.ClusterCapacity != expected.concurrency {
			t.Fatalf("pool %q live capacity = %+v", pool.Name, pool)
		}
	}
}

func TestGetRuntimeQueuesFallsBackFromInvalidHistoricalConcurrency(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{
		systemSettingSvc: runtimeInvalidSettings{},
		taskInspector:    runtimeTestInspector{},
	}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/queues", nil)

	handler.GetRuntimeQueues(ctx)

	var response RuntimeQueuesResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.UpstreamConcurrency != types.DefaultUpstreamWorkerConcurrency {
		t.Fatalf("invalid stored values should use worker defaults: %+v", response)
	}
	for _, pool := range response.Pools {
		if pool.Concurrency < 1 {
			t.Fatalf("pool %q reported non-positive concurrency: %+v", pool.Name, pool)
		}
	}
}

func TestListRuntimeTasksReturnsSafeTaskDetails(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{tasks: []types.RuntimeTaskInfo{{
		ID:        "task-1",
		Queue:     types.QueueDefault,
		Type:      types.TypeDocumentProcess,
		LastError: "model unavailable",
		Retried:   5,
		MaxRetry:  5,
		LibraryID: "library-1",
		EntryID:   "entry-1",
	}}}
	handler := &SystemHandler{taskInspector: inspector}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: types.QueueDefault}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/queues/default/tasks?state=archived", nil)

	handler.ListRuntimeTasks(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response RuntimeTasksResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !response.Available || len(response.Tasks) != 1 {
		t.Fatalf("unexpected response: %+v", response)
	}
	if response.Tasks[0].EntryID != "entry-1" || response.Tasks[0].LastError != "model unavailable" {
		t.Fatalf("task details missing: %+v", response.Tasks[0])
	}
}

func TestListRuntimeTasksReturnsAndForwardsCursor(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{
		tasks:      []types.RuntimeTaskInfo{{ID: "task-1"}},
		nextCursor: "next-page", hasMore: true,
	}
	handler := &SystemHandler{taskInspector: inspector}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: types.QueueDefault}}
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/system/runtime/queues/default/tasks?state=archived&cursor=previous-page&page_size=25",
		nil,
	)

	handler.ListRuntimeTasks(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response RuntimeTasksResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if inspector.inputCursor != "previous-page" || inspector.inputPageSize != 25 {
		t.Fatalf("cursor request not forwarded: cursor=%q size=%d", inspector.inputCursor, inspector.inputPageSize)
	}
	if !response.HasMore || response.NextCursor != "next-page" {
		t.Fatalf("cursor response missing: %+v", response)
	}
}

func TestListRuntimeTasksReportsExpiredCursorForFrontendRefresh(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{taskInspector: &runtimeTaskTestInspector{
		listErr: types.ErrExpiredRuntimeTaskCursor,
	}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: types.QueueDefault}}
	ctx.Request = httptest.NewRequest(
		http.MethodGet,
		"/api/v1/system/runtime/queues/default/tasks?state=pending&cursor=expired",
		nil,
	)

	handler.ListRuntimeTasks(ctx)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["code"] != "runtime_task_cursor_expired" {
		t.Fatalf("unexpected error response: %+v", response)
	}
}

func TestRuntimeTaskMutationsDelegateToInspector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{}
	handler := &SystemHandler{taskInspector: inspector}

	retryRecorder := httptest.NewRecorder()
	retryCtx, _ := gin.CreateTestContext(retryRecorder)
	retryCtx.Params = gin.Params{
		{Key: "queue", Value: types.QueueDefault},
		{Key: "task_id", Value: "task-1"},
		{Key: "action", Value: string(types.RuntimeTaskActionRunNow)},
	}
	retryCtx.Request = httptest.NewRequest(http.MethodPost, "/retry", nil)
	handler.MutateRuntimeTask(retryCtx)
	if retryRecorder.Code != http.StatusOK || inspector.retriedTask != "task-1" {
		t.Fatalf("retry failed: status=%d inspector=%+v", retryRecorder.Code, inspector)
	}

	deleteRecorder := httptest.NewRecorder()
	deleteCtx, _ := gin.CreateTestContext(deleteRecorder)
	deleteCtx.Params = gin.Params{
		{Key: "queue", Value: types.QueueDefault},
		{Key: "task_id", Value: "task-2"},
		{Key: "action", Value: string(types.RuntimeTaskActionDelete)},
	}
	deleteCtx.Request = httptest.NewRequest(http.MethodDelete, "/task-2", nil)
	handler.MutateRuntimeTask(deleteCtx)
	if deleteRecorder.Code != http.StatusOK || inspector.deletedTask != "task-2" {
		t.Fatalf("delete failed: status=%d inspector=%+v", deleteRecorder.Code, inspector)
	}
}

func TestPurgeArchivedRuntimeTasksDelegatesToInspector(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{purgedCount: 7}
	handler := &SystemHandler{taskInspector: inspector}

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: types.QueueDefault}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/archived", nil)

	handler.PurgeArchivedRuntimeTasks(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if inspector.purgedQueue != types.QueueDefault {
		t.Fatalf("purged queue = %q, want %q", inspector.purgedQueue, types.QueueDefault)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["deleted"] != float64(7) {
		t.Fatalf("deleted = %v, want 7", response["deleted"])
	}
}

func TestPurgeArchivedRuntimeTasksRejectsUnknownQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{taskInspector: &runtimeTaskTestInspector{}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: "unknown"}}
	ctx.Request = httptest.NewRequest(http.MethodDelete, "/archived", nil)

	handler.PurgeArchivedRuntimeTasks(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestListRuntimeTasksRejectsUnknownQueue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{taskInspector: &runtimeTaskTestInspector{}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: "unknown"}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/tasks?state=archived", nil)

	handler.ListRuntimeTasks(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestListRuntimeTasksRejectsUnknownState(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := &SystemHandler{taskInspector: &runtimeTaskTestInspector{}}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "queue", Value: types.QueueDefault}}
	ctx.Request = httptest.NewRequest(http.MethodGet, "/tasks?state=unknown", nil)

	handler.ListRuntimeTasks(ctx)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}

func TestRuntimeTaskCancelUsesPersonalSpaceContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, spaceID := range []uint64{0, types.SingleSpaceID} {
		t.Run(strconv.FormatUint(spaceID, 10), func(t *testing.T) {
			inspector := &runtimeTaskTestInspector{tasks: []types.RuntimeTaskInfo{{
				ID: "task-cancel", Queue: types.QueueDefault, Type: types.TypeDocumentProcess,
				State: types.RuntimeTaskActive, SpaceID: spaceID, EntryID: "entry-1",
				AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionCancel},
			}}}
			canceller := &runtimeEntryCancelTest{}
			handler := &SystemHandler{taskInspector: inspector, entrySvc: canceller}
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Params = gin.Params{
				{Key: "queue", Value: types.QueueDefault},
				{Key: "task_id", Value: "task-cancel"},
				{Key: "action", Value: string(types.RuntimeTaskActionCancel)},
			}
			ctx.Request = httptest.NewRequest(http.MethodPost, "/cancel", nil)
			ctx.Request = ctx.Request.WithContext(context.WithValue(ctx.Request.Context(), types.SpaceIDContextKey, uint64(999)))

			handler.MutateRuntimeTask(ctx)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
			}
			if canceller.spaceID != types.SingleSpaceID || canceller.entryID != "entry-1" {
				t.Fatalf("domain cancellation context mismatch: %+v", canceller)
			}
		})
	}
}

func TestRuntimeTaskActionsRejectForeignSpaceBeforeMutation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, spaceID := range []uint64{2, 42} {
		for _, action := range []types.RuntimeTaskAction{
			types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete,
		} {
			t.Run(strconv.FormatUint(spaceID, 10)+"/"+string(action), func(t *testing.T) {
				inspector := &runtimeTaskTestInspector{tasks: []types.RuntimeTaskInfo{{
					ID: "task-foreign", Queue: types.QueueDefault, Type: types.TypeDocumentProcess,
					State: types.RuntimeTaskArchived, SpaceID: spaceID, EntryID: "foreign-entry",
					AllowedActions: []types.RuntimeTaskAction{
						types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete,
					},
				}}}
				canceller := &runtimeEntryCancelTest{}
				handler := &SystemHandler{taskInspector: inspector, entrySvc: canceller}
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Params = gin.Params{
					{Key: "queue", Value: types.QueueDefault},
					{Key: "task_id", Value: "task-foreign"},
					{Key: "action", Value: string(action)},
				}
				ctx.Request = httptest.NewRequest(http.MethodPost, "/actions/"+string(action), nil)

				handler.MutateRuntimeTask(ctx)
				if recorder.Code != http.StatusForbidden {
					t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
				}
				if inspector.retriedTask != "" || inspector.deletedTask != "" || inspector.forceDeleted != "" ||
					inspector.cancelEntry != "" || canceller.entryID != "" {
					t.Fatalf("foreign task caused mutation: inspector=%+v cancellation=%+v", inspector, canceller)
				}
			})
		}
	}
}

func TestRuntimeTaskCancelPurgesOrphanWhenEntryGone(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{tasks: []types.RuntimeTaskInfo{{
		ID: "task-orphan", Queue: types.QueueEntity, Type: types.TypeEntityNameEmbeddingRebuild,
		State: types.RuntimeTaskRetry, SpaceID: types.SingleSpaceID, EntryID: "entry-gone",
		AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionCancel},
	}}}
	canceller := &runtimeEntryCancelTest{err: repository.ErrEntryNotFound}
	handler := &SystemHandler{taskInspector: inspector, entrySvc: canceller}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "queue", Value: types.QueueEntity},
		{Key: "task_id", Value: "task-orphan"},
		{Key: "action", Value: string(types.RuntimeTaskActionCancel)},
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/cancel", nil)

	handler.MutateRuntimeTask(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if inspector.forceDeleted != "task-orphan" {
		t.Fatalf("expected force delete, got deleted=%q force=%q cancel=%q",
			inspector.deletedTask, inspector.forceDeleted, inspector.cancelEntry)
	}
}

func TestRuntimeTaskCancelPurgesOrphanAfterSiblingSweep(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{
		cancelDeleted: 2,
		tasks: []types.RuntimeTaskInfo{{
			ID: "task-orphan", Queue: types.QueueEntity, Type: types.TypeEntityNameEmbeddingRebuild,
			State: types.RuntimeTaskArchived, SpaceID: types.SingleSpaceID, EntryID: "entry-gone",
			AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionCancel},
		}},
	}
	canceller := &runtimeEntryCancelTest{err: repository.ErrEntryNotFound}
	handler := &SystemHandler{taskInspector: inspector, entrySvc: canceller}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "queue", Value: types.QueueEntity},
		{Key: "task_id", Value: "task-orphan"},
		{Key: "action", Value: string(types.RuntimeTaskActionCancel)},
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/cancel", nil)

	handler.MutateRuntimeTask(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if inspector.cancelEntry != "entry-gone" || inspector.forceDeleted != "task-orphan" {
		t.Fatalf("expected sibling sweep then force delete, got cancel=%q force=%q",
			inspector.cancelEntry, inspector.forceDeleted)
	}
}

func TestRuntimeTaskCancelPurgesOrphanWhenSweepAlreadyRemovedTask(t *testing.T) {
	gin.SetMode(gin.TestMode)
	inspector := &runtimeTaskTestInspector{
		cancelDeleted:     1,
		forceDeleteErr:    errors.New("task not found"),
		getRuntimeErr:     errors.New("task not found"),
		getRuntimeErrFrom: 2,
		tasks: []types.RuntimeTaskInfo{{
			ID: "task-orphan", Queue: types.QueueEntity, Type: types.TypeEntityNameEmbeddingRebuild,
			State: types.RuntimeTaskRetry, SpaceID: types.SingleSpaceID, EntryID: "entry-gone",
			AllowedActions: []types.RuntimeTaskAction{types.RuntimeTaskActionCancel},
		}},
	}
	canceller := &runtimeEntryCancelTest{err: repository.ErrEntryNotFound}
	handler := &SystemHandler{taskInspector: inspector, entrySvc: canceller}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{
		{Key: "queue", Value: types.QueueEntity},
		{Key: "task_id", Value: "task-orphan"},
		{Key: "action", Value: string(types.RuntimeTaskActionCancel)},
	}
	ctx.Request = httptest.NewRequest(http.MethodPost, "/cancel", nil)

	handler.MutateRuntimeTask(ctx)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	if inspector.forceDeleted != "task-orphan" {
		t.Fatalf("expected force delete attempt, got %q", inspector.forceDeleted)
	}
}
