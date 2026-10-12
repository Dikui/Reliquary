package router

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	task "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// postgresTaskInspector reads the same durable rows the workers claim.
// Cancellation uses the client so an active worker receives its cancellation
// signal as well as the persistent cancellation marker.
type postgresTaskInspector struct {
	client *task.Client
	db     *gorm.DB
}

func NewPostgresTaskInspector(client *task.Client) interfaces.TaskInspector {
	if client == nil || client.DB() == nil {
		panic("task inspection requires a PostgreSQL task client")
	}
	return &postgresTaskInspector{client: client, db: client.DB()}
}

type entryIDProbe struct {
	EntryID string `json:"knowledge_id,omitempty"`
}

type runtimeTaskPayloadProbe struct {
	SpaceID         uint64   `json:"tenant_id,omitempty"`
	KnowledgeBaseID string   `json:"knowledge_base_id,omitempty"`
	EntryID         string   `json:"knowledge_id,omitempty"`
	TaskID          string   `json:"task_id,omitempty"`
	JobID           string   `json:"job_id,omitempty"`
	DataSourceID    string   `json:"data_source_id,omitempty"`
	SyncLogID       string   `json:"sync_log_id,omitempty"`
	EntryIDs        []string `json:"knowledge_ids,omitempty"`
	EnqueuedAt      int64    `json:"enqueued_at,omitempty"`
	CreatedAt       int64    `json:"created_at,omitempty"`
}

var taskTypesForEntryCancel = map[string]struct{}{
	types.TypeDocumentProcess: {},
	types.TypeManualProcess:   {},
}

const listPageSize = 100

func (a *postgresTaskInspector) CancelTasksForEntry(ctx context.Context, entryID string) (int, int, error) {
	if entryID == "" {
		return 0, 0, nil
	}
	var jobs []task.Job
	err := a.db.WithContext(ctx).Where(
		"task_type IN ? AND COALESCE(payload->>'knowledge_id',payload->>'entry_id') = ? AND state IN ?",
		[]string{types.TypeDocumentProcess, types.TypeManualProcess}, entryID,
		[]string{"pending", "active", "scheduled", "retry"},
	).Find(&jobs).Error
	if err != nil {
		return 0, 0, err
	}
	deleted, cancelled := 0, 0
	for _, job := range jobs {
		if err := a.client.Cancel(ctx, job.ID); err != nil {
			if errors.Is(err, task.ErrTaskNotFound) || errors.Is(err, task.ErrTaskState) {
				continue
			}
			return deleted, cancelled, err
		}
		if string(job.State) == "active" {
			cancelled++
		} else {
			deleted++
		}
	}
	return deleted, cancelled, nil
}

func (a *postgresTaskInspector) HasQueuedTasksForEntry(ctx context.Context, entryID string) (bool, error) {
	if entryID == "" {
		return false, nil
	}
	var count int64
	err := a.db.WithContext(ctx).Model(&task.Job{}).Where(
		"task_type IN ? AND COALESCE(payload->>'knowledge_id',payload->>'entry_id') = ? AND state IN ?",
		[]string{types.TypeDocumentProcess, types.TypeManualProcess}, entryID,
		[]string{"pending", "active", "scheduled", "retry"},
	).Count(&count).Error
	return count > 0, err
}

func (a *postgresTaskInspector) QueueStats(ctx context.Context) ([]types.QueueStat, bool, error) {
	var counts []struct {
		Queue     string
		State     string
		Count     int
		LatencyMs int64
	}
	err := a.db.WithContext(ctx).Raw(`SELECT queue, state, count(*) AS count,
		CASE WHEN state = 'pending' THEN GREATEST(0, EXTRACT(EPOCH FROM (now() - min(created_at))) * 1000)::bigint ELSE 0 END AS latency_ms
		FROM task_jobs GROUP BY queue, state`).Scan(&counts).Error
	if err != nil {
		return nil, true, err
	}
	var counters []struct {
		Queue     string
		Processed int
		Failed    int
	}
	if err = a.db.WithContext(ctx).Raw(`SELECT queue, processed, failed FROM task_queue_counters WHERE day = (now() AT TIME ZONE 'UTC')::date`).Scan(&counters).Error; err != nil {
		return nil, true, err
	}
	stats := make([]types.QueueStat, 0, len(types.QueueDefinitions()))
	for _, definition := range types.QueueDefinitions() {
		stat := types.QueueStat{Name: definition.Name, Pool: definition.Pool, Weight: definition.Weight}
		for _, row := range counts {
			if row.Queue != stat.Name {
				continue
			}
			switch row.State {
			case "pending":
				stat.Pending, stat.LatencyMs = row.Count, row.LatencyMs
			case "active":
				stat.Active = row.Count
			case "scheduled":
				stat.Scheduled = row.Count
			case "retry":
				stat.Retry = row.Count
			case "archived":
				stat.Archived = row.Count
			case "completed":
				stat.Completed = row.Count
			}
		}
		stat.Size = stat.Pending + stat.Active + stat.Scheduled + stat.Retry + stat.Archived
		for _, row := range counters {
			if row.Queue == stat.Name {
				stat.Processed, stat.Failed = row.Processed, row.Failed
			}
		}
		stats = append(stats, stat)
	}
	return stats, true, nil
}

func runtimeTaskTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func runtimePayloadTime(value int64) *time.Time {
	if value <= 0 {
		return nil
	}
	var parsed time.Time
	if value > 100_000_000_000_000_000 {
		parsed = time.Unix(0, value)
	} else if value > 100_000_000_000_000 {
		parsed = time.UnixMicro(value)
	} else if value > 10_000_000_000 {
		parsed = time.UnixMilli(value)
	} else {
		parsed = time.Unix(value, 0)
	}
	return &parsed
}

func runtimeTaskActions(info types.RuntimeTaskInfo) []types.RuntimeTaskAction {
	actions := make([]types.RuntimeTaskAction, 0, 3)
	if info.SpaceID != 0 && info.SpaceID != types.SingleSpaceID {
		return actions
	}
	if _, cancellable := taskTypesForEntryCancel[info.Type]; cancellable && info.EntryID != "" {
		switch info.State {
		case types.RuntimeTaskPending, types.RuntimeTaskActive, types.RuntimeTaskScheduled, types.RuntimeTaskRetry:
			actions = append(actions, types.RuntimeTaskActionCancel)
		}
	}
	switch info.State {
	case types.RuntimeTaskScheduled, types.RuntimeTaskRetry:
		actions = append(actions, types.RuntimeTaskActionRunNow)
	case types.RuntimeTaskArchived:
		actions = append(actions, types.RuntimeTaskActionRunNow, types.RuntimeTaskActionDelete)
	}
	return actions
}

func projectRuntimeTask(job *task.Job) (types.RuntimeTaskInfo, error) {
	state := types.RuntimeTaskState(job.State)
	if !state.Valid() {
		return types.RuntimeTaskInfo{}, fmt.Errorf("unsupported runtime task state %q", state)
	}
	probe := runtimeTaskPayloadProbe{}
	payloadValid := types.UnmarshalSpacePayload(job.Payload, &probe) == nil
	enqueuedAt := probe.EnqueuedAt
	if enqueuedAt == 0 {
		enqueuedAt = probe.CreatedAt
	}
	info := types.RuntimeTaskInfo{
		ID: job.ID, Queue: job.Queue, Type: job.TaskType, State: state,
		LastError: job.LastError, LastFailedAt: job.LastFailedAt, NextProcessAt: runtimeTaskTime(job.ProcessAt),
		StartedAt: job.StartedAt, CompletedAt: job.CompletedAt, Deadline: job.Deadline,
		EnqueuedAt: runtimePayloadTime(enqueuedAt), Retried: job.Retried, MaxRetry: job.MaxRetry,
		Worker: job.WorkerPool, SpaceID: probe.SpaceID, KnowledgeBaseID: probe.KnowledgeBaseID,
		EntryID: probe.EntryID, TaskID: probe.TaskID, DataSourceID: probe.DataSourceID,
		SyncLogID: probe.SyncLogID, EntryCount: len(probe.EntryIDs),
		AllowedActions: []types.RuntimeTaskAction{},
	}
	if job.TaskType == types.TypeTopicBackfill {
		info.TaskID = probe.JobID
	}
	if info.EnqueuedAt == nil {
		info.EnqueuedAt = runtimeTaskTime(job.CreatedAt)
	}
	if state == types.RuntimeTaskActive && job.LeaseUntil != nil {
		info.IsOrphaned = job.LeaseUntil.Before(time.Now())
	}
	if payloadValid {
		info.AllowedActions = runtimeTaskActions(info)
	}
	return info, nil
}

const (
	runtimeTaskCursorVersion    = 1
	runtimeTaskCursorMaxAnchors = 32
	runtimeTaskCursorMaxBytes   = 16 * 1024
)

type runtimeTaskCursor struct {
	Version int                    `json:"v"`
	Queue   string                 `json:"q"`
	State   types.RuntimeTaskState `json:"s"`
	Anchors []string               `json:"a"`
}

