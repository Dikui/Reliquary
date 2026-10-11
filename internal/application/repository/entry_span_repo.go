package repository

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// EntrySpanRepository persists the per-attempt span tree used by the
// processing pipeline. Operations are deliberately narrow:
//
//   - Upsert covers Begin/End/Fail/Skip — every state transition routes
//     through the same write so the row stays internally consistent.
//   - NextAttempt allocates a new attempt for re-parses without touching
//     historical rows. Old attempts stay queryable for post-mortem.
//   - ListByAttempt is the only read path; the handler builds the tree
//     in memory rather than recursing through the DB.
type EntrySpanRepository interface {
	Upsert(ctx context.Context, row *types.EntryProcessingSpan) error
	NextAttempt(ctx context.Context, entryID string) (int, error)
	LatestAttempt(ctx context.Context, entryID string) (int, error)
	ListByAttempt(ctx context.Context, entryID string, attempt int) ([]types.EntryProcessingSpan, error)
	GetSpan(ctx context.Context, entryID string, attempt int, spanID string) (*types.EntryProcessingSpan, error)
	// CancelDescendants marks every descendant of a parent span as
	// "cancelled" with the given reason. Used by the tracker to
	// cascade an upstream failure across a stage's downstream subtree
	// without iterating in Go memory.
	CancelDescendants(ctx context.Context, entryID string, attempt int, parentSpanID, reason string) (int64, error)
	// CancelAllOpenSpans flips every non-terminal (pending/running) span
	// for (entryID, attempt) to "cancelled" in one statement,
	// regardless of tree position. Used by the user-cancel path where
	// fan-out stages (e.g. "多模态识别") flip themselves to done as soon
	// as they finish dispatching, while their async children are still
	// running — a tree walk that stops at terminal parents would miss
	// those orphan leaves.
	CancelAllOpenSpans(ctx context.Context, entryID string, attempt int, errorCode, reason string) (int64, error)
	// CancelOpenSpansByName flips pending/running rows with the given span
	// name for (entryID, attempt). Used before re-opening a subspan
	// after queue retry or server restart so the trace tree does not
	// accumulate duplicate postprocess.summary / question rows.
	CancelOpenSpansByName(ctx context.Context, entryID string, attempt int, name, errorCode, reason string) (int64, error)
}

type entrySpanRepository struct {
	db *gorm.DB
}

// NewEntrySpanRepository wires the GORM-backed implementation.
func NewEntrySpanRepository(db *gorm.DB) EntrySpanRepository {
	return &entrySpanRepository{db: db}
}

func (r *entrySpanRepository) Upsert(ctx context.Context, row *types.EntryProcessingSpan) error {
	if row == nil || row.EntryID == "" || row.SpanID == "" {
		return errors.New("entrySpanRepository.Upsert: entry_id and span_id required")
	}
	if row.Attempt == 0 {
		row.Attempt = 1
	}
	// Error fields may originate from third-party parser/model responses.
	// Normalize them at the persistence boundary so PostgreSQL and the
	// tracing API never receive malformed UTF-8.
	row.ErrorCode = utils.CleanInvalidUTF8(row.ErrorCode)
	row.ErrorMessage = utils.CleanInvalidUTF8(row.ErrorMessage)
	row.ErrorDetail = utils.CleanInvalidUTF8(row.ErrorDetail)
	// We let GORM populate created_at/updated_at via the autoCreate /
	// autoUpdate tags. ON CONFLICT updates only the fields that may
	// transition between calls — name/kind/parent are immutable once
	// set so we don't list them in DoUpdates (saves a few bytes per
	// write, and any mismatch indicates a programming error).
	//
	// CRITICAL: input / output / metadata are CONTENT fields that
	// individual call sites only fill when they have something to set.
	// EndSpan e.g. only sets `output`; if we always listed `input` in
	// DoUpdates, the End call would clobber the input set by Begin with
	// NULL. Same for metadata. Build the DoUpdates list dynamically and
	// skip these three columns when the incoming row has nothing to
	// write — so "no value" preserves the existing column instead of
	// nuking it.
	cols := []string{
		"status",
		"error_code",
		"error_message",
		"error_detail",
		"started_at",
		"finished_at",
		"duration_ms",
		"updated_at",
	}
	if row.Input != nil {
		cols = append(cols, "input")
	}
	if row.Output != nil {
		cols = append(cols, "output")
	}
	if row.Metadata != nil {
		cols = append(cols, "metadata")
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "entry_id"},
			{Name: "attempt"},
			{Name: "span_id"},
		},
		DoUpdates: clause.AssignmentColumns(cols),
	}).Create(row).Error
}

func (r *entrySpanRepository) NextAttempt(ctx context.Context, entryID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&types.EntryProcessingSpan{}).
		Where("entry_id = ?", entryID).
		Select("COALESCE(MAX(attempt), 0)").
		Row().Scan(&max)
	if err != nil {
		return 0, err
	}
	return max + 1, nil
}

func (r *entrySpanRepository) LatestAttempt(ctx context.Context, entryID string) (int, error) {
	var max int
	err := r.db.WithContext(ctx).Model(&types.EntryProcessingSpan{}).
		Where("entry_id = ?", entryID).
		Select("COALESCE(MAX(attempt), 0)").
		Row().Scan(&max)
	return max, err
}

