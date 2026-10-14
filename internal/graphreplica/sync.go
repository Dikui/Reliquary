package graphreplica

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"reliquary/internal/application/graph"
	"reliquary/internal/queue"
	"reliquary/internal/types"
)

const TypeSync = "graph:sync"
const TypeCheck = "graph:check"
const TypeRebuild = "graph:rebuild"
const barrier = "reliquary:graphreplica:barrier"

type Service struct {
	db    *gorm.DB
	store *Store
	queue *queue.Client
}

func NewService(db *gorm.DB, store *Store, client *queue.Client) *Service {
	return &Service{db: db, store: store, queue: client}
}

type notification struct {
	SpaceID  uint64 `json:"space_id"`
	EntityID string `json:"entity_id"`
	JobID    string `json:"job_id"`
}

func lock(tx *gorm.DB, key string, shared bool) error {
	function := "pg_advisory_xact_lock"
	if shared {
		function += "_shared"
	}
	return tx.Exec("SELECT "+function+"(hashtextextended(?,0))", key).Error
}
func pendingIDs(tx *gorm.DB, space uint64, entity string) ([]string, error) {
	ids := []string{}
	err := tx.Model(&types.GraphPendingSync{}).Where("space_id=? AND entity_id=? AND completed_at IS NULL", space, entity).Order("id").Pluck("id", &ids).Error
	return ids, err
}

// Keep acknowledgement atomic without exceeding PostgreSQL's parameter limit
// after a long outage. The caller owns the transaction and the captured IDs.
func confirmPending(tx *gorm.DB, ids []string) error {
	at := time.Now().UTC()
	for start := 0; start < len(ids); start += 1000 {
		batch := ids[start:min(start+1000, len(ids))]
		if err := tx.Model(&types.GraphPendingSync{}).Where("id IN ? AND completed_at IS NULL", batch).Update("completed_at", at).Error; err != nil {
			return err
		}
	}
	return nil
}

// Sync ignores notification versions: it always rereads PG after serialization.
// PG and Neo4j cannot commit atomically; a lost PG acknowledgement repeats safely.
func (s *Service) Sync(ctx context.Context, space uint64, entity string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// Do not hold a business lock while discovering an offline replica.
	if err := s.store.ensure(ctx); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, barrier, true); err != nil {
			return err
		}
		if err := lock(tx, fmt.Sprintf("reliquary:graphreplica:entity:%d:%s", space, entity), false); err != nil {
			return err
		}
		if err := graph.Lock(tx, space); err != nil {
			return err
		}
		ids, err := pendingIDs(tx, space, entity)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		before, err := readPG(tx, space, entity)
		if err != nil {
			return err
		}
		// Capture a stable fingerprint before Hash sorts its slices.
		fingerprint := before.Hash()
		if err := s.store.apply(ctx, space, entity, before); err != nil {
			return err
		}
		after, err := readPG(tx, space, entity)
		if err != nil {
			return err
		}
		currentIDs, err := pendingIDs(tx, space, entity)
		if err != nil {
			return err
		}
		if after.Hash() != fingerprint || !reflect.DeepEqual(ids, currentIDs) {
			return errors.New("PostgreSQL graph changed during sync; retry current state")
		}
		return confirmPending(tx, ids)
	})
}
func (s *Service) HandleSync(ctx context.Context, t *queue.Task) error {
	var n notification
	if err := json.Unmarshal(t.Payload(), &n); err != nil {
		return fmt.Errorf("invalid graph notification: %w", queue.SkipRetry)
	}
	if n.SpaceID != types.SingleSpaceID || n.EntityID == "" {
		return fmt.Errorf("invalid graph entity: %w", queue.SkipRetry)
	}
	return s.Sync(ctx, n.SpaceID, n.EntityID)
}

type CheckResult struct {
	PGEdges    int    `json:"pg_edges"`
	Neo4jEdges int    `json:"neo4j_edges"`
	PGHash     string `json:"pg_hash"`
	Neo4jHash  string `json:"neo4j_hash"`
	Repaired   bool   `json:"repaired"`
	Method     string `json:"method"`
	Error      string `json:"error,omitempty"`
}

