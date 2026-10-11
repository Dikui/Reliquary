package repository

import (
	"context"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strconv"
)

// taskDeadLetterRepository implements interfaces.TaskDeadLetterRepository.
type taskDeadLetterRepository struct {
	db *gorm.DB
}

// NewTaskDeadLetterRepository constructs a GORM-backed implementation.
func NewTaskDeadLetterRepository(db *gorm.DB) interfaces.TaskDeadLetterRepository {
	return &taskDeadLetterRepository{db: db}
}

// Insert records one dead letter. Best-effort caller: the queue
// middleware swallows the error so a failed insert never masks the
// underlying task error.
func (r *taskDeadLetterRepository) Insert(ctx context.Context, dl *types.TaskDeadLetter) error {
	if dl == nil {
		return errors.New("task dead letters: nil entry")
	}
	if dl.TaskType == "" {
		return errors.New("task dead letters: task_type is required")
	}
	if dl.Scope == "" {
		dl.Scope = types.TaskScopeUnknown
	}
	if len(dl.Payload) == 0 {
		dl.Payload = []byte("{}")
	}
	return r.db.WithContext(ctx).Create(dl).Error
}

// ListByScope returns dead letters for (scope, scope_id) newest-first
// with a stringified id cursor. `limit` is clamped to [1, 200]. Empty
// nextCursor signals the tail.
func (r *taskDeadLetterRepository) ListByScope(
	ctx context.Context,
	scope, scopeID, cursor string,
	limit int,
) ([]*types.TaskDeadLetter, string, error) {
	if scope == "" || scopeID == "" {
		return nil, "", errors.New("task dead letters: scope and scope_id are required")
	}
	return r.list(ctx, cursor, limit, func(q *gorm.DB) *gorm.DB {
		return q.Where("scope = ? AND scope_id = ?", scope, scopeID)
	})
}

// ListByTaskType returns dead letters for the given task_type
// newest-first with a stringified id cursor. Same clamping rules.
func (r *taskDeadLetterRepository) ListByTaskType(
	ctx context.Context,
	taskType, cursor string,
	limit int,
) ([]*types.TaskDeadLetter, string, error) {
	if taskType == "" {
		return nil, "", errors.New("task dead letters: task_type is required")
	}
	return r.list(ctx, cursor, limit, func(q *gorm.DB) *gorm.DB {
		return q.Where("task_type = ?", taskType)
	})
}

// list is the shared cursor pagination implementation, parametrized by
// the caller-supplied filter.
func (r *taskDeadLetterRepository) list(
	ctx context.Context,
	cursor string,
	limit int,
	filter func(*gorm.DB) *gorm.DB,
) ([]*types.TaskDeadLetter, string, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	q := r.db.WithContext(ctx).Order("id DESC").Limit(limit)
	q = filter(q)

	if cursor != "" {
		cursorID, err := strconv.ParseInt(cursor, 10, 64)
		if err != nil {
			return nil, "", fmt.Errorf("invalid cursor %q: %w", cursor, err)
		}
		q = q.Where("id < ?", cursorID)
	}

	var rows []*types.TaskDeadLetter
	if err := q.Find(&rows).Error; err != nil {
		return nil, "", err
	}

	nextCursor := ""
	if len(rows) == limit {
		nextCursor = strconv.FormatInt(rows[len(rows)-1].ID, 10)
	}
	return rows, nextCursor, nil
}

// DeleteByID drops a single dead letter row. Returns nil even if the
// row is already gone — operators issuing concurrent deletes shouldn't
// see spurious errors.
func (r *taskDeadLetterRepository) DeleteByID(ctx context.Context, id int64) error {
	return r.db.WithContext(ctx).
		Where("id = ?", id).
		Delete(&types.TaskDeadLetter{}).Error
}
