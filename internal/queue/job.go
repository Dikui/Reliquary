package queue

import (
	"encoding/json"
	"time"
)

// Job is the persisted record read by the worker and runtime inspector.
type Job struct {
	ID                                               string `gorm:"primaryKey"`
	TaskType                                         string
	Queue                                            string
	Payload                                          json.RawMessage `gorm:"type:jsonb"`
	Headers                                          json.RawMessage `gorm:"type:jsonb"`
	State                                            TaskState
	MaxRetry, Retried, FailCount                     int
	TimeoutNS                                        int64 `gorm:"column:timeout_ns"`
	RetentionNS                                      int64 `gorm:"column:retention_ns"`
	Deadline                                         *time.Time
	GroupName                                        string
	CreatedAt, UpdatedAt, ProcessAt                  time.Time
	StartedAt, CompletedAt, LastFailedAt, LeaseUntil *time.Time
	LastError, ClaimToken, WorkerPool                string
	CancelRequested                                  bool
}

func (Job) TableName() string { return "task_jobs" }
func (j *Job) Info() *TaskInfo {
	i := &TaskInfo{ID: j.ID, Type: j.TaskType, Queue: j.Queue, Payload: append([]byte(nil), j.Payload...), State: j.State, MaxRetry: j.MaxRetry, Retried: j.Retried, LastErr: j.LastError, Timeout: time.Duration(j.TimeoutNS), Retention: time.Duration(j.RetentionNS), Group: j.GroupName, NextProcessAt: j.ProcessAt, CreatedAt: j.CreatedAt}
	_ = json.Unmarshal(j.Headers, &i.Headers)
	if j.Deadline != nil {
		i.Deadline = *j.Deadline
	}
	if j.CompletedAt != nil {
		i.CompletedAt = *j.CompletedAt
	}
	if j.LastFailedAt != nil {
		i.LastFailedAt = *j.LastFailedAt
	}
	return i
}
