package queue

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reliquary/internal/types"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Client struct {
	observerMu     sync.Mutex
	observerCtx    context.Context
	observerCancel context.CancelFunc
	observerWG     sync.WaitGroup
	closed         bool
	suspending     map[string]bool
	objects        map[string]string
	db             *gorm.DB
	mu             sync.Mutex
	dispatchMu     sync.Mutex
	cancel         context.CancelFunc
	workerCtx      context.Context
	connection     types.QueueConnectionStatus
	lock           *sql.Conn
	wg             sync.WaitGroup
	config         Config
	pools          []*workerPool
	active         map[string]context.CancelFunc
	redisOpt       asynq.RedisClientOpt
	transport      *asynq.Client
	wake           chan struct{}
	namespace      string
}

func NewClient(db *gorm.DB) *Client { return NewClientWithRedis(db, RedisOptions()) }
func NewClientWithRedis(db *gorm.DB, opt asynq.RedisClientOpt) *Client {
	var name string
	if db != nil {
		_ = db.Raw("SELECT current_database()").Scan(&name).Error
	}
	observerCtx, observerCancel := context.WithCancel(context.Background())
	return &Client{observerCtx: observerCtx, observerCancel: observerCancel, namespace: name, db: db, active: map[string]context.CancelFunc{}, objects: map[string]string{}, suspending: map[string]bool{}, redisOpt: opt, wake: make(chan struct{}, 1)}
}
func (c *Client) DB() *gorm.DB { return c.db }
func (c *Client) Enqueue(t *Task, opts ...Option) (*TaskInfo, error) {
	var info *TaskInfo
	err := c.db.Transaction(func(tx *gorm.DB) error {
		var err error
		info, err = c.EnqueueTx(context.Background(), tx, t, opts...)
		return err
	})
	if err == nil {
		c.Wake()
	}
	return info, err
}

