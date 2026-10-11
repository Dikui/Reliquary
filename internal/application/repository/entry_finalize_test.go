package repository

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// entriesTestDDL mirrors the columns of `entries` that
// SetFinalizing / FinalizeSubtask / UpdateEntry actually read or write.
// We inline the DDL (instead of AutoMigrate) so the schema is explicit,
// and we include pending_subtasks_count from the baseline migration plus the
// processing/finalizing/completed columns the helpers care about.
const entriesTestDDL = `
CREATE TABLE IF NOT EXISTS entries (
    id VARCHAR(36) PRIMARY KEY,
    space_id INTEGER NOT NULL,
    knowledge_base_id VARCHAR(36) NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT '',
    title VARCHAR(255) NOT NULL DEFAULT '',
    description TEXT,
    content_type VARCHAR(16),
    content_type_uncertain BOOLEAN NOT NULL DEFAULT false,
    processing_review_status VARCHAR(16) NOT NULL DEFAULT '',
    processing_review_content_type VARCHAR(16) NOT NULL DEFAULT '',
    processing_review_reason TEXT NOT NULL DEFAULT '',
    source_unverified BOOLEAN NOT NULL DEFAULT false,
    processing_mode VARCHAR(16) NOT NULL DEFAULT 'full',
    source_kind VARCHAR(16) NOT NULL DEFAULT 'article',
    vault_path TEXT NOT NULL DEFAULT '',
    vault_body TEXT NOT NULL DEFAULT '',
    vault_original_path TEXT NOT NULL DEFAULT '',
    starred BOOLEAN NOT NULL DEFAULT false,
    collect_reason TEXT NOT NULL DEFAULT '',
    agent_summary TEXT NOT NULL DEFAULT '',
    agent_tags JSONB NOT NULL DEFAULT '[]'::jsonb,
    source_name TEXT NOT NULL DEFAULT '',
    structured_summary JSONB,
    source VARCHAR(2048) NOT NULL DEFAULT '',
    parse_status VARCHAR(50) NOT NULL DEFAULT 'unprocessed',
    enable_status VARCHAR(50) NOT NULL DEFAULT 'enabled',
    embedding_model_id VARCHAR(64),
    file_name VARCHAR(255),
    file_type VARCHAR(50),
    file_size BIGINT,
    file_path TEXT,
    file_hash VARCHAR(64),
    normalized_url TEXT NOT NULL DEFAULT '',
    content_hash CHAR(64) NOT NULL DEFAULT '',
    content_sim_hash BIGINT NOT NULL DEFAULT 0,
    content_rune_count INTEGER NOT NULL DEFAULT 0,
    storage_size BIGINT NOT NULL DEFAULT 0,
    metadata JSONB,
    summary_status VARCHAR(32) DEFAULT 'none',
    channel VARCHAR(50) NOT NULL DEFAULT 'web',
    pending_subtasks_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    processed_at TIMESTAMPTZ,
    error_message TEXT,
    deleted_at TIMESTAMPTZ
);
`

// setupEntryTestDB creates an isolated PostgreSQL entry fixture.
func setupEntryTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(entriesTestDDL).Error)
	return db
}

// insertProcessingEntry seeds a row in `processing` state ready for a
// SetFinalizing transition.
func insertProcessingEntry(t *testing.T, db *gorm.DB) string {
	t.Helper()
	id := uuid.New().String()
	require.NoError(t, db.Exec(`
		INSERT INTO entries (id, space_id, knowledge_base_id, type, title, source, parse_status, pending_subtasks_count)
		VALUES (?, 1, ?, 'document', 'finalize-test', 'manual', 'processing', 0)
	`, id, uuid.New().String()).Error)
	return id
}

// reloadEntryRow returns the parse_status and pending_subtasks_count of
// a row directly via raw SQL — bypasses any GORM hook noise.
func reloadEntryRow(t *testing.T, db *gorm.DB, id string) (status string, count int) {
	t.Helper()
	row := db.Raw(`SELECT parse_status, pending_subtasks_count FROM entries WHERE id = ?`, id).Row()
	require.NoError(t, row.Scan(&status, &count))
	return status, count
}

func reloadEntryErrorMessage(t *testing.T, db *gorm.DB, id string) string {
	t.Helper()
	var msg string
	require.NoError(t, db.Raw(`SELECT COALESCE(error_message, '') FROM entries WHERE id = ?`, id).Scan(&msg).Error)
	return msg
}

