package router

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

func TestQueueStatsDoesNotWarnForQueuesThatDoNotExist(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	inspector := &asynqTaskInspector{
		inspector: asynq.NewInspectorFromRedisClient(client),
		redis:     client,
	}

	var logs bytes.Buffer
	logger.SetOutput(&logs)
	t.Cleanup(func() { logger.SetOutput(os.Stdout) })

	stats, supported, err := inspector.QueueStats(context.Background())
	if err != nil || !supported {
		t.Fatalf("QueueStats: supported=%v err=%v", supported, err)
	}
	if got, want := len(stats), len(types.QueueDefinitions()); got != want {
		t.Fatalf("QueueStats returned %d rows, want %d", got, want)
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
		"tenant_id":         types.SingleTenantID,
		"knowledge_base_id": "kb-1",
		"knowledge_id":      "knowledge-1",
		"file_url":          "secret://signed-document-url",
	})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Unix(1_700_000_000, 0)
	info, err := projectRuntimeTask(&asynq.TaskInfo{
		ID: "task-1", Queue: types.QueueDefault, Type: types.TypeDocumentProcess,
		Payload: payload, State: asynq.TaskStateActive, MaxRetry: 3, Retried: 1,
	}, runtimeWorkerMetadata{started: started, worker: "worker-a:123"})
	if err != nil {
		t.Fatalf("project task: %v", err)
	}
	if info.State != types.RuntimeTaskActive || info.TenantID != types.SingleTenantID ||
		info.KnowledgeBaseID != "kb-1" || info.KnowledgeID != "knowledge-1" {
		t.Fatalf("safe routing metadata missing: %+v", info)
	}
	if info.StartedAt == nil || !info.StartedAt.Equal(started) || info.Worker != "worker-a:123" {
		t.Fatalf("worker metadata missing: %+v", info)
	}
	if len(info.AllowedActions) != 1 || info.AllowedActions[0] != types.RuntimeTaskActionCancel {
		t.Fatalf("active document actions = %v", info.AllowedActions)
	}
}