func (r *entrySpanRepository) ListByAttempt(ctx context.Context, entryID string, attempt int) ([]types.EntryProcessingSpan, error) {
	if entryID == "" {
		return nil, nil
	}
	var rows []types.EntryProcessingSpan
	q := r.db.WithContext(ctx).Where("entry_id = ?", entryID)
	if attempt > 0 {
		q = q.Where("attempt = ?", attempt)
	}
	// id ASC keeps the natural insertion order — useful for stable
	// rendering of fan-out subspans (e.g. multimodal.image[0..N] in
	// the order they were enqueued).
	err := q.Order("id ASC").Find(&rows).Error
	return rows, err
}

func (r *entrySpanRepository) GetSpan(ctx context.Context, entryID string, attempt int, spanID string) (*types.EntryProcessingSpan, error) {
	var row types.EntryProcessingSpan
	err := r.db.WithContext(ctx).
		Where("entry_id = ? AND attempt = ? AND span_id = ?", entryID, attempt, spanID).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &row, nil
}

// CancelDescendants performs an iterative SQL walk: each level we update
// every row whose parent_span_id is in the previous level's span_id set,
// flipping pending/running rows to cancelled. We bail when a level adds
// zero rows (fixed point reached) or after a generous depth bound.
//
// The iterative path bounds traversal depth and is easy to exercise in fixtures.
func (r *entrySpanRepository) CancelDescendants(ctx context.Context, entryID string, attempt int, parentSpanID, reason string) (int64, error) {
	reason = utils.CleanInvalidUTF8(reason)
	frontier := []string{parentSpanID}
	var totalAffected int64
	for depth := 0; depth < 16 && len(frontier) > 0; depth++ {
		var nextFrontier []string
		// Find children of every span currently on the frontier
		// that are still in a non-terminal state — terminal rows
		// (done/failed/skipped/cancelled) are left as-is so the UI
		// can still see their original outcome.
		var children []types.EntryProcessingSpan
		err := r.db.WithContext(ctx).
			Where("entry_id = ? AND attempt = ? AND parent_span_id IN ? AND status IN ?",
				entryID, attempt, frontier,
				[]string{types.SpanStatusPending, types.SpanStatusRunning}).
			Find(&children).Error
		if err != nil {
			return totalAffected, err
		}
		if len(children) == 0 {
			break
		}
		ids := make([]string, 0, len(children))
		for _, c := range children {
			ids = append(ids, c.SpanID)
			nextFrontier = append(nextFrontier, c.SpanID)
		}
		res := r.db.WithContext(ctx).Model(&types.EntryProcessingSpan{}).
			Where("entry_id = ? AND attempt = ? AND span_id IN ?", entryID, attempt, ids).
			Updates(map[string]any{
				"status":        types.SpanStatusCancelled,
				"error_code":    "UPSTREAM_FAILED",
				"error_message": reason,
			})
		if res.Error != nil {
			return totalAffected, res.Error
		}
		totalAffected += res.RowsAffected
		frontier = nextFrontier
	}
	return totalAffected, nil
}

// CancelAllOpenSpans is the "abort the attempt" counterpart to
// CancelDescendants. It avoids the BFS entirely so spans whose parent
// is already terminal (typical for stage fan-outs that EndSpan as soon
// as they finish dispatching async work) still get flipped to cancelled.
// We deliberately do NOT touch finished_at / duration_ms here — the
// span row remains observable in the trace tree with its original
// start time and gets a cancelled status + reason, which is enough
// for the UI to drop the running-bar styling.
func (r *entrySpanRepository) CancelAllOpenSpans(
	ctx context.Context, entryID string, attempt int, errorCode, reason string,
) (int64, error) {
	errorCode = utils.CleanInvalidUTF8(errorCode)
	reason = utils.CleanInvalidUTF8(reason)
	now := time.Now()
	updates := map[string]any{
		"status":        types.SpanStatusCancelled,
		"error_code":    errorCode,
		"error_message": reason,
		"finished_at":   now,
		"updated_at":    now,
	}
	res := r.db.WithContext(ctx).Model(&types.EntryProcessingSpan{}).
		Where("entry_id = ? AND attempt = ? AND status IN ?",
			entryID, attempt,
			[]string{types.SpanStatusPending, types.SpanStatusRunning}).
		Updates(updates)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

func (r *entrySpanRepository) CancelOpenSpansByName(
	ctx context.Context, entryID string, attempt int, name, errorCode, reason string,
) (int64, error) {
	if entryID == "" || attempt <= 0 || name == "" {
		return 0, nil
	}
	errorCode = utils.CleanInvalidUTF8(errorCode)
	reason = utils.CleanInvalidUTF8(reason)
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&types.EntryProcessingSpan{}).
		Where("entry_id = ? AND attempt = ? AND name = ? AND status IN ?",
			entryID, attempt, name,
			[]string{types.SpanStatusPending, types.SpanStatusRunning}).
		Updates(map[string]any{
			"status":        types.SpanStatusCancelled,
			"error_code":    errorCode,
			"error_message": reason,
			"finished_at":   now,
			"updated_at":    now,
		})
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}
