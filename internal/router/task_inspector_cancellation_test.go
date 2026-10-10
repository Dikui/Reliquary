package router

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"

	"reliquary/internal/types"
)

func TestMatchesKnowledgePreservesPerKnowledgeAllowList(t *testing.T) {
	if !matchesKnowledge(types.TypeDocumentProcess, []byte(`{"knowledge_id":"knowledge-1"}`), "knowledge-1") {
		t.Fatal("document task should remain cancellable by knowledge ID")
	}
}

func enqueueTask(t *testing.T, client *asynq.Client, taskType, payload, taskID string) {
	t.Helper()
	if _, err := client.Enqueue(
		asynq.NewTask(taskType, []byte(payload)),
		asynq.Queue(types.QueueDefault),
		asynq.TaskID(taskID),
	); err != nil {
		t.Fatalf("enqueue %s: %v", taskID, err)
	}
}

func scheduleTask(t *testing.T, client *asynq.Client, taskType, payload, taskID string) {
	t.Helper()
	if _, err := client.Enqueue(
		asynq.NewTask(taskType, []byte(payload)),
		asynq.Queue(types.QueueDefault),
		asynq.TaskID(taskID),
		asynq.ProcessAt(time.Now().Add(time.Hour)),
	); err != nil {
		t.Fatalf("schedule %s: %v", taskID, err)
	}
}

func hasTaskID(tasks []*asynq.TaskInfo, taskID string) bool {
	for _, task := range tasks {
		if task.ID == taskID {
			return true
		}
	}
	return false
}

func taskIDs(tasks []*asynq.TaskInfo) []string {
	ids := make([]string, 0, len(tasks))
	for _, task := range tasks {
		ids = append(ids, task.ID)
	}
	return ids
}

func waitForTaskState(t *testing.T, inspector *asynq.Inspector, taskID string, want asynq.TaskState) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastState asynq.TaskState
	for time.Now().Before(deadline) {
		task, err := inspector.GetTaskInfo(types.QueueDefault, taskID)
		if err == nil {
			lastState = task.State
			if task.State == want {
				return
			}
		} else if !errors.Is(err, asynq.ErrTaskNotFound) {
			t.Fatalf("get task %s: %v", taskID, err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("task %s state = %v, want %v", taskID, lastState, want)
}

func waitForAsynqCancellationSubscriber(t *testing.T, redisClient *redis.Client) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		subscribers, err := redisClient.PubSubNumSub(context.Background(), "asynq:cancel").Result()
		if err != nil {
			t.Fatalf("query cancellation subscribers: %v", err)
		}
		if subscribers["asynq:cancel"] > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("asynq cancellation subscriber did not start")
}

func waitForTaskToLeaveLiveStates(t *testing.T, inspector *asynq.Inspector, taskID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var lastState asynq.TaskState
	for time.Now().Before(deadline) {
		task, err := inspector.GetTaskInfo(types.QueueDefault, taskID)
		if errors.Is(err, asynq.ErrTaskNotFound) {
			return
		}
		if err != nil {
			t.Fatalf("get task %s: %v", taskID, err)
		}
		lastState = task.State
		switch task.State {
		case asynq.TaskStatePending, asynq.TaskStateScheduled, asynq.TaskStateRetry, asynq.TaskStateActive:
			time.Sleep(10 * time.Millisecond)
		default:
			return
		}
	}
	t.Fatalf("task %s remained in live state %v", taskID, lastState)
}
