package repository

import (
	"context"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// spansTestDDL mirrors the PostgreSQL columns used from the baseline migration.
const spansTestDDL = `
CREATE TABLE IF NOT EXISTS entry_processing_spans (
    id              BIGSERIAL PRIMARY KEY,
    entry_id    VARCHAR(64) NOT NULL,
    attempt         INTEGER     NOT NULL DEFAULT 1,
    span_id         VARCHAR(64) NOT NULL,
    parent_span_id  VARCHAR(64),
    name            VARCHAR(255) NOT NULL,
    kind            VARCHAR(16) NOT NULL,
    status          VARCHAR(16) NOT NULL,
    input           JSONB,
    output          JSONB,
    metadata        JSONB,
    error_code      VARCHAR(64),
    error_message   TEXT,
    error_detail    TEXT,
    started_at      TIMESTAMPTZ,
    finished_at     TIMESTAMPTZ,
    duration_ms     BIGINT,
    created_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at      TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (entry_id, attempt, span_id)
);
`

func setupSpanTestRepo(t *testing.T) (EntrySpanRepository, *gorm.DB) {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(spansTestDDL).Error)
	return NewEntrySpanRepository(db), db
}

// TestEntrySpanRepo_UpsertAndList covers the round-trip: a Begin
// followed by an End for the same (kid, attempt, span_id) updates the
// existing row in place, leaving exactly one row queryable by
// ListByAttempt with the latest state.
func TestEntrySpanRepo_UpsertAndList(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-1"
	now := time.Now()
	row := &types.EntryProcessingSpan{
		EntryID:   kid,
		Attempt:   1,
		SpanID:    "span-A",
		Name:      types.StageDocReader,
		Kind:      types.SpanKindStage,
		Status:    types.SpanStatusRunning,
		StartedAt: &now,
	}
	require.NoError(t, repo.Upsert(ctx, row))

	// Second Upsert with same (kid, attempt, span_id) flips status and
	// sets finished_at — must overwrite, not insert a duplicate.
	finished := now.Add(2 * time.Second)
	row.Status = types.SpanStatusDone
	row.FinishedAt = &finished
	row.DurationMs = 2000
	require.NoError(t, repo.Upsert(ctx, row))

	rows, err := repo.ListByAttempt(ctx, kid, 1)
	require.NoError(t, err)
	require.Len(t, rows, 1, "Upsert must replace, not append")
	assert.Equal(t, types.SpanStatusDone, rows[0].Status)
	assert.Equal(t, int64(2000), rows[0].DurationMs)
}

