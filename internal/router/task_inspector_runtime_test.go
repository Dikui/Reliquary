package router

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
	"reliquary/internal/logger"
	task "reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func newPostgresInspectorTest(t *testing.T) *postgresTaskInspector {
	t.Helper()
	client := task.NewClient(pgtest.Open(t))
	t.Cleanup(func() { _ = client.Close() })
	return NewPostgresTaskInspector(client).(*postgresTaskInspector)
}

func insertRuntimeJob(t *testing.T, inspector *postgresTaskInspector, id, payload, queue string, state types.RuntimeTaskState, orderTime time.Time) {
	t.Helper()
	_, err := inspector.client.Enqueue(task.NewTask(types.TypeDocumentProcess, []byte(payload)), task.Queue(queue), task.TaskID(id), task.MaxRetry(3))
	if err != nil {
		t.Fatalf("enqueue %s: %v", id, err)
	}
	updates := map[string]any{"state": string(state), "created_at": orderTime, "process_at": orderTime, "last_failed_at": orderTime, "completed_at": orderTime, "started_at": orderTime}
	if err := inspector.db.Model(&task.Job{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		t.Fatal(err)
	}
}

func TestQueueStatsDoesNotWarnForQueuesThatDoNotExist(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	var logs bytes.Buffer
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(os.Stdout) })
	stats, supported, err := inspector.QueueStats(context.Background())
	if err != nil || !supported {
		t.Fatalf("QueueStats: supported=%v err=%v", supported, err)
	}
	if len(stats) != len(types.QueueDefinitions()) {
		t.Fatalf("QueueStats returned %d rows", len(stats))
	}
	for _, stat := range stats {
		if stat.Size != 0 || stat.Pending != 0 || stat.Active != 0 {
			t.Fatalf("missing queue %q should have zero depth: %+v", stat.Name, stat)
		}
	}
	if strings.Contains(logs.String(), "[TaskInspector] queue info") {
		t.Fatalf("missing queues should not emit warnings:\n%s", logs.String())
	}
}

