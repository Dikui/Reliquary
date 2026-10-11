package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/types"
)

type Client struct {
	db         *gorm.DB
	mu         sync.Mutex
	lockMu     sync.Mutex
	cancel     context.CancelFunc
	workerCtx  context.Context
	connection types.QueueConnectionStatus
	lock       *sql.Conn
	wg         sync.WaitGroup
	config     Config
	pools      []*workerPool
	active     map[string]context.CancelFunc
}

func NewClient(db *gorm.DB) *Client { return &Client{db: db, active: map[string]context.CancelFunc{}} }
func (c *Client) DB() *gorm.DB      { return c.db }
func (c *Client) Enqueue(t *Task, opts ...Option) (*TaskInfo, error) {
	return c.EnqueueTx(context.Background(), c.db, t, opts...)
}

// EnqueueTx uses the caller's connection, including its open GORM transaction.
// No job becomes visible to the workers until that transaction commits.
func (c *Client) EnqueueTx(ctx context.Context, tx *gorm.DB, t *Task, opts ...Option) (*TaskInfo, error) {
	if tx == nil || t == nil || t.Type() == "" {
		return nil, errors.New("task queue requires database and task type")
	}
	now := time.Now().UTC()
	payload := t.Payload()
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	if !json.Valid(payload) {
		return nil, errors.New("task payload must be JSON")
	}
	headers, err := json.Marshal(t.Headers())
	if err != nil {
		return nil, err
	}
	if string(headers) == "null" {
		headers = []byte("{}")
	}
	j := &Job{ID: uuid.NewString(), TaskType: t.Type(), Queue: types.QueueDefault, Payload: payload, Headers: headers, State: TaskStatePending, MaxRetry: 25, CreatedAt: now, UpdatedAt: now, ProcessAt: now}
	for _, o := range append(append([]Option(nil), t.options...), opts...) {
		switch o.Type() {
		case MaxRetryOpt:
			j.MaxRetry = o.Value().(int)
		case QueueOpt:
			j.Queue = o.Value().(string)
		case TimeoutOpt:
			j.TimeoutNS = int64(o.Value().(time.Duration))
		case RetentionOpt:
			j.RetentionNS = int64(o.Value().(time.Duration))
		case DeadlineOpt:
			v := o.Value().(time.Time).UTC()
			if !v.IsZero() {
				j.Deadline = &v
			}
		case GroupOpt:
			j.GroupName = o.Value().(string)
		case TaskIDOpt:
			j.ID = o.Value().(string)
		case ProcessAtOpt:
			j.ProcessAt = o.Value().(time.Time).UTC()
		case ProcessInOpt:
			j.ProcessAt = now.Add(o.Value().(time.Duration))
		}
	}
	if j.ID == "" || j.Queue == "" || j.TimeoutNS < 0 || j.RetentionNS < 0 {
		return nil, errors.New("invalid task queue options")
	}
	if j.TimeoutNS == 0 && j.Deadline == nil {
		j.TimeoutNS = int64(30 * time.Minute)
	}
	if j.ProcessAt.After(now) {
		j.State = TaskStateScheduled
	}
	// ON CONFLICT keeps a duplicate schedule ID from poisoning an outer
	// PostgreSQL transaction before its caller can recognize the duplicate.
	result := tx.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(j)
	if result.Error != nil {
		var pgErr *pgconn.PgError
		if errors.As(result.Error, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrTaskIDConflict
		}
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, ErrTaskIDConflict
	}
	return j.Info(), nil
}

func (c *Client) GetTaskInfo(ctx context.Context, queue, id string) (*TaskInfo, error) {
	var j Job
	err := c.db.WithContext(ctx).Where("id = ? AND queue = ?", id, queue).First(&j).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrTaskNotFound
	}
	if err != nil {
		return nil, err
	}
	return j.Info(), nil
}