// EnqueueTx uses the caller's connection, including its open GORM transaction.
// No operation becomes visible until commit. Wake should be called
// by the transaction owner; the periodic sweep also covers missed notifications.
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
	j := &Job{ID: uuid.NewString(), TaskType: t.Type(), Queue: types.QueueDefault, Payload: payload, Headers: headers, State: TaskStatePending, Generation: 1, Delivery: 1, MaxRetry: 25, CreatedAt: now, UpdatedAt: now, ProcessAt: now}
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
	var identity struct {
		EntryID string `json:"entry_id"`
		JobID   string `json:"job_id"`
		RunID   string `json:"run_id"`
		AssetID string `json:"asset_id"`
		Attempt int64  `json:"attempt"`
	}
	_ = json.Unmarshal(payload, &identity)
	var entryEnvelope types.DocumentProcessPayload
	if json.Unmarshal(payload, &entryEnvelope) == nil && identity.EntryID == "" {
		identity.EntryID = entryEnvelope.EntryID
	}
	for _, id := range []string{identity.EntryID, identity.JobID, identity.RunID, identity.AssetID} {
		if id != "" {
			j.ObjectID = id
			break
		}
	}
	if j.ObjectID == "" {
		j.ObjectID = j.ID
	}
	// Serialize generations with the target business row when one exists.
	// The payload's attempt remains untouched for existing span checkpoints.
	table := ""
	switch j.TaskType {
	case types.TypeDocumentProcess, types.TypeManualProcess, types.TypeReportIndex:
		table = "entries"
	case types.TypeEmbeddingRebuild:
		table = "embedding_rebuild_runs"
	case types.TypeEntityNameEmbeddingRebuild:
		table = "entity_embedding_rebuild_runs"
	case types.TypeIngestAssetCleanup:
		table = "ingest_assets"
	}
	if table != "" {
		var locked []string
		if err := tx.WithContext(ctx).Table(table).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=?", j.ObjectID).Pluck("id", &locked).Error; err != nil {
			return nil, err
		}
	}
	if err := tx.WithContext(ctx).Model(&Job{}).Select("COALESCE(MAX(generation),0)+1").Where("object_id=?", j.ObjectID).Scan(&j.Generation).Error; err != nil {
		return nil, err
	}
	if identity.Attempt > j.Generation {
		j.Generation = identity.Attempt
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
	if j.TaskType == types.TypeDocumentProcess || j.TaskType == types.TypeManualProcess {
		// A newer user operation supersedes older queued work in the same business
		// transaction. Even deleting the new operation cannot revive an old message.
		if err := tx.WithContext(ctx).Model(&Job{}).Where("object_id=? AND id<>? AND task_type IN ? AND state IN ? AND generation<?", j.ObjectID, j.ID, []string{types.TypeDocumentProcess, types.TypeManualProcess}, []TaskState{TaskStatePending, TaskStateDispatched, TaskStateScheduled, TaskStateRetry}, j.Generation).Updates(map[string]any{"state": TaskStateCancelled, "cancel_requested": true, "delivery": gorm.Expr("delivery+1"), "updated_at": now}).Error; err != nil {
			return nil, err
		}
	}
	if _, bound := c.db.Statement.ConnPool.(gorm.TxCommitter); !bound {
		c.watchCommit(j.ID)
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
	r := c.db.WithContext(ctx).Model(&Job{}).Where("id=? AND state IN ?", id, []TaskState{TaskStatePending, TaskStateDispatched, TaskStateActive, TaskStateScheduled, TaskStateRetry}).Updates(map[string]any{"state": TaskStateCancelled, "cancel_requested": true, "delivery": gorm.Expr("delivery+1"), "updated_at": time.Now().UTC()})
	if r.Error != nil {
		return r.Error
	}
	if r.RowsAffected == 0 {
		var n int64
		if err := c.db.WithContext(ctx).Model(&Job{}).Where("id=?", id).Count(&n).Error; err != nil {
			return err
		}
		if n == 0 {
			return ErrTaskNotFound
		}
		return ErrTaskState
	}
	c.mu.Lock()
	cancel := c.active[id]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (c *Client) RunNow(ctx context.Context, id string) error {
	err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
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
		updates := map[string]interface{}{"state": TaskStatePending, "process_at": time.Now().UTC(), "updated_at": time.Now().UTC(), "cancel_requested": false, "dispatched_at": nil, "delivery": gorm.Expr("delivery+1")}
		return tx.Model(&j).Updates(updates).Error
	})
	if err == nil {
		c.Wake()
	}
	return err
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

// Start owns one database advisory lock solely to fence application processes.
// Redis delivers ready operations; PostgreSQL owns their schedule and outcome.
func (c *Client) Start(ctx context.Context, handler Handler, config Config) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		return errors.New("task worker is already started")
	}
	if c.db == nil || handler == nil {
		return errors.New("task worker requires database and handler")
	}
	c.observerMu.Lock()
	if c.closed {
		c.observerCtx, c.observerCancel = context.WithCancel(context.Background())
		c.closed = false
	}
	c.observerMu.Unlock()
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
	c.transport = asynq.NewClient(c.redisOpt)
	c.connection = types.QueueConnectionStatus{State: "active"}
	c.wg.Add(1)
	go func() { defer c.wg.Done(); c.supervise(root, handler, conn) }()
	return nil
}

func (c *Client) Close() error {
	c.mu.Lock()
	cancel := c.cancel
	c.observerMu.Lock()
	c.closed = true
	c.observerCancel()
	c.observerMu.Unlock()
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	c.observerWG.Wait()
	if cancel == nil {
		return nil
	}
	c.wg.Wait()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancel, c.workerCtx, c.lock = nil, nil, nil
	c.connection.State = "stopped"
	if c.transport != nil {
		_ = c.transport.Close()
		c.transport = nil
	}
	return nil
}

// Wake is called after commit. Losing a notification is harmless: the sweep
// always reads PostgreSQL, including transactions from other producers.
func (c *Client) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
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