func decodeRuntimeTaskCursor(raw, queue string, state types.RuntimeTaskState) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > runtimeTaskCursorMaxBytes {
		return nil, types.ErrInvalidRuntimeTaskCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, types.ErrInvalidRuntimeTaskCursor
	}
	var cursor runtimeTaskCursor
	if err = json.Unmarshal(data, &cursor); err != nil || cursor.Version != runtimeTaskCursorVersion ||
		cursor.Queue != queue || cursor.State != state || len(cursor.Anchors) == 0 || len(cursor.Anchors) > runtimeTaskCursorMaxAnchors {
		return nil, types.ErrInvalidRuntimeTaskCursor
	}
	for _, anchor := range cursor.Anchors {
		if anchor == "" {
			return nil, types.ErrInvalidRuntimeTaskCursor
		}
	}
	return cursor.Anchors, nil
}

func encodeRuntimeTaskCursor(queue string, state types.RuntimeTaskState, anchors []string) (string, error) {
	if len(anchors) > runtimeTaskCursorMaxAnchors {
		anchors = anchors[len(anchors)-runtimeTaskCursorMaxAnchors:]
	}
	data, err := json.Marshal(runtimeTaskCursor{Version: runtimeTaskCursorVersion, Queue: queue, State: state, Anchors: anchors})
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// The cursor uses a bounded history of IDs. A surviving earlier anchor keeps
// pagination usable when the last task finishes or is removed between pages.
func runtimeTaskOrder(state types.RuntimeTaskState) string {
	switch state {
	case types.RuntimeTaskActive:
		return "started_at DESC NULLS LAST, id DESC"
	case types.RuntimeTaskScheduled, types.RuntimeTaskRetry:
		return "process_at ASC, id ASC"
	case types.RuntimeTaskArchived:
		return "last_failed_at DESC NULLS LAST, id DESC"
	case types.RuntimeTaskCompleted:
		return "completed_at DESC NULLS LAST, id DESC"
	default:
		return "created_at DESC, id DESC"
	}
}

func (a *postgresTaskInspector) ListRuntimeTasks(ctx context.Context, queue string, state types.RuntimeTaskState, cursor string, pageSize int) (types.RuntimeTaskPage, bool, error) {
	if !state.Valid() {
		return types.RuntimeTaskPage{}, true, fmt.Errorf("unsupported runtime task state %q", state)
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	anchors, err := decodeRuntimeTaskCursor(cursor, queue, state)
	if err != nil {
		return types.RuntimeTaskPage{}, true, err
	}
	// One statement resolves the surviving anchor and reads its following rows
	// from one PostgreSQL snapshot, avoiding page shifts during concurrent work.
	query := `WITH ordered AS (SELECT task_jobs.*, row_number() OVER (ORDER BY ` + runtimeTaskOrder(state) + `) AS position
		FROM task_jobs WHERE queue = ? AND state = ?), anchor AS (
		SELECT position FROM ordered JOIN jsonb_array_elements_text(?::jsonb) WITH ORDINALITY AS anchors(id, ordinal) USING (id)
		ORDER BY ordinal DESC LIMIT 1)
		SELECT ordered.* FROM ordered WHERE position > COALESCE((SELECT position FROM anchor), 0)
		ORDER BY position LIMIT ?`
	var jobs []struct {
		task.Job
		Position int64
	}
	if anchors == nil {
		anchors = []string{}
	}
	anchorJSON, err := json.Marshal(anchors)
	if err != nil {
		return types.RuntimeTaskPage{}, true, err
	}
	err = a.db.WithContext(ctx).Raw(query, queue, string(state), string(anchorJSON), pageSize+1).Scan(&jobs).Error
	if err != nil {
		return types.RuntimeTaskPage{}, true, err
	}
	if cursor != "" && (len(jobs) == 0 || jobs[0].Position == 1) {
		// Empty can also mean the anchor is the last task. Check that case
		// explicitly before declaring the cursor stale.
		var count int64
		if err = a.db.WithContext(ctx).Model(&task.Job{}).Where("queue = ? AND state = ? AND id IN ?", queue, string(state), anchors).Count(&count).Error; err != nil {
			return types.RuntimeTaskPage{}, true, err
		}
		if count == 0 {
			return types.RuntimeTaskPage{}, true, types.ErrExpiredRuntimeTaskCursor
		}
	}
	page := types.RuntimeTaskPage{Tasks: make([]types.RuntimeTaskInfo, 0, pageSize), HasMore: len(jobs) > pageSize}
	if page.HasMore {
		jobs = jobs[:pageSize]
	}
	if cursor == "" {
		anchors = nil
	}
	for _, job := range jobs {
		info, projectErr := projectRuntimeTask(&job.Job)
		if projectErr != nil {
			return types.RuntimeTaskPage{}, true, projectErr
		}
		page.Tasks = append(page.Tasks, info)
		anchors = append(anchors, job.ID)
	}
	if page.HasMore {
		page.NextCursor, err = encodeRuntimeTaskCursor(queue, state, anchors)
	}
	return page, true, err
}

func (a *postgresTaskInspector) GetRuntimeTask(ctx context.Context, queue, taskID string) (*types.RuntimeTaskInfo, bool, error) {
	var job task.Job
	if err := a.db.WithContext(ctx).Where("queue = ? AND id = ?", queue, taskID).First(&job).Error; err != nil {
		return nil, true, err
	}
	info, err := projectRuntimeTask(&job)
	return &info, true, err
}

func (a *postgresTaskInspector) RunRuntimeTask(ctx context.Context, queue, taskID string) (bool, error) {
	info, _, err := a.GetRuntimeTask(ctx, queue, taskID)
	if err != nil {
		return true, err
	}
	if !info.Allows(types.RuntimeTaskActionRunNow) {
		return true, fmt.Errorf("task %s in queue %s cannot run now", taskID, queue)
	}
	return true, a.client.RunNow(ctx, taskID)
}

func (a *postgresTaskInspector) DeleteRuntimeTask(ctx context.Context, queue, taskID string) (bool, error) {
	var job task.Job
	if err := a.db.WithContext(ctx).Where("queue = ? AND id = ?", queue, taskID).First(&job).Error; err != nil {
		return true, err
	}
	info, err := projectRuntimeTask(&job)
	if err != nil {
		return true, err
	}
	if !info.Allows(types.RuntimeTaskActionDelete) {
		return true, fmt.Errorf("task %s in queue %s cannot be deleted", taskID, queue)
	}
	count, err := a.deleteArchivedRuntimeTask(ctx, queue, taskID, job.Payload)
	if err == nil && count == 0 {
		err = task.ErrTaskState
	}
	return true, err
}

func (a *postgresTaskInspector) ForceDeleteRuntimeTask(ctx context.Context, queue, taskID string) (bool, error) {
	info, _, err := a.GetRuntimeTask(ctx, queue, taskID)
	if err != nil {
		return true, err
	}
	if info.State == types.RuntimeTaskActive {
		return true, a.client.Cancel(ctx, taskID)
	}
	return true, a.client.Delete(ctx, taskID)
}

// A state and payload comparison makes archived deletion safe against a
// concurrent run-now or metadata update after the operator snapshot.
func (a *postgresTaskInspector) deleteArchivedRuntimeTask(ctx context.Context, queue, taskID string, payload []byte) (int, error) {
	result := a.db.WithContext(ctx).Exec(`DELETE FROM task_jobs WHERE queue = ? AND id = ? AND state = 'archived' AND payload = ?::jsonb`, queue, taskID, string(payload))
	return int(result.RowsAffected), result.Error
}

func (a *postgresTaskInspector) PurgeArchivedRuntimeTasks(ctx context.Context, queue string) (int, bool, error) {
	var jobs []task.Job
	if err := a.db.WithContext(ctx).Where("queue = ? AND state = ?", queue, "archived").Find(&jobs).Error; err != nil {
		return 0, true, err
	}
	deleted := 0
	for _, job := range jobs {
		info, err := projectRuntimeTask(&job)
		if err != nil || !info.Allows(types.RuntimeTaskActionDelete) {
			continue
		}
		count, err := a.deleteArchivedRuntimeTask(ctx, queue, job.ID, job.Payload)
		if err != nil {
			return deleted, true, err
		}
		deleted += count
	}
	return deleted, true, nil
}

func (a *postgresTaskInspector) WorkerServerStats(ctx context.Context) ([]types.WorkerServerStat, bool, error) {
	stats, err := a.client.WorkerStats(ctx)
	return stats, true, err
}

func matchesEntry(taskType string, payload []byte, entryID string) bool {
	if _, ok := taskTypesForEntryCancel[taskType]; !ok {
		return false
	}
	var probe entryIDProbe
	if err := types.UnmarshalSpacePayload(payload, &probe); err != nil {
		return false
	}
	return probe.EntryID == entryID
}

func (a *postgresTaskInspector) QueueConnectionStatus() types.QueueConnectionStatus {
	return a.client.ConnectionStatus()
}