func (s *Service) allLocked(ctx context.Context, fn func(*gorm.DB) error) error {
	if err := s.store.ensure(ctx); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lock(tx, barrier, false); err != nil {
			return err
		}
		var spaces []uint64
		if err := tx.Table("spaces").Order("id").Pluck("id", &spaces).Error; err != nil {
			return err
		}
		for _, space := range spaces {
			if err := graph.Lock(tx, space); err != nil {
				return err
			}
		}
		return fn(tx)
	})
}
func (s *Service) audit(ctx context.Context, operation string, result CheckResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(&types.OperationLog{ID: uuid.NewString(), SpaceID: types.SingleSpaceID, Actor: "system", OperationType: operation, Before: types.JSON(`{}`), After: types.JSON(data), CreatedAt: time.Now().UTC()}).Error
}

// Check compares counts plus all node and edge fields, including versions and
// source counts, with one SHA-256 fingerprint. Small deployments trade O(V+E)
// memory and a PG graph write pause for deterministic daily coverage.
func (s *Service) Check(ctx context.Context) (CheckResult, error) {
	result := CheckResult{Method: "full SHA-256 of sorted global entities and relations"}
	err := s.allLocked(ctx, func(tx *gorm.DB) error {
		pg, err := readPG(tx, 0, "")
		if err != nil {
			return err
		}
		replica, err := s.store.snapshot(ctx)
		if err != nil {
			return err
		}
		result.PGEdges = len(pg.Edges)
		result.Neo4jEdges = len(replica.Edges)
		result.PGHash = pg.Hash()
		result.Neo4jHash = replica.Hash()
		if result.PGHash != result.Neo4jHash {
			if err := s.store.replace(ctx, pg); err != nil {
				return err
			}
			result.Repaired = true
			actual, err := s.store.snapshot(ctx)
			if err != nil {
				return err
			}
			if actual.Hash() != result.PGHash {
				return errors.New("Neo4j graph repair verification failed")
			}
		}
		return nil
	})
	if err != nil {
		result.Error = err.Error()
	}
	// Also keep failures visible when no Redis delivery can complete.
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if auditErr := s.audit(auditCtx, "graph_check", result); auditErr != nil {
		err = errors.Join(err, auditErr)
	}
	return result, err
}
func (s *Service) Rebuild(ctx context.Context) (CheckResult, error) {
	result := CheckResult{Method: "clear dedicated Neo4j database and rebuild current PG state"}
	err := s.allLocked(ctx, func(tx *gorm.DB) error {
		// Confirm only notifications seen under the rebuild barrier.
		var ids []string
		if err := tx.Model(&types.GraphPendingSync{}).Where("completed_at IS NULL").Order("id").Pluck("id", &ids).Error; err != nil {
			return err
		}
		pg, err := readPG(tx, 0, "")
		if err != nil {
			return err
		}
		if err := s.store.replace(ctx, pg); err != nil {
			return err
		}
		replica, err := s.store.snapshot(ctx)
		if err != nil {
			return err
		}
		result.PGEdges = len(pg.Edges)
		result.Neo4jEdges = len(replica.Edges)
		result.PGHash = pg.Hash()
		result.Neo4jHash = replica.Hash()
		result.Repaired = true
		if result.PGHash != result.Neo4jHash {
			return errors.New("Neo4j rebuild verification failed")
		}
		return confirmPending(tx, ids)
	})
	if err != nil {
		result.Error = err.Error()
	}
	auditCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if auditErr := s.audit(auditCtx, "graph_rebuild", result); auditErr != nil {
		err = errors.Join(err, auditErr)
	}
	return result, err
}
func (s *Service) HandleCheck(ctx context.Context, _ *queue.Task) error {
	_, err := s.Check(ctx)
	return err
}
func (s *Service) HandleRebuild(ctx context.Context, _ *queue.Task) error {
	_, err := s.Rebuild(ctx)
	return err
}