func TestEntryRepository_UpdateEntryColumnsSanitizesErrorMessage(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db)
	id := insertProcessingEntry(t, db)
	invalid := "parse failed " + string([]byte{0xef, 0xbc, 0x2e})

	require.NoError(t, repo.UpdateEntryColumns(context.Background(), id, map[string]interface{}{
		"error_message": invalid,
	}))

	got := reloadEntryErrorMessage(t, db, id)
	if !utf8.ValidString(got) {
		t.Fatalf("persisted error_message is invalid UTF-8: % x", []byte(got))
	}
	assert.Equal(t, "parse failed .", got)
}

func insertEntryWithStatus(t *testing.T, db *gorm.DB, status string, deleted bool) string {
	t.Helper()
	id := uuid.New().String()
	deletedAt := interface{}(nil)
	if deleted {
		deletedAt = "2026-06-16 12:00:00"
	}
	require.NoError(t, db.Exec(`
		INSERT INTO entries (id, space_id, knowledge_base_id, type, title, source, parse_status, deleted_at)
		VALUES (?, 1, ?, 'document', 'delete-test', 'manual', ?, ?)
	`, id, uuid.New().String(), status, deletedAt).Error)
	return id
}

// TestFinalizeSubtask_Concurrent_ExactlyOnePromote spawns N goroutines that
// each call FinalizeSubtask after SetFinalizing(N), and asserts:
//   - the counter ends at zero,
//   - parse_status is "completed",
//   - exactly one caller observed promoted=true.
//
// This is the behavior the original "stuck pending_subtasks_count" bug
// violated: clobbered counters meant some callers saw a non-zero value
// after the true count had reached zero, and none of them promoted.
func TestFinalizeSubtask_Concurrent_ExactlyOnePromote(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	const n = 20
	id := insertProcessingEntry(t, db)

	transitioned, err := repo.SetFinalizing(ctx, id, n)
	require.NoError(t, err)
	require.True(t, transitioned, "SetFinalizing should transition processing -> finalizing")

	var promoteWins atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, promoted, ferr := repo.FinalizeSubtask(ctx, id)
			if ferr != nil {
				t.Errorf("FinalizeSubtask: %v", ferr)
				return
			}
			if promoted {
				promoteWins.Add(1)
			}
		}()
	}
	wg.Wait()

	assert.Equal(t, int32(1), promoteWins.Load(),
		"exactly one caller must observe promoted=true even under concurrent decrements")

	status, count := reloadEntryRow(t, db, id)
	assert.Equal(t, types.ParseStatusCompleted, status)
	assert.Equal(t, 0, count)
}

// TestFinalizeSubtask_PartialDecrement_StaysFinalizing verifies the row
// remains in "finalizing" with the expected residual count when fewer
// callers decrement than were seeded — the promote guard must not fire
// early.
func TestFinalizeSubtask_PartialDecrement_StaysFinalizing(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	id := insertProcessingEntry(t, db)
	_, err := repo.SetFinalizing(ctx, id, 3)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		_, promoted, ferr := repo.FinalizeSubtask(ctx, id)
		require.NoError(t, ferr)
		assert.False(t, promoted, "promote must not fire while count > 0")
	}

	status, count := reloadEntryRow(t, db, id)
	assert.Equal(t, types.ParseStatusFinalizing, status)
	assert.Equal(t, 1, count)
}

// TestFinalizeSubtask_DecrementClampedAtZero verifies the safety-net
// clamp on the decrement: extra calls past the seeded count must not
// underflow pending_subtasks_count below zero. (Reconciliation's
// shortfall-release loop relies on this.)
func TestFinalizeSubtask_DecrementClampedAtZero(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	id := insertProcessingEntry(t, db)
	_, err := repo.SetFinalizing(ctx, id, 1)
	require.NoError(t, err)

	// First decrement drains the only slot and promotes.
	_, promoted, err := repo.FinalizeSubtask(ctx, id)
	require.NoError(t, err)
	assert.True(t, promoted)

	// Subsequent decrements must be no-ops, not underflow.
	for i := 0; i < 3; i++ {
		_, promoted, err := repo.FinalizeSubtask(ctx, id)
		require.NoError(t, err)
		assert.False(t, promoted)
	}

	status, count := reloadEntryRow(t, db, id)
	assert.Equal(t, types.ParseStatusCompleted, status)
	assert.Equal(t, 0, count, "pending_subtasks_count must be clamped at zero")
}