func TestEntrySpanRepo_UpsertSanitizesErrorFields(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	now := time.Now()
	invalid := "prefix " + string([]byte{0xef, 0xbc, 0x2e}) + " suffix"

	require.NoError(t, repo.Upsert(context.Background(), &types.EntryProcessingSpan{
		EntryID:      "kid-invalid-utf8",
		Attempt:      1,
		SpanID:       "span-invalid",
		Name:         types.StageDocReader,
		Kind:         types.SpanKindStage,
		Status:       types.SpanStatusFailed,
		ErrorCode:    invalid,
		ErrorMessage: invalid,
		ErrorDetail:  invalid,
		StartedAt:    &now,
	}))

	rows, err := repo.ListByAttempt(context.Background(), "kid-invalid-utf8", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	for _, value := range []string{rows[0].ErrorCode, rows[0].ErrorMessage, rows[0].ErrorDetail} {
		if !utf8.ValidString(value) {
			t.Fatalf("persisted error field is invalid UTF-8: % x", []byte(value))
		}
		if value != "prefix . suffix" {
			t.Fatalf("persisted error field = %q, want %q", value, "prefix . suffix")
		}
	}
}

// TestEntrySpanRepo_NextAttempt confirms that NextAttempt allocates
// a fresh number per entry, isolating reparse history. Critical
// because the API layer renders attempt history by this number.
func TestEntrySpanRepo_NextAttempt(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-2"

	a, err := repo.NextAttempt(ctx, kid)
	require.NoError(t, err)
	assert.Equal(t, 1, a, "first NextAttempt for fresh entry must be 1")

	now := time.Now()
	require.NoError(t, repo.Upsert(ctx, &types.EntryProcessingSpan{
		EntryID: kid, Attempt: 1, SpanID: "root-1",
		Name: "knowledge_processing", Kind: types.SpanKindRoot,
		Status: types.SpanStatusRunning, StartedAt: &now,
	}))
	a, err = repo.NextAttempt(ctx, kid)
	require.NoError(t, err)
	assert.Equal(t, 2, a, "after one attempt exists, NextAttempt must be 2")

	// Cross-entry isolation: a different kid stays at 1.
	other, err := repo.NextAttempt(ctx, "kid-other")
	require.NoError(t, err)
	assert.Equal(t, 1, other, "NextAttempt must scope to the entry_id")
}

// TestEntrySpanRepo_CancelDescendants verifies the cascade walk:
// failing a stage cancels every pending/running descendant in its
// subtree, while terminal states (done/skipped/failed) are left intact.
func TestEntrySpanRepo_CancelDescendants(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-3"
	now := time.Now()

	// Tree: chunking → embedding (running) → batch[0] (running)
	//                → multimodal (running) → image[0] (done)
	for _, r := range []*types.EntryProcessingSpan{
		{EntryID: kid, Attempt: 1, SpanID: "chunking", Name: types.StageChunking, Kind: types.SpanKindStage, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "embedding", ParentSpanID: "chunking", Name: types.StageEmbedding, Kind: types.SpanKindStage, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "batch0", ParentSpanID: "embedding", Name: "embedding.batch[0]", Kind: types.SpanKindGeneration, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "multimodal", ParentSpanID: "chunking", Name: types.StageProcessing, Kind: types.SpanKindStage, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "image0", ParentSpanID: "multimodal", Name: "multimodal.image[0]", Kind: types.SpanKindGeneration, Status: types.SpanStatusDone, StartedAt: &now},
	} {
		require.NoError(t, repo.Upsert(ctx, r))
	}

	affected, err := repo.CancelDescendants(ctx, kid, 1, "chunking", "test reason")
	require.NoError(t, err)
	// Expected cancellations: embedding, batch0, multimodal (3 rows).
	// The done image0 is terminal and left alone.
	assert.Equal(t, int64(3), affected, "must cancel exactly the 3 pending/running descendants")

	rows, err := repo.ListByAttempt(ctx, kid, 1)
	require.NoError(t, err)
	statusBy := map[string]string{}
	for _, r := range rows {
		statusBy[r.SpanID] = r.Status
	}
	assert.Equal(t, types.SpanStatusRunning, statusBy["chunking"], "the failed span itself stays untouched (FailSpan layer flips it)")
	assert.Equal(t, types.SpanStatusCancelled, statusBy["embedding"])
	assert.Equal(t, types.SpanStatusCancelled, statusBy["batch0"])
	assert.Equal(t, types.SpanStatusCancelled, statusBy["multimodal"])
	assert.Equal(t, types.SpanStatusDone, statusBy["image0"], "terminal states must not be touched")
}

func TestEntrySpanRepo_CancelOpenSpansByName(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-supersede"
	now := time.Now()

	for _, r := range []*types.EntryProcessingSpan{
		{EntryID: kid, Attempt: 1, SpanID: "sum-old", Name: "postprocess.summary", Kind: types.SpanKindSubSpan, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "sum-done", Name: "postprocess.summary", Kind: types.SpanKindSubSpan, Status: types.SpanStatusDone, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "q-old", Name: "postprocess.question", Kind: types.SpanKindSubSpan, Status: types.SpanStatusRunning, StartedAt: &now},
	} {
		require.NoError(t, repo.Upsert(ctx, r))
	}

	affected, err := repo.CancelOpenSpansByName(ctx, kid, 1, "postprocess.summary", "TASK_SUPERSEDED", "retry")
	require.NoError(t, err)
	assert.Equal(t, int64(1), affected)

	rows, err := repo.ListByAttempt(ctx, kid, 1)
	require.NoError(t, err)
	statusBy := map[string]string{}
	for _, r := range rows {
		statusBy[r.SpanID] = r.Status
	}
	assert.Equal(t, types.SpanStatusCancelled, statusBy["sum-old"])
	assert.Equal(t, types.SpanStatusDone, statusBy["sum-done"])
	assert.Equal(t, types.SpanStatusRunning, statusBy["q-old"])
}

func TestEntrySpanRepo_CancelPathsSanitizeErrorFields(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-cancel-invalid-utf8"
	now := time.Now()
	for _, r := range []*types.EntryProcessingSpan{
		{EntryID: kid, Attempt: 1, SpanID: "root", Name: "root", Kind: types.SpanKindRoot, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "descendant", ParentSpanID: "root", Name: "descendant", Kind: types.SpanKindSubSpan, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "bulk", Name: "bulk", Kind: types.SpanKindSubSpan, Status: types.SpanStatusRunning, StartedAt: &now},
		{EntryID: kid, Attempt: 1, SpanID: "named", Name: "named", Kind: types.SpanKindSubSpan, Status: types.SpanStatusRunning, StartedAt: &now},
	} {
		require.NoError(t, repo.Upsert(ctx, r))
	}

	invalidCode := "CODE " + string([]byte{0xef, 0xbc, 0x2e})
	invalidReason := "reason " + string([]byte{0xef, 0xbc, 0x2e})
	_, err := repo.CancelOpenSpansByName(ctx, kid, 1, "named", invalidCode, invalidReason)
	require.NoError(t, err)
	_, err = repo.CancelDescendants(ctx, kid, 1, "root", invalidReason)
	require.NoError(t, err)
	_, err = repo.CancelAllOpenSpans(ctx, kid, 1, invalidCode, invalidReason)
	require.NoError(t, err)

	rows, err := repo.ListByAttempt(ctx, kid, 1)
	require.NoError(t, err)
	byID := make(map[string]types.EntryProcessingSpan, len(rows))
	for _, row := range rows {
		byID[row.SpanID] = row
	}
	assert.Equal(t, "UPSTREAM_FAILED", byID["descendant"].ErrorCode)
	assert.Equal(t, "reason .", byID["descendant"].ErrorMessage)
	for _, id := range []string{"root", "bulk", "named"} {
		assert.Equal(t, "CODE .", byID[id].ErrorCode, id)
		assert.Equal(t, "reason .", byID[id].ErrorMessage, id)
		assert.True(t, utf8.ValidString(byID[id].ErrorCode), id)
		assert.True(t, utf8.ValidString(byID[id].ErrorMessage), id)
	}
}

// TestEntrySpanRepo_ListAttemptIsolation guarantees that different
// attempts of the same entry stay queryable independently — the
// foundation for the "show history" UI navigation (?attempt=N).
func TestEntrySpanRepo_ListAttemptIsolation(t *testing.T) {
	repo, _ := setupSpanTestRepo(t)
	ctx := context.Background()
	kid := "kid-history"
	now := time.Now()

	for _, attempt := range []int{1, 2} {
		require.NoError(t, repo.Upsert(ctx, &types.EntryProcessingSpan{
			EntryID: kid, Attempt: attempt, SpanID: "root",
			Name: "knowledge_processing", Kind: types.SpanKindRoot,
			Status: types.SpanStatusDone, StartedAt: &now,
		}))
	}
	a1, err := repo.ListByAttempt(ctx, kid, 1)
	require.NoError(t, err)
	require.Len(t, a1, 1)
	a2, err := repo.ListByAttempt(ctx, kid, 2)
	require.NoError(t, err)
	require.Len(t, a2, 1)

	all, err := repo.ListByAttempt(ctx, kid, 0)
	require.NoError(t, err)
	assert.Len(t, all, 2, "attempt=0 returns all attempts (used by housekeeping)")
}
