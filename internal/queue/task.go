// Package queue stores durable operations in PostgreSQL and dispatches them through asynq.
package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type OptionType int

const (
	MaxRetryOpt OptionType = iota
	QueueOpt
	TimeoutOpt
	DeadlineOpt
	ProcessAtOpt
	ProcessInOpt
	TaskIDOpt
	RetentionOpt
	GroupOpt
)

type Option interface {
	Type() OptionType
	Value() interface{}
	String() string
}

type option struct {
	kind  OptionType
	value interface{}
}

func (o option) Type() OptionType      { return o.kind }
func (o option) Value() interface{}    { return o.value }
func (o option) String() string        { return fmt.Sprintf("Option(%d,%v)", o.kind, o.value) }
func MaxRetry(n int) Option            { return option{MaxRetryOpt, max(0, n)} }
func Queue(name string) Option         { return option{QueueOpt, name} }
func Timeout(d time.Duration) Option   { return option{TimeoutOpt, d} }
func Deadline(t time.Time) Option      { return option{DeadlineOpt, t} }
func ProcessAt(t time.Time) Option     { return option{ProcessAtOpt, t} }
func ProcessIn(d time.Duration) Option { return option{ProcessInOpt, d} }
func TaskID(id string) Option          { return option{TaskIDOpt, id} }
func Retention(d time.Duration) Option { return option{RetentionOpt, d} }
func Group(name string) Option         { return option{GroupOpt, name} }

var (
	SkipRetry         = errors.New("task retry skipped")
	ErrTaskIDConflict = errors.New("task ID already exists")
	ErrTaskNotFound   = errors.New("task not found")
	ErrTaskActive     = errors.New("task is active")
	ErrTaskState      = errors.New("task state does not allow this action")
)

type Task struct {
	taskType string
	payload  []byte
	headers  map[string]string
	options  []Option
}

func NewTask(taskType string, payload []byte, opts ...Option) *Task {
	return &Task{taskType: taskType, payload: append([]byte(nil), payload...), options: opts}
}
func NewTaskWithHeaders(taskType string, payload []byte, headers map[string]string, opts ...Option) *Task {
	t := NewTask(taskType, payload, opts...)
	t.headers = make(map[string]string, len(headers))
	for k, v := range headers {
		t.headers[k] = v
	}
	return t
}
func (t *Task) Type() string               { return t.taskType }
func (t *Task) Payload() []byte            { return t.payload }
func (t *Task) Headers() map[string]string { return t.headers }

type TaskState string

const (
	TaskStatePending    TaskState = "pending"
	TaskStateDispatched TaskState = "dispatched"
	TaskStateCancelled  TaskState = "cancelled"
	TaskStateActive     TaskState = "active"
	TaskStateScheduled  TaskState = "scheduled"
	TaskStateRetry      TaskState = "retry"
	TaskStateArchived   TaskState = "archived"
	TaskStateCompleted  TaskState = "completed"
)

type TaskInfo struct {
	ID, Queue, Type                       string
	Payload                               []byte
	Headers                               map[string]string
	State                                 TaskState
	MaxRetry, Retried                     int
	LastErr                               string
	LastFailedAt                          time.Time
	Timeout, Retention                    time.Duration
	Deadline                              time.Time
	Group                                 string
	NextProcessAt, CompletedAt, CreatedAt time.Time
}

type taskContextKey struct{}
type execution struct {
	ID, Queue, Pool   string
	Retried, MaxRetry int
}

func WithExecution(ctx context.Context, info *TaskInfo, pool string) context.Context {
	return context.WithValue(ctx, taskContextKey{}, execution{info.ID, info.Queue, pool, info.Retried, info.MaxRetry})
}
func GetRetryCount(ctx context.Context) (int, bool) {
	e, ok := ctx.Value(taskContextKey{}).(execution)
	return e.Retried, ok
}
func GetMaxRetry(ctx context.Context) (int, bool) {
	e, ok := ctx.Value(taskContextKey{}).(execution)
	return e.MaxRetry, ok
}
func GetQueueName(ctx context.Context) (string, bool) {
	e, ok := ctx.Value(taskContextKey{}).(execution)
	return e.Queue, ok
}
func GetTaskID(ctx context.Context) (string, bool) {
	e, ok := ctx.Value(taskContextKey{}).(execution)
	return e.ID, ok
}
func GetWorkerPool(ctx context.Context) (string, bool) {
	e, ok := ctx.Value(taskContextKey{}).(execution)
	return e.Pool, ok
}

type Handler interface {
	ProcessTask(context.Context, *Task) error
}
type HandlerFunc func(context.Context, *Task) error

func (f HandlerFunc) ProcessTask(ctx context.Context, t *Task) error { return f(ctx, t) }

type MiddlewareFunc func(Handler) Handler
type ServeMux struct {
	mu         sync.RWMutex
	handlers   map[string]Handler
	middleware []MiddlewareFunc
}

func NewServeMux() *ServeMux { return &ServeMux{handlers: map[string]Handler{}} }
func (m *ServeMux) Use(middleware ...MiddlewareFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.middleware = append(m.middleware, middleware...)
}
func (m *ServeMux) HandleFunc(taskType string, f HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[taskType] = f
}
func (m *ServeMux) ProcessTask(ctx context.Context, t *Task) error {
	m.mu.RLock()
	h := m.handlers[t.Type()]
	middleware := append([]MiddlewareFunc(nil), m.middleware...)
	m.mu.RUnlock()
	if h == nil {
		return fmt.Errorf("unknown task type %s: %w", t.Type(), SkipRetry)
	}
	for i := len(middleware) - 1; i >= 0; i-- {
		h = middleware[i](h)
	}
	return h.ProcessTask(ctx, t)
}
