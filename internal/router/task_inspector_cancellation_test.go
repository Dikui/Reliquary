package router

import (
	"context"
	"testing"
	"time"

	task "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestMatchesEntryPreservesPerEntryAllowList(t *testing.T) {
	if !matchesEntry(types.TypeDocumentProcess, []byte(`{"entry_id":"entry-1"}`), "entry-1") {
		t.Fatal("document task should remain cancellable by entry ID")
	}
	if matchesEntry(types.TypeEntryListReparse, []byte(`{"entry_id":"entry-1"}`), "entry-1") {
		t.Fatal("data source sync must not be cancelled as article processing")
	}
	if matchesEntry(types.TypeDocumentProcess, []byte(`{"entry_id":"entry-2"}`), "entry-1") {
		t.Fatal("another article must remain unaffected")
	}
}

func TestPostgresEntryCancellationRemovesAllQueuedStatesAndSignalsActive(t *testing.T) {
	inspector := newPostgresInspectorTest(t)
	ctx := context.Background()
	states := []types.RuntimeTaskState{types.RuntimeTaskPending, types.RuntimeTaskScheduled, types.RuntimeTaskRetry, types.RuntimeTaskActive, types.RuntimeTaskArchived}
	for _, state := range states {
		insertRuntimeJob(t, inspector, string(state), `{"entry_id":"entry-1"}`, types.QueueDefault, state, time.Now())
	}
	insertRuntimeJob(t, inspector, "other", `{"entry_id":"entry-2"}`, types.QueueDefault, types.RuntimeTaskPending, time.Now())
	exists, err := inspector.HasQueuedTasksForEntry(ctx, "entry-1")
	if err != nil || !exists {
		t.Fatalf("live tasks not found: exists=%v err=%v", exists, err)
	}
	deleted, cancelled, err := inspector.CancelTasksForEntry(ctx, "entry-1")
	if err != nil || deleted != 3 || cancelled != 1 {
		t.Fatalf("cancel deleted=%d signaled=%d err=%v", deleted, cancelled, err)
	}
	for _, state := range states[:3] {
		if _, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, string(state)); err == nil {
			t.Fatalf("%s task survived", state)
		}
	}
	var active task.Job
	if err := inspector.db.Where("id = ?", "active").First(&active).Error; err != nil || !active.CancelRequested {
		t.Fatalf("active task did not receive persistent cancellation: %+v err=%v", active, err)
	}
	for _, id := range []string{"other", "archived"} {
		if _, _, err := inspector.GetRuntimeTask(ctx, types.QueueDefault, id); err != nil {
			t.Fatalf("unrelated/history task %s removed: %v", id, err)
		}
	}
	// The durable active marker stays live until the worker acknowledges it,
	// preventing orphan housekeeping from failing an article during that gap.
	exists, err = inspector.HasQueuedTasksForEntry(ctx, "entry-1")
	if err != nil || !exists {
		t.Fatalf("active cancellation marker lost: exists=%v err=%v", exists, err)
	}
}