func TestProjectRuntimeTaskActionsFollowCurrentState(t *testing.T) {
	payload := []byte(`{"tenant_id":1,"knowledge_id":"knowledge-1"}`)
	cases := []struct {
		state asynq.TaskState
		want  []types.RuntimeTaskAction
	}{
		{asynq.TaskStateScheduled, []types.RuntimeTaskAction{types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow}},
		{asynq.TaskStateRetry, []types.RuntimeTaskAction{types.RuntimeTaskActionCancel, types.RuntimeTaskActionRunNow}},
		{asynq.TaskStateArchived, []types.RuntimeTaskAction{types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete}},
		{asynq.TaskStateCompleted, []types.RuntimeTaskAction{}},
	}
	for _, tc := range cases {
		info, err := projectRuntimeTask(&asynq.TaskInfo{
			ID: "task", Queue: types.QueueDefault, Type: types.TypeDocumentProcess,
			Payload: payload, State: tc.state,
		}, runtimeWorkerMetadata{})
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

func TestProjectRuntimeTaskActionsRespectSingleTeamBoundary(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		allowed bool
	}{
		{"single_team", []byte(`{"tenant_id":1,"knowledge_id":"knowledge-1"}`), true},
		{"legacy_no_tenant", []byte(`{"knowledge_id":"knowledge-1"}`), true},
		{"legacy_zero_tenant", []byte(`{"tenant_id":0,"knowledge_id":"knowledge-1"}`), true},
		{"foreign_tenant", []byte(`{"tenant_id":2,"knowledge_id":"knowledge-2"}`), false},
		{"retired_tenant", []byte(`{"tenant_id":42,"knowledge_id":"knowledge-42"}`), false},
		{"malformed_json", []byte(`{"tenant_id":1`), false},
		{"invalid_tenant_type", []byte(`{"tenant_id":"1","knowledge_id":"knowledge-1"}`), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, state := range []asynq.TaskState{
				asynq.TaskStateActive, asynq.TaskStateScheduled, asynq.TaskStateRetry, asynq.TaskStateArchived,
			} {
				info, err := projectRuntimeTask(&asynq.TaskInfo{
					ID: "task", Queue: types.QueueDefault, Type: types.TypeDocumentProcess,
					Payload: tc.payload, State: state,
				}, runtimeWorkerMetadata{})
				if err != nil {
					t.Fatalf("state %v: %v", state, err)
				}
				if info.AllowedActions == nil {
					t.Fatalf("state %v: actions must serialize as an empty array", state)
				}
				if (len(info.AllowedActions) > 0) != tc.allowed {
					t.Fatalf("state %v: actions=%v allowed=%v", state, info.AllowedActions, tc.allowed)
				}
				if tc.allowed && state == asynq.TaskStateActive && !info.Allows(types.RuntimeTaskActionCancel) {
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
		"knowledge_base_id":"kb",
		"knowledge_ids":["a","b"],
		"created_at":1700000000,
		"content":"must-not-be-projected"
	}`)
	info, err := projectRuntimeTask(&asynq.TaskInfo{
		ID: "task-reparse", Queue: types.QueueMaintenance, Type: types.TypeKnowledgeListReparse,
		Payload: payload, State: asynq.TaskStatePending,
	}, runtimeWorkerMetadata{})
	if err != nil {
		t.Fatal(err)
	}
	if info.TaskID != "reparse-1" || info.KnowledgeBaseID != "kb" || info.KnowledgeCount != 2 || info.EnqueuedAt == nil {
		t.Fatalf("batch projection mismatch: %+v", info)
	}
	if len(info.AllowedActions) != 0 {
		t.Fatalf("generic maintenance task must not expose raw deletion: %v", info.AllowedActions)
	}
}

func TestPurgeArchivedRuntimeTasksOnlyDeletesTeamAndLegacyTasks(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	asynqClient := asynq.NewClientFromRedisClient(client)
	inspector := &asynqTaskInspector{
		inspector: asynq.NewInspectorFromRedisClient(client), redis: client,
	}
	ctx := context.Background()
	enqueue := func(id, payload, queue string, archived bool) {
		t.Helper()
		if _, err := asynqClient.Enqueue(asynq.NewTask(types.TypeDocumentProcess, []byte(payload)),
			asynq.Queue(queue), asynq.TaskID(id)); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
		if archived {
			if err := inspector.inspector.ArchiveTask(queue, id); err != nil {
				t.Fatalf("archive %s: %v", id, err)
			}
		}
	}
	retained := []string{}
	for i := 0; i < listPageSize+20; i++ {
		enqueue("team-"+strconv.Itoa(i), `{"tenant_id":1,"knowledge_id":"team-knowledge"}`, types.QueueDefault, true)
		if i%3 == 0 {
			id := "foreign-" + strconv.Itoa(i)
			enqueue(id, `{"tenant_id":42,"knowledge_id":"foreign-knowledge"}`, types.QueueDefault, true)
			retained = append(retained, id)
		}
	}
	enqueue("legacy-zero", `{"tenant_id":0,"knowledge_id":"team-knowledge"}`, types.QueueDefault, true)
	enqueue("legacy-missing", `{"knowledge_id":"team-knowledge"}`, types.QueueDefault, true)
	enqueue("invalid-json", `{"tenant_id":1`, types.QueueDefault, true)
	enqueue("invalid-tenant-type", `{"tenant_id":"1"}`, types.QueueDefault, true)
	retained = append(retained, "invalid-json", "invalid-tenant-type")
	enqueue("other-queue", `{"tenant_id":1}`, types.QueueEntity, true)
	liveStates := []types.RuntimeTaskState{
		types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskScheduled, types.RuntimeTaskRetry,
	}
	for _, state := range liveStates {
		id := "live-" + string(state)
		enqueue(id, `{"tenant_id":1}`, types.QueueDefault, false)
		if state != types.RuntimeTaskPending {
			pendingKey, _ := runtimeTaskStateKey(types.QueueDefault, types.RuntimeTaskPending)
			if err := client.LRem(ctx, pendingKey, 0, id).Err(); err != nil {
				t.Fatal(err)
			}
			stateKey, _ := runtimeTaskStateKey(types.QueueDefault, state)
			if state == types.RuntimeTaskActive {
				if err := client.LPush(ctx, stateKey, id).Err(); err != nil {
					t.Fatal(err)
				}
			} else if err := client.ZAdd(ctx, stateKey, redis.Z{Score: float64(time.Now().Unix()), Member: id}).Err(); err != nil {
				t.Fatal(err)
			}
			if err := client.HSet(ctx, "asynq:{"+types.QueueDefault+"}:t:"+id, "state", string(state)).Err(); err != nil {
				t.Fatal(err)
			}
		}
	}

	deleted, supported, err := inspector.PurgeArchivedRuntimeTasks(ctx, types.QueueDefault)
	if err != nil || !supported || deleted != listPageSize+22 {
		t.Fatalf("purge deleted=%d supported=%v err=%v", deleted, supported, err)
	}
	for _, id := range retained {
		task, err := inspector.inspector.GetTaskInfo(types.QueueDefault, id)
		if err != nil || task.State != asynq.TaskStateArchived {
			t.Fatalf("retained task %s missing or changed: task=%+v err=%v", id, task, err)
		}
	}
	for _, state := range liveStates {
		task, err := inspector.inspector.GetTaskInfo(types.QueueDefault, "live-"+string(state))
		if err != nil {
			t.Fatalf("live state %s affected: task=%+v err=%v", state, task, err)
		}
		actualState, err := runtimeTaskState(task.State)
		if err != nil || actualState != state {
			t.Fatalf("live state %s affected: task=%+v err=%v", state, task, err)
		}
	}
	if _, err := inspector.inspector.GetTaskInfo(types.QueueEntity, "other-queue"); err != nil {
		t.Fatalf("purge affected another queue: %v", err)
	}
	if deleted, _, err := inspector.PurgeArchivedRuntimeTasks(ctx, types.QueueDefault); err != nil || deleted != 0 {
		t.Fatalf("repeat purge deleted=%d err=%v", deleted, err)
	}
}

func TestArchivedTaskDeletionRequiresUnchangedArchivedSnapshot(t *testing.T) {
	for _, change := range []string{"run_now", "message_changed"} {
		t.Run(change, func(t *testing.T) {
			server := miniredis.RunT(t)
			client := redis.NewClient(&redis.Options{Addr: server.Addr()})
			t.Cleanup(func() { _ = client.Close() })
			asynqClient := asynq.NewClientFromRedisClient(client)
			inspector := &asynqTaskInspector{
				inspector: asynq.NewInspectorFromRedisClient(client), redis: client,
			}
			ctx := context.Background()
			if _, err := asynqClient.Enqueue(asynq.NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":1}`)),
				asynq.Queue(types.QueueDefault), asynq.TaskID("task")); err != nil {
				t.Fatal(err)
			}
			if err := inspector.inspector.ArchiveTask(types.QueueDefault, "task"); err != nil {
				t.Fatal(err)
			}
			taskKey := "asynq:{" + types.QueueDefault + "}:t:task"
			message, err := client.HGet(ctx, taskKey, "msg").Result()
			if err != nil {
				t.Fatal(err)
			}
			if change == "run_now" {
				if err := inspector.inspector.RunTask(types.QueueDefault, "task"); err != nil {
					t.Fatal(err)
				}
			} else if err := client.HSet(ctx, taskKey, "msg", message+"changed").Err(); err != nil {
				t.Fatal(err)
			}
			deleted, err := inspector.deleteArchivedRuntimeTask(ctx, types.QueueDefault, "task", message)
			if err != nil || deleted != 0 {
				t.Fatalf("stale snapshot deleted=%d err=%v", deleted, err)
			}
			if exists, err := client.Exists(ctx, taskKey).Result(); err != nil || exists != 1 {
				t.Fatalf("changed task removed: exists=%d err=%v", exists, err)
			}
		})
	}
}