func TestProjectRuntimeTaskRedactsPayloadAndBuildsSafeActions(t *testing.T) {
	payload, err := json.Marshal(map[string]any{
		"space_id":   types.SingleSpaceID,
		"library_id": "library-1",
		"entry_id":   "entry-1",
		"file_url":   "secret://signed-document-url",
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Unix(1_700_000_000, 0)
	info, err := projectRuntimeTask(&task.Job{
		ID: "task-1", Queue: types.QueueDefault, TaskType: types.TypeDocumentProcess,
		Payload: payload, State: task.TaskStateActive, MaxRetry: 3, Retried: 1,
		StartedAt: &started, WorkerPool: "core",
	})
	if err != nil {
		t.Fatalf("project task: %v", err)
	}
	if info.State != types.RuntimeTaskActive || info.SpaceID != types.SingleSpaceID ||
		info.LibraryID != "library-1" || info.EntryID != "entry-1" {
		t.Fatalf("safe routing metadata missing: %+v", info)
	}
	if info.StartedAt == nil || !info.StartedAt.Equal(started) || info.Worker != "core" {
		t.Fatalf("worker metadata missing: %+v", info)
	}
	if len(info.AllowedActions) != 1 || info.AllowedActions[0] != types.RuntimeTaskActionCancel {
		t.Fatalf("active document actions = %v", info.AllowedActions)
	}
}

func TestProjectRuntimeTaskActionsFollowCurrentState(t *testing.T) {
	payload := []byte(`{"tenant_id":1,"entry_id":"entry-1"}`)
	cases := []struct {
		state task.TaskState
		want  []types.RuntimeTaskAction
	}{
		{task.TaskStateScheduled, []types.RuntimeTaskAction{types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow}},
		{task.TaskStateRetry, []types.RuntimeTaskAction{types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow}},
		{task.TaskStateArchived, []types.RuntimeTaskAction{types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete}},
		{task.TaskStateCompleted, []types.RuntimeTaskAction{}},
	}
	for _, tc := range cases {
		info, err := projectRuntimeTask(&task.Job{
			ID: "task", Queue: types.QueueDefault, TaskType: types.TypeDocumentProcess,
			Payload: payload, State: tc.state,
		})
		if err != nil {
			t.Fatalf("state %v: %v", tc.state, err)
		}
		if len(info.AllowedActions) != len(tc.want) {
			t.Fatalf("state %v actions = %v, want %v", tc.state, info.AllowedActions, tc.want)
		}
		for i := range tc.want {
			if info.AllowedActions[i] != tc.want[i] {
				t.Fatalf("state %v actions = %v, want %v", tc.state, info.AllowedActions, tc.want)
			}
		}
	}
}

func TestProjectRuntimeTaskActionsRespectPersonalSpaceBoundary(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		allowed bool
	}{
		{"personal_space", []byte(`{"tenant_id":1,"entry_id":"entry-1"}`), true},
		{"legacy_no_space", []byte(`{"entry_id":"entry-1"}`), true},
		{"legacy_zero_space", []byte(`{"tenant_id":0,"entry_id":"entry-1"}`), true},
		{"foreign_space", []byte(`{"tenant_id":2,"entry_id":"entry-2"}`), false},
		{"retired_space", []byte(`{"tenant_id":42,"entry_id":"entry-42"}`), false},
		{"malformed_json", []byte(`{"tenant_id":1`), false},
		{"invalid_space_type", []byte(`{"tenant_id":"1","entry_id":"entry-1"}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, state := range []task.TaskState{
				task.TaskStateActive, task.TaskStateScheduled, task.TaskStateRetry, task.TaskStateArchived,
			} {
				info, err := projectRuntimeTask(&task.Job{
					ID: "task", Queue: types.QueueDefault, TaskType: types.TypeDocumentProcess,
					Payload: tc.payload, State: state,
				})
				if err != nil {
					t.Fatalf("state %v: %v", state, err)
				}
				if info.AllowedActions == nil {
					t.Fatalf("state %v: actions must serialize as an empty array", state)
				}
				if (len(info.AllowedActions) > 0) != tc.allowed {
					t.Fatalf("state %v: actions=%v allowed=%v", state, info.AllowedActions, tc.allowed)
				}
				if tc.allowed && state == task.TaskStateActive && !info.Allows(types.RuntimeTaskActionCancel) {
					t.Fatalf("legacy or team task lost domain cancellation: %+v", info)
				}
			}
		})
	}
}

func TestProjectRuntimeTaskUsesAllowListedBatchMetadata(t *testing.T) {
	payload := []byte(`{
		"tenant_id":9,
		"task_id":"reparse-1",
		"library_id":"library",
		"entry_ids":["a","b"],
		"created_at":1700000000,
		"content":"must-not-be-projected"
	}`)
	info, err := projectRuntimeTask(&task.Job{
		ID: "task-reparse", Queue: types.QueueMaintenance, TaskType: types.TypeEntryListReparse,
		Payload: payload, State: task.TaskStatePending,
	})
	if err != nil {
		t.Fatal(err)
	}
	if info.TaskID != "reparse-1" || info.LibraryID != "library" || info.EntryCount != 2 || info.EnqueuedAt == nil {
		t.Fatalf("batch projection mismatch: %+v", info)
	}
	if len(info.AllowedActions) != 0 {
		t.Fatalf("generic maintenance task must not expose raw deletion: %v", info.AllowedActions)
	}
}

func TestQueueStatsReadsDurableStateAndDailyCounters(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	now := time.Now().Add(-time.Minute)
	for _, state := range []types.RuntimeTaskState{types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskScheduled, types.RuntimeTaskRetry, types.RuntimeTaskArchived, types.RuntimeTaskCompleted} {
		insertRuntimeJob(t, inspector, string(state), `{"tenant_id":1}`, types.QueueDefault, state, now)
	}
	if err := inspector.db.Exec(`INSERT INTO task_queue_counters(queue, day, processed, failed) VALUES (?, CURRENT_DATE, 9, 4)`, types.QueueDefault).Error; err != nil {
		t.Fatal(err)
	}
	stats, supported, err := inspector.QueueStats(context.Background())
	if err != nil || !supported {
		t.Fatalf("stats supported=%v err=%v", supported, err)
	}
	for _, stat := range stats {
		if stat.Name == types.QueueDefault {
			if stat.Size != 5 || stat.Pending != 1 || stat.Active != 1 || stat.Scheduled != 1 || stat.Retry != 1 || stat.Archived != 1 || stat.Completed != 1 || stat.Processed != 9 || stat.Failed != 4 || stat.LatencyMs < 59000 {
				t.Fatalf("stats mismatch: %+v", stat)
			}
		}
	}
}

func TestPurgeArchivedRuntimeTasksOnlyDeletesTeamAndLegacyTasks(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	ctx := context.Background()
	now := time.Now()
	retained := []string{}
	for i := 0; i < listPageSize+20; i++ {
		insertRuntimeJob(t, inspector, "team-"+strconv.Itoa(i), `{"tenant_id":1,"entry_id":"team-entry"}`, types.QueueDefault, types.RuntimeTaskArchived, now)
		if i%3 == 0 {
			id := "foreign-" + strconv.Itoa(i)
			insertRuntimeJob(t, inspector, id, `{"tenant_id":42,"entry_id":"foreign-entry"}`, types.QueueDefault, types.RuntimeTaskArchived, now)
			retained = append(retained, id)
		}
	}
	insertRuntimeJob(t, inspector, "legacy-zero", `{"tenant_id":0,"entry_id":"team-entry"}`, types.QueueDefault, types.RuntimeTaskArchived, now)
	insertRuntimeJob(t, inspector, "legacy-missing", `{"entry_id":"team-entry"}`, types.QueueDefault, types.RuntimeTaskArchived, now)
	// JSONB prevents invalid JSON from being stored. A JSON string preserves
	// the invalid metadata guard while exercising a valid storage value.
	insertRuntimeJob(t, inspector, "invalid-shape", `"invalid metadata"`, types.QueueDefault, types.RuntimeTaskArchived, now)
	insertRuntimeJob(t, inspector, "invalid-space-type", `{"tenant_id":"1"}`, types.QueueDefault, types.RuntimeTaskArchived, now)
	retained = append(retained, "invalid-shape", "invalid-space-type")
	insertRuntimeJob(t, inspector, "other-queue", `{"tenant_id":1}`, types.QueueEntity, types.RuntimeTaskArchived, now)
	liveStates := []types.RuntimeTaskState{types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskScheduled, types.RuntimeTaskRetry}
	for _, state := range liveStates {
		insertRuntimeJob(t, inspector, "live-"+string(state), `{"tenant_id":1}`, types.QueueDefault, state, now)
	}
	deleted, supported, err := inspector.PurgeArchivedRuntimeTasks(ctx, types.QueueDefault)
	if err != nil || !supported || deleted != listPageSize+22 {
		t.Fatalf("purge deleted=%d supported=%v err=%v", deleted, supported, err)
	}
	for _, id := range retained {
		info, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, id)
		if err != nil || info.State != types.RuntimeTaskArchived {
			t.Fatalf("retained task %s missing or changed: task=%+v err=%v", id, info, err)
		}
	}
	for _, state := range liveStates {
		info, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, "live-"+string(state))
		if err != nil || info.State != state {
			t.Fatalf("live state %s affected: task=%+v err=%v", state, info, err)
		}
	}
	if _, _, err := inspector.GetRuntimeTask(ctx, types.QueueEntity, "other-queue"); err != nil {
		t.Fatalf("purge affected another queue: %v", err)
	}
	if deleted, _, err := inspector.PurgeArchivedRuntimeTasks(ctx, types.QueueDefault); err != nil || deleted != 0 {
		t.Fatalf("repeat purge deleted=%d err=%v", deleted, err)
	}
}

func TestArchivedTaskDeletionRequiresUnchangedArchivedSnapshot(t *testing.T) {
	for _, change := range []string{"run_now", "message_changed"} {
		t.Run(change, func(t *testing.T) {
			inspector := newPostgresInspectorTest(t)
			ctx := context.Background()
			insertRuntimeJob(t, inspector, "task", `{"tenant_id":1}`, types.QueueDefault, types.RuntimeTaskArchived, time.Now())
			var job task.Job
			if err := inspector.db.First(&job, "id = ?", "task").Error; err != nil {
				t.Fatal(err)
			}
			if change == "run_now" {
				if _, err := inspector.RunRuntimeTask(ctx, types.QueueDefault, "task"); err != nil {
					t.Fatal(err)
				}
			} else if err := inspector.db.Model(&task.Job{}).Where("id = ?", "task").Update("payload", json.RawMessage(`{"tenant_id":1,"entry_id":"changed"}`)).Error; err != nil {
				t.Fatal(err)
			}
			deleted, err := inspector.deleteArchivedRuntimeTask(ctx, types.QueueDefault, "task", job.Payload)
			if err != nil || deleted != 0 {
				t.Fatalf("stale snapshot deleted=%d err=%v", deleted, err)
			}
			if _, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, "task"); err != nil {
				t.Fatalf("changed task removed: %v", err)
			}
		})
	}
}

func TestRuntimeTaskCursorUsesStateTimeOrderAndSurvivingAnchors(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	ctx := context.Background()
	start := time.Now()
	for i, id := range []string{"old", "middle", "new"} {
		insertRuntimeJob(t, inspector, id, `{}`, types.QueueDefault, types.RuntimeTaskPending, start.Add(time.Duration(i)*time.Second))
	}
	first, _, err := inspector.ListRuntimeTasks(ctx, types.QueueDefault, types.RuntimeTaskPending, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runtimeTaskIDs(first.Tasks), []string{"new", "middle"}; !equalStrings(got, want) {
		t.Fatalf("pending order = %v, want %v", got, want)
	}
	if err := inspector.client.Delete(ctx, "middle"); err != nil {
		t.Fatal(err)
	}
	second, _, err := inspector.ListRuntimeTasks(ctx, types.QueueDefault, types.RuntimeTaskPending, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	// The surviving previous anchor can overlap the previous page; the deleted
	// last anchor is absent, and the remaining unseen task is still reached.
	if got, want := runtimeTaskIDs(second.Tasks), []string{"old"}; !equalStrings(got, want) {
		t.Fatalf("anchor fallback order = %v, want %v", got, want)
	}
	for i, id := range []string{"next", "after-next", "later"} {
		insertRuntimeJob(t, inspector, id, `{}`, types.QueueDefault, types.RuntimeTaskScheduled, start.Add(time.Duration(i)*time.Second))
	}
	scheduled, _, err := inspector.ListRuntimeTasks(ctx, types.QueueDefault, types.RuntimeTaskScheduled, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runtimeTaskIDs(scheduled.Tasks), []string{"next", "after-next", "later"}; !equalStrings(got, want) {
		t.Fatalf("scheduled order = %v, want %v", got, want)
	}
	for i, id := range []string{"old-failure", "middle-failure", "new-failure"} {
		insertRuntimeJob(t, inspector, id, `{}`, types.QueueDefault, types.RuntimeTaskArchived, start.Add(time.Duration(i)*time.Second))
	}
	archived, _, err := inspector.ListRuntimeTasks(ctx, types.QueueDefault, types.RuntimeTaskArchived, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := runtimeTaskIDs(archived.Tasks), []string{"new-failure", "middle-failure", "old-failure"}; !equalStrings(got, want) {
		t.Fatalf("archived order = %v, want %v", got, want)
	}
}

func TestRuntimeTaskCursorIsBoundToQueueAndState(t *testing.T) {
	raw, err := encodeRuntimeTaskCursor(types.QueueDefault, types.RuntimeTaskArchived, []string{"task-1", "task-2"})
	if err != nil {
		t.Fatal(err)
	}
	anchors, err := decodeRuntimeTaskCursor(raw, types.QueueDefault, types.RuntimeTaskArchived)
	if err != nil {
		t.Fatal(err)
	}
	if !equalStrings(anchors, []string{"task-1", "task-2"}) {
		t.Fatalf("anchors = %v", anchors)
	}
	if _, err := decodeRuntimeTaskCursor(raw, types.QueueDefault, types.RuntimeTaskRetry); err == nil {
		t.Fatal("cursor from another state should be rejected")
	}
	if _, err := decodeRuntimeTaskCursor(raw, types.QueueEntity, types.RuntimeTaskArchived); err == nil {
		t.Fatal("cursor from another queue should be rejected")
	}
	if _, err := decodeRuntimeTaskCursor("not-base64", types.QueueDefault, types.RuntimeTaskArchived); err == nil {
		t.Fatal("malformed cursor should be rejected")
	}
}

func TestListRuntimeTasksPaginatesNewestPendingTasksWithoutOverlap(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	start := time.Now()
	for i, id := range []string{"old", "middle", "new"} {
		insertRuntimeJob(t, inspector, id, `{"tenant_id":1,"entry_id":"entry-1"}`, types.QueueDefault, types.RuntimeTaskPending, start.Add(time.Duration(i)*time.Second))
	}
	first, supported, err := inspector.ListRuntimeTasks(context.Background(), types.QueueDefault, types.RuntimeTaskPending, "", 2)
	if err != nil || !supported {
		t.Fatalf("first page supported=%v err=%v", supported, err)
	}
	if got, want := runtimeTaskIDs(first.Tasks), []string{"new", "middle"}; !equalStrings(got, want) {
		t.Fatalf("first page = %v, want %v", got, want)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page cursor missing: %+v", first)
	}
	second, supported, err := inspector.ListRuntimeTasks(context.Background(), types.QueueDefault, types.RuntimeTaskPending, first.NextCursor, 2)
	if err != nil || !supported {
		t.Fatalf("second page supported=%v err=%v", supported, err)
	}
	if got, want := runtimeTaskIDs(second.Tasks), []string{"old"}; !equalStrings(got, want) {
		t.Fatalf("second page = %v, want %v", got, want)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Fatalf("unexpected continuation after final page: %+v", second)
	}
	if err := inspector.db.Where("id IN ?", []string{"new", "middle"}).Delete(&task.Job{}).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := inspector.ListRuntimeTasks(context.Background(), types.QueueDefault, types.RuntimeTaskPending, first.NextCursor, 2); !errors.Is(err, types.ErrExpiredRuntimeTaskCursor) {
		t.Fatalf("removed anchors should expire cursor: %v", err)
	}
}

func TestRuntimeTaskMutationsPreserveRetryCountAndRespectQueue(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	ctx := context.Background()
	insertRuntimeJob(t, inspector, "task", `{"tenant_id":1}`, types.QueueDefault, types.RuntimeTaskArchived, time.Now())
	if err := inspector.db.Model(&task.Job{}).Where("id = ?", "task").Update("retried", 3).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := inspector.RunRuntimeTask(ctx, types.QueueEntity, "task"); err == nil {
		t.Fatal("wrong queue should reject mutation")
	}
	if _, err := inspector.RunRuntimeTask(ctx, types.QueueDefault, "task"); err != nil {
		t.Fatal(err)
	}
	info, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, "task")
	if err != nil || info.State != types.RuntimeTaskPending || info.Retried != 3 {
		t.Fatalf("run now reset retries or wrong state: info=%+v err=%v", info, err)
	}
	if _, err := inspector.DeleteRuntimeTask(ctx, types.QueueDefault, "task"); err == nil {
		t.Fatal("live task should reject operator deletion")
	}
}

func TestPostgresRuntimeQueueHTTPReadsAndMutatesRealRows(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	insertRuntimeJob(t, inspector, "runtime-http", `{"tenant_id":1,"entry_id":"article-1","file_url":"private-document"}`, types.QueueDefault, types.RuntimeTaskArchived, time.Now())
	if err := inspector.db.Model(&task.Job{}).Where("id = ?", "runtime-http").Updates(map[string]any{"retried": 3, "last_error": "model unavailable"}).Error; err != nil {
		t.Fatal(err)
	}
	h := handler.NewSystemHandler(nil, nil, nil, nil, nil, inspector, nil)
	r := gin.New()
	r.GET("/api/v1/system/runtime/queues", h.GetRuntimeQueues)
	r.GET("/api/v1/system/runtime/queues/:queue/tasks", h.ListRuntimeTasks)
	r.POST("/api/v1/system/runtime/queues/:queue/tasks/:task_id/actions/:action", h.MutateRuntimeTask)
	request := func(method, path string) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		r.ServeHTTP(recorder, httptest.NewRequest(method, path, nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("%s %s: status=%d body=%s", method, path, recorder.Code, recorder.Body.String())
		}
		return recorder
	}
	var overview handler.RuntimeQueuesResponse
	if err := json.Unmarshal(request(http.MethodGet, "/api/v1/system/runtime/queues").Body.Bytes(), &overview); err != nil {
		t.Fatal(err)
	}
	if !overview.Available || len(overview.Queues) != len(types.QueueDefinitions()) {
		t.Fatalf("incorrect overview: %+v", overview)
	}
	for _, stat := range overview.Queues {
		if stat.Name == types.QueueDefault && stat.Archived != 1 {
			t.Fatalf("incorrect archived count: %+v", stat)
		}
	}
	body := request(http.MethodGet, "/api/v1/system/runtime/queues/default/tasks?state=archived&page_size=20").Body.String()
	if strings.Contains(body, "private-document") || strings.Contains(body, "file_url") {
		t.Fatalf("runtime API exposed payload: %s", body)
	}
	var page handler.RuntimeTasksResponse
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatal(err)
	}
	if !page.Available || len(page.Tasks) != 1 || page.Tasks[0].LastError != "model unavailable" || !page.Tasks[0].Allows(types.RuntimeTaskActionRunNow) || !page.Tasks[0].Allows(types.RuntimeTaskActionDelete) {
		t.Fatalf("incorrect archived details: %+v", page)
	}
	request(http.MethodPost, "/api/v1/system/runtime/queues/default/tasks/runtime-http/actions/run_now")
	var pending handler.RuntimeTasksResponse
	if err := json.Unmarshal(request(http.MethodGet, "/api/v1/system/runtime/queues/default/tasks?state=pending").Body.Bytes(), &pending); err != nil {
		t.Fatal(err)
	}
	if len(pending.Tasks) != 1 || pending.Tasks[0].Retried != 3 || pending.Tasks[0].State != types.RuntimeTaskPending {
		t.Fatalf("run now did not preserve task: %+v", pending)
	}
}

func TestPostgresRuntimeTopicTaskProjectsBusinessJobID(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	payload, err := json.Marshal(types.TopicBackfillPayload{SpaceID: types.SingleSpaceID, JobID: "topic-history-job"})
	if err != nil {
		t.Fatal(err)
	}
	h := handler.NewSystemHandler(nil, nil, nil, nil, nil, inspector, nil)
	r := gin.New()
	r.GET("/api/v1/system/runtime/queues/:queue/tasks", h.ListRuntimeTasks)
	for _, test := range []struct {
		kind       string
		queue      string
		wantTaskID string
	}{
		{types.TypeTopicBackfill, types.QueueEntity, "topic-history-job"},
		{types.TypeDocumentProcess, types.QueueDefault, ""},
	} {
		t.Run(test.kind, func(t *testing.T) {
			job, err := inspector.client.Enqueue(task.NewTask(test.kind, payload), task.Queue(test.queue))
			if err != nil {
				t.Fatal(err)
			}
			info, available, err := inspector.GetRuntimeTask(context.Background(), test.queue, job.ID)
			if err != nil || !available || info.TaskID != test.wantTaskID {
				t.Fatalf("business job projection: info=%+v available=%v err=%v", info, available, err)
			}
			recorder := httptest.NewRecorder()
			r.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/v1/system/runtime/queues/"+test.queue+"/tasks?state=pending", nil))
			if recorder.Code != http.StatusOK {
				t.Fatalf("task list status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			var response handler.RuntimeTasksResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if !response.Available || len(response.Tasks) != 1 || response.Tasks[0].TaskID != test.wantTaskID {
				t.Fatalf("business job missing from HTTP response: %+v", response)
			}
			if strings.Contains(recorder.Body.String(), "job_id") {
				t.Fatalf("raw payload field should not be exposed: %s", recorder.Body.String())
			}
		})
	}
}

func runtimeTaskIDs(tasks []types.RuntimeTaskInfo) []string {
	ids := make([]string, 0, len(tasks))
	for _, item := range tasks {
		ids = append(ids, item.ID)
	}
	return ids
}
func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