func (c *Client) Cancel(ctx context.Context, id string) error {
	var active bool
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ?", id).Error; err != nil {
			return err
		}
		active = j.State == TaskStateActive
		if active {
			return tx.Model(&j).Updates(map[string]interface{}{"cancel_requested": true, "updated_at": time.Now().UTC()}).Error
		}
		if j.State != TaskStatePending && j.State != TaskStateScheduled && j.State != TaskStateRetry {
			return ErrTaskState
		}
		return tx.Delete(&j).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrTaskNotFound
	}
	if err != nil {
		return err
	}
	if active {
		c.mu.Lock()
		cancel := c.active[id]
		c.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
	return nil
}

func (c *Client) RunNow(ctx context.Context, id string) error {
	return c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var j Job
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&j, "id = ?", id).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTaskNotFound
			}
			return err
		}
		if j.State == TaskStateActive {
			return ErrTaskActive
		}
		if j.State != TaskStateRetry && j.State != TaskStateScheduled && j.State != TaskStateArchived {
			return ErrTaskState
		}
		updates := map[string]interface{}{"state": TaskStatePending, "process_at": time.Now().UTC(), "updated_at": time.Now().UTC(), "cancel_requested": false}
		return tx.Model(&j).Updates(updates).Error
	})
}

func (c *Client) Delete(ctx context.Context, id string) error {
	result := c.db.WithContext(ctx).Where("id = ? AND state <> ?", id, TaskStateActive).Delete(&Job{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var count int64
	if err := c.db.WithContext(ctx).Model(&Job{}).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return ErrTaskActive
	}
	return ErrTaskNotFound
}

func (c *Client) Purge(ctx context.Context, queue string, state TaskState) (int64, error) {
	if state == TaskStateActive {
		return 0, ErrTaskActive
	}
	r := c.db.WithContext(ctx).Where("queue = ? AND state = ?", queue, state).Delete(&Job{})
	return r.RowsAffected, r.Error
}

// Start owns one database-scoped advisory lock for the single application
// process. A fresh process can therefore recover interrupted jobs immediately,
// without racing a still-live process or spending an ordinary retry.
func (c *Client) Start(ctx context.Context, handler Handler, config Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("task worker is already started")
	}
	if c.db == nil || handler == nil {
		return errors.New("task worker requires database and handler")
	}
	c.config = config.withDefaults()
	conn, err := c.acquire(ctx)
	if err != nil {
		return err
	}
	if err = c.recoverJobs(ctx, true); err != nil {
		releaseLock(conn)
		return err
	}
	root, cancel := context.WithCancel(ctx)
	c.cancel, c.lock, c.workerCtx = cancel, conn, root
	c.pools = c.config.workerPools()
	c.connection = types.QueueConnectionStatus{State: "active"}
	c.wg.Add(1)
	go func() { defer c.wg.Done(); c.supervise(root, handler, conn) }()
	return nil
}
func (c *Client) Close() error {
	c.mu.Lock()
	cancel := c.cancel
	c.mu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel, c.workerCtx, c.lock = nil, nil, nil
	c.connection.State = "stopped"
	return nil
}
func (c *Client) ConnectionStatus() types.QueueConnectionStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.connection
	if status.State == "" {
		status.State = "stopped"
	}
	status.AlertAfterSeconds = int64(c.config.ReconnectAlertAfter.Seconds())
	status.Alert = status.State == "reconnecting" && status.Since != nil && time.Since(*status.Since) >= c.config.ReconnectAlertAfter
	return status
}

func (c *Client) WorkerStats(ctx context.Context) ([]types.WorkerServerStat, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stats := make([]types.WorkerServerStat, 0, len(c.pools))
	for _, p := range c.pools {
		weights := map[string]int{}
		for q, w := range p.weights {
			weights[q] = w
		}
		status := "active"
		if c.cancel == nil || c.workerCtx == nil || c.workerCtx.Err() != nil {
			status = "stopped"
		}
		stats = append(stats, types.WorkerServerStat{Concurrency: p.concurrency, Active: int(p.active.Load()), Status: status, Queues: weights})
	}
	return stats, nil
}