func TestRuntimeTaskCursorUsesStateTimeOrderAndSurvivingAnchors(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	inspector := &asynqTaskInspector{redis: client}
	ctx := context.Background()

	pendingKey, _ := runtimeTaskStateKey(types.QueueDefault, types.RuntimeTaskPending)
	if err := client.LPush(ctx, pendingKey, "old", "middle", "new").Err(); err != nil {
		t.Fatal(err)
	}
	ids, err := inspector.listRuntimeTaskIDs(ctx, types.QueueDefault, types.RuntimeTaskPending, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids, []string{"new", "middle"}; !equalStrings(got, want) {
		t.Fatalf("pending order = %v, want %v", got, want)
	}
	if err = client.LRem(ctx, pendingKey, 0, "middle").Err(); err != nil {
		t.Fatal(err)
	}
	ids, err = inspector.listRuntimeTaskIDs(
		ctx, types.QueueDefault, types.RuntimeTaskPending, []string{"new", "middle"}, 2,
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids, []string{"old"}; !equalStrings(got, want) {
		t.Fatalf("anchor fallback order = %v, want %v", got, want)
	}

	scheduledKey, _ := runtimeTaskStateKey(types.QueueDefault, types.RuntimeTaskScheduled)
	if err = client.ZAdd(ctx, scheduledKey,
		redis.Z{Score: 30, Member: "later"},
		redis.Z{Score: 10, Member: "next"},
		redis.Z{Score: 20, Member: "after-next"},
	).Err(); err != nil {
		t.Fatal(err)
	}
	ids, err = inspector.listRuntimeTaskIDs(ctx, types.QueueDefault, types.RuntimeTaskScheduled, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids, []string{"next", "after-next", "later"}; !equalStrings(got, want) {
		t.Fatalf("scheduled order = %v, want %v", got, want)
	}

	archivedKey, _ := runtimeTaskStateKey(types.QueueDefault, types.RuntimeTaskArchived)
	if err = client.ZAdd(ctx, archivedKey,
		redis.Z{Score: 10, Member: "old-failure"},
		redis.Z{Score: 30, Member: "new-failure"},
		redis.Z{Score: 20, Member: "middle-failure"},
	).Err(); err != nil {
		t.Fatal(err)
	}
	ids, err = inspector.listRuntimeTaskIDs(ctx, types.QueueDefault, types.RuntimeTaskArchived, nil, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := ids, []string{"new-failure", "middle-failure", "old-failure"}; !equalStrings(got, want) {
		t.Fatalf("archived order = %v, want %v", got, want)
	}
}

func TestRuntimeTaskCursorIsBoundToQueueAndState(t *testing.T) {
	raw, err := encodeRuntimeTaskCursor(
		types.QueueDefault,
		types.RuntimeTaskArchived,
		[]string{"task-1", "task-2"},
	)
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
	if _, err = decodeRuntimeTaskCursor(raw, types.QueueDefault, types.RuntimeTaskRetry); err == nil {
		t.Fatal("cursor from another state should be rejected")
	}
	if _, err = decodeRuntimeTaskCursor("not-base64", types.QueueDefault, types.RuntimeTaskArchived); err == nil {
		t.Fatal("malformed cursor should be rejected")
	}
}

func TestListRuntimeTasksPaginatesNewestPendingTasksWithoutOverlap(t *testing.T) {
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	asynqClient := asynq.NewClientFromRedisClient(client)
	inspector := &asynqTaskInspector{
		inspector: asynq.NewInspectorFromRedisClient(client),
		redis:     client,
	}

	for _, id := range []string{"old", "middle", "new"} {
		_, err := asynqClient.Enqueue(
			asynq.NewTask(types.TypeDocumentProcess, []byte(`{"tenant_id":1,"knowledge_id":"knowledge-1"}`)),
			asynq.Queue(types.QueueDefault),
			asynq.TaskID(id),
		)
		if err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}
	pendingKey, _ := runtimeTaskStateKey(types.QueueDefault, types.RuntimeTaskPending)
	if err := client.LInsert(context.Background(), pendingKey, "AFTER", "new", "already-gone").Err(); err != nil {
		t.Fatalf("insert stale task id: %v", err)
	}

	first, supported, err := inspector.ListRuntimeTasks(
		context.Background(), types.QueueDefault, types.RuntimeTaskPending, "", 2,
	)
	if err != nil || !supported {
		t.Fatalf("first page: supported=%v err=%v", supported, err)
	}
	if got, want := runtimeTaskIDs(first.Tasks), []string{"new", "middle"}; !equalStrings(got, want) {
		t.Fatalf("first page = %v, want %v", got, want)
	}
	if !first.HasMore || first.NextCursor == "" {
		t.Fatalf("first page cursor missing: %+v", first)
	}

	second, supported, err := inspector.ListRuntimeTasks(
		context.Background(), types.QueueDefault, types.RuntimeTaskPending, first.NextCursor, 2,
	)
	if err != nil || !supported {
		t.Fatalf("second page: supported=%v err=%v", supported, err)
	}
	if got, want := runtimeTaskIDs(second.Tasks), []string{"old"}; !equalStrings(got, want) {
		t.Fatalf("second page = %v, want %v", got, want)
	}
	if second.HasMore || second.NextCursor != "" {
		t.Fatalf("unexpected continuation after final page: %+v", second)
	}
}

func runtimeTaskIDs(tasks []types.RuntimeTaskInfo) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
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