// TestSetFinalizingAndFinalizeSubtask_ClearStaleErrorMessage is the
// regression test for stale error_message: a row that failed once keeps
// error_message set, and both entering finalizing (a new attempt) and
// promoting to completed (a successful finish) must clear it so the UI
// no longer shows an outdated failure.
func TestSetFinalizingAndFinalizeSubtask_ClearStaleErrorMessage(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	id := insertProcessingEntry(t, db)
	require.NoError(t, db.Exec(
		`UPDATE entries SET error_message = ? WHERE id = ?`,
		"Task interrupted due to application restart",
		id,
	).Error)

	transitioned, err := repo.SetFinalizing(ctx, id, 1)
	require.NoError(t, err)
	require.True(t, transitioned)
	assert.Empty(t, reloadEntryErrorMessage(t, db, id),
		"SetFinalizing must clear error_message from the previous attempt")

	require.NoError(t, db.Exec(
		`UPDATE entries SET error_message = ? WHERE id = ?`,
		"stale finalizing failure",
		id,
	).Error)
	_, promoted, err := repo.FinalizeSubtask(ctx, id)
	require.NoError(t, err)
	require.True(t, promoted)
	assert.Empty(t, reloadEntryErrorMessage(t, db, id),
		"promotion to completed must clear error_message")
}

// TestUpdateEntry_DoesNotClobberPendingCounter is the regression test
// for the original bug: a full-row Save with a stale in-memory counter
// must not write that stale value back, otherwise it overwrites atomic
// decrements made by other goroutines.
//
// Sequence:
//  1. SetFinalizing(N=5) -> counter=5
//  2. Caller A loads the row (sees counter=5)
//  3. FinalizeSubtask runs concurrently and decrements to counter=4
//  4. Caller A modifies an unrelated field (Title) and calls UpdateEntry
//  5. Counter must still be 4 (not 5).
func TestUpdateEntry_DoesNotClobberPendingCounter(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	id := insertProcessingEntry(t, db)
	_, err := repo.SetFinalizing(ctx, id, 5)
	require.NoError(t, err)

	// Step 2: caller A snapshots the row with counter=5 in memory.
	loaded, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, 5, loaded.PendingSubtasksCount)

	// Step 3: an enrichment subtask decrements concurrently.
	_, _, err = repo.FinalizeSubtask(ctx, id)
	require.NoError(t, err)

	// Step 4: caller A persists an unrelated change. The in-memory copy
	// of PendingSubtasksCount is the STALE 5 — Save must NOT write it.
	loaded.Title = "renamed-after-stale-load"
	require.NoError(t, repo.UpdateEntry(ctx, loaded))

	// Step 5: the live counter is still 4, not clobbered back to 5.
	status, count := reloadEntryRow(t, db, id)
	assert.Equal(t, types.ParseStatusFinalizing, status)
	assert.Equal(t, 4, count,
		"UpdateEntry must omit pending_subtasks_count so a stale in-memory value cannot clobber atomic decrements")

	// And the unrelated field WAS persisted.
	reloaded, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	assert.Equal(t, "renamed-after-stale-load", reloaded.Title)
}

// TestUpdateEntry_PendingCounterOmittedOnReset verifies the inverse
// case the reparse paths rely on: even setting PendingSubtasksCount=0
// in memory and calling UpdateEntry does NOT persist that value.
// Reparse must use UpdateEntryColumn explicitly.
func TestUpdateEntry_PendingCounterOmittedOnReset(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	id := insertProcessingEntry(t, db)
	_, err := repo.SetFinalizing(ctx, id, 7)
	require.NoError(t, err)

	loaded, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)

	// Caller tries to reset the counter via Save — this must be a no-op
	// for that column. The dedicated UpdateEntryColumn is the only
	// path that actually writes pending_subtasks_count.
	loaded.PendingSubtasksCount = 0
	require.NoError(t, repo.UpdateEntry(ctx, loaded))

	_, count := reloadEntryRow(t, db, id)
	assert.Equal(t, 7, count, "UpdateEntry with PendingSubtasksCount=0 must NOT persist the reset")

	// The explicit column write IS the supported path.
	require.NoError(t, repo.UpdateEntryColumn(ctx, id, "pending_subtasks_count", 0))
	_, count = reloadEntryRow(t, db, id)
	assert.Equal(t, 0, count)
}

func TestUpdateEntryPreservesCollectionReasonFromConcurrentIntake(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewEntryRepository(db)
	ctx := context.Background()
	id := insertProcessingEntry(t, db)
	stale, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	require.Empty(t, stale.CollectReason)
	require.NoError(t, repo.UpdateEntryColumn(ctx, id, "collect_reason", "Preserve the measured result"))
	stale.Description = "Processing finished after the user supplied a collection reason."
	require.NoError(t, repo.UpdateEntry(ctx, stale))
	current, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, "Preserve the measured result", current.CollectReason)
	require.Equal(t, stale.Description, current.Description)
	stale.Description = "A later batch update also preserves the user judgment."
	require.NoError(t, repo.UpdateEntryBatch(ctx, []*types.Entry{stale}))
	current, err = repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	require.Equal(t, "Preserve the measured result", current.CollectReason)
	require.Equal(t, stale.Description, current.Description)
}

func TestUpdateEntryPreservesProcessingReviewDecisionAndUnverifiedSource(t *testing.T) {
	db := pgtest.Open(t)
	repo := NewEntryRepository(db)
	ctx := context.Background()
	id := insertProcessingEntry(t, db)
	stale, err := repo.GetEntryByID(ctx, 1, id)
	require.NoError(t, err)
	require.NoError(t, repo.UpdateEntryColumns(ctx, id, map[string]any{
		"source_unverified":              true,
		"content_type_uncertain":         true,
		"processing_review_status":       types.ProcessingReviewIgnored,
		"processing_review_content_type": types.ContentTypeTutorial,
		"processing_review_reason":       "User retained the original source despite the uncertain type.",
	}))
	for _, batch := range []bool{false, true} {
		stale.Description = "An unrelated background update completed."
		if batch {
			require.NoError(t, repo.UpdateEntryBatch(ctx, []*types.Entry{stale}))
		} else {
			require.NoError(t, repo.UpdateEntry(ctx, stale))
		}
		current, err := repo.GetEntryByID(ctx, 1, id)
		require.NoError(t, err)
		require.True(t, current.SourceUnverified)
		require.True(t, current.ContentTypeUncertain)
		require.Equal(t, types.ProcessingReviewIgnored, current.ProcessingReviewStatus)
		require.Equal(t, types.ContentTypeTutorial, current.ProcessingReviewContentType)
		require.Contains(t, current.ProcessingReviewReason, "User retained")
		require.Equal(t, stale.Description, current.Description)
	}
}

func TestUpdateActiveDeletingEntryColumns_GuardsStateAndSoftDelete(t *testing.T) {
	db := setupEntryTestDB(t)
	repo := NewEntryRepository(db).(*entryRepository)
	ctx := context.Background()

	activeDeletingID := insertEntryWithStatus(t, db, types.ParseStatusDeleting, false)
	activeCompletedID := insertEntryWithStatus(t, db, types.ParseStatusCompleted, false)
	deletedDeletingID := insertEntryWithStatus(t, db, types.ParseStatusDeleting, true)

	updated, err := repo.UpdateActiveDeletingEntryColumns(ctx, activeDeletingID, map[string]interface{}{
		"parse_status":  types.ParseStatusFailed,
		"error_message": "delete task exhausted retries",
	})
	require.NoError(t, err)
	assert.True(t, updated)

	updated, err = repo.UpdateActiveDeletingEntryColumns(ctx, activeCompletedID, map[string]interface{}{
		"parse_status": types.ParseStatusFailed,
	})
	require.NoError(t, err)
	assert.False(t, updated)

	updated, err = repo.UpdateActiveDeletingEntryColumns(ctx, deletedDeletingID, map[string]interface{}{
		"parse_status": types.ParseStatusFailed,
	})
	require.NoError(t, err)
	assert.False(t, updated)

	status, _ := reloadEntryRow(t, db, activeDeletingID)
	assert.Equal(t, types.ParseStatusFailed, status)
	status, _ = reloadEntryRow(t, db, activeCompletedID)
	assert.Equal(t, types.ParseStatusCompleted, status)
	status, _ = reloadEntryRow(t, db, deletedDeletingID)
	assert.Equal(t, types.ParseStatusDeleting, status)
}
