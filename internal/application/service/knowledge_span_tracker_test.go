package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Span tracker tests use a real GORM-backed repo against isolated PostgreSQL
// tables. We do this instead of a stub repo because the cascade /
// LookupStage logic interacts non-trivially with the persistence layer
// (UPSERT, MAX(attempt), parent IN ...) — a stub would let regressions
// in those queries slip through.
//
// We DDL-define the spans table inline (same content as the repo test's
// spansTestDDL — kept duplicated rather than exported because a service
// test crossing into the repository test file's identifiers couples the
// two too tightly).
const spanTrackerTestDDL = `
CREATE TABLE IF NOT EXISTS knowledge_processing_spans (
    id              BIGSERIAL PRIMARY KEY,
    knowledge_id    VARCHAR(64) NOT NULL,
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
    UNIQUE (knowledge_id, attempt, span_id)
);
`

func setupSpanTrackerTest(t *testing.T) (SpanTracker, *gorm.DB) {
	t.Helper()
	db := pgtest.OpenFixture(t)
	require.NoError(t, db.Exec(spanTrackerTestDDL).Error)
	// Pass nil for the heartbeat db: these tests don't exercise
	// heartbeat side-effects (those are covered in the housekeeping
	// suite). Keeping it nil also avoids needing the knowledges
	// table just to validate span behaviour.
	repo := repository.NewKnowledgeSpanRepository(db)
	return NewSpanTracker(repo, nil), db
}

// TestSpanTracker_OpenAttempt_AllocatesFreshNumbers covers the contract
// that drives reparse history: each OpenAttempt must hand out a strictly
// increasing attempt number per knowledge, and previous attempts'
// rows must remain queryable (via a separate ?attempt=N navigation).
func TestSpanTracker_OpenAttempt_AllocatesFreshNumbers(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	root1, n1, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	require.NotNil(t, root1)
	assert.Equal(t, 1, n1)

	root2, n2, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	require.NotNil(t, root2)
	assert.Equal(t, 2, n2, "second OpenAttempt must allocate attempt 2")
	assert.NotEqual(t, root1.SpanID, root2.SpanID, "each attempt has its own root span ID")

	// Both roots must persist — a reparse must NOT erase the previous
	// attempt's history.
	var count int64
	require.NoError(t, db.Table("knowledge_processing_spans").
		Where("knowledge_id = ? AND kind = 'root'", "kid").
		Count(&count).Error)
	assert.Equal(t, int64(2), count, "previous attempt's root must remain after reparse")
}

// TestSpanTracker_FailSpan_CascadesDownstream verifies that failing a
// stage flips its dependent stages to "cancelled" so the UI shows a
// clear blast radius instead of orphan spinners. This is the central
// guarantee of the DAG model: a Chunking failure must cancel Embedding.
func TestSpanTracker_FailSpan_CascadesDownstream(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	require.Equal(t, 1, attempt)

	// Begin every stage so the cascade has something to cancel.
	docreader := tracker.BeginStage(ctx, "kid", attempt, types.StageDocReader, nil)
	tracker.EndSpan(ctx, docreader, nil)
	chunking := tracker.BeginStage(ctx, "kid", attempt, types.StageChunking, nil)
	embedding := tracker.BeginStage(ctx, "kid", attempt, types.StageEmbedding, nil)

	tracker.FailSpan(ctx, chunking, "CHUNKING_FAILED", "synthetic", errors.New("boom"))
	var rows []types.KnowledgeProcessingSpan
	require.NoError(t, db.Where("knowledge_id = ? AND attempt = ?", "kid", attempt).Find(&rows).Error)
	statusBy := map[string]string{}
	for _, r := range rows {
		statusBy[r.Name] = r.Status
	}
	assert.Equal(t, types.SpanStatusDone, statusBy[types.StageDocReader])
	assert.Equal(t, types.SpanStatusFailed, statusBy[types.StageChunking])
	assert.Equal(t, types.SpanStatusCancelled, statusBy[types.StageEmbedding])
	_ = embedding

}

// TestSpanTracker_LookupStage_FindsAcrossProcesses simulates the
// cross-process lookup: the upstream pipeline creates the processing
// stage span, then another process locates it by (kid, attempt, name)
// to attach a model-call subspan.
func TestSpanTracker_LookupStage_FindsAcrossProcesses(t *testing.T) {
	tracker, _ := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	processing := tracker.BeginStage(ctx, "kid", attempt, types.StageProcessing, nil)
	require.NotNil(t, processing)

	// Pretend we're a different process — the in-memory `starts`
	// cache is the same map here, but the cross-process semantics
	// don't depend on it; LookupStage hits the DB.
	found := tracker.LookupStage(ctx, "kid", attempt, types.StageProcessing)
	require.NotNil(t, found)
	assert.Equal(t, processing.SpanID, found.SpanID, "LookupStage must return the same span row")

	// A different stage must not be confused with processing.
	other := tracker.LookupStage(ctx, "kid", attempt, types.StageEmbedding)
	assert.Nil(t, other, "LookupStage(missing) must return nil")
}

func TestFitSpanName(t *testing.T) {
	short := "processing.call"
	if got := fitSpanName(short); got != short {
		t.Fatalf("short name should pass through, got %q", got)
	}

	// Regression: names in the 65–223 char window failed under VARCHAR(64)
	// but must pass through unchanged at VARCHAR(255).
	mid := "processing.entity_resolution[" + strings.Repeat("a", 100) + "]"
	if got := fitSpanName(mid); got != mid {
		t.Fatalf("mid-length entity resolution name should pass through, got %q", got)
	}

	// Use a synthetic overlong entity resolution span name (> varchar(255)).
	long := "processing.entity_resolution[" + strings.Repeat("a", 280) + "]"
	got := fitSpanName(long)
	if utf8.RuneCountInString(got) > maxSpanNameLen {
		t.Fatalf("fitted name runes=%d, want <= %d: %q", utf8.RuneCountInString(got), maxSpanNameLen, got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("fitted name must be valid UTF-8: %q", got)
	}
	if got == long {
		t.Fatalf("expected truncation, got unchanged %q", got)
	}
	if fitSpanName(long) != got {
		t.Fatal("fitSpanName must be deterministic")
	}

	other := "processing.entity_resolution[" + strings.Repeat("b", 280) + "]"
	if fitSpanName(other) == got {
		t.Fatalf("different long names must not collapse to the same fitted name")
	}

	// CJK entity names must truncate on rune boundaries, not byte boundaries.
	cjkLong := "processing.entity_resolution[" + strings.Repeat("中", 260) + "]"
	cjkGot := fitSpanName(cjkLong)
	if utf8.RuneCountInString(cjkGot) > maxSpanNameLen {
		t.Fatalf("CJK fitted name runes=%d, want <= %d", utf8.RuneCountInString(cjkGot), maxSpanNameLen)
	}
	if !utf8.ValidString(cjkGot) {
		t.Fatalf("CJK fitted name must be valid UTF-8: %q", cjkGot)
	}
}

// TestSpanTracker_BeginSubSpan_LongEntityResolutionSpanName verifies that
// entity resolution subspans persist even when an entity name pushes
// the span name past varchar(255).
func TestSpanTracker_BeginSubSpan_LongEntityResolutionSpanName(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	parent := tracker.BeginStage(ctx, "kid", attempt, types.StageProcessing, nil)
	require.NotNil(t, parent)

	rawName := "processing.entity_resolution[" + strings.Repeat("x", 280) + "]"
	sub := tracker.BeginSubSpan(ctx, parent, rawName, types.SpanKindSubSpan, types.JSONMap{
		"entity_name": strings.Repeat("x", 280),
	})
	require.NotNil(t, sub)
	require.LessOrEqual(t, utf8.RuneCountInString(sub.Name), maxSpanNameLen)

	var count int64
	require.NoError(t, db.Table("knowledge_processing_spans").
		Where("knowledge_id = ? AND name = ?", "kid", sub.Name).
		Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// TestSpanTracker_LookupSpanByName_FitsLongName verifies cross-process
// callers can look up an entity resolution subspan using the raw overlong name.
func TestSpanTracker_LookupSpanByName_FitsLongName(t *testing.T) {
	tracker, _ := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	parent := tracker.BeginStage(ctx, "kid", attempt, types.StageProcessing, nil)
	require.NotNil(t, parent)

	rawName := "processing.entity_resolution[" + strings.Repeat("y", 280) + "]"
	created := tracker.BeginSubSpan(ctx, parent, rawName, types.SpanKindSubSpan, nil)
	require.NotNil(t, created)

	found := tracker.LookupSpanByName(ctx, "kid", attempt, rawName)
	require.NotNil(t, found, "LookupSpanByName must normalize the raw name")
	assert.Equal(t, created.SpanID, found.SpanID)
	assert.Equal(t, created.Name, found.Name)
}

// TestSpanTracker_BeginSubSpan_HangsUnderParent confirms model-call
// subspans reference the parent stage's span_id, preserving
// the structural invariant the buildSpanTree handler walks.
func TestSpanTracker_BeginSubSpan_HangsUnderParent(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)
	parent := tracker.BeginStage(ctx, "kid", attempt, types.StageProcessing, nil)
	require.NotNil(t, parent)

	sub := tracker.BeginSubSpan(ctx, parent, "processing.call", types.SpanKindGeneration, types.JSONMap{
		"title": "Example article",
	})
	require.NotNil(t, sub)

	type row struct {
		Name, Kind, ParentSpanID string
	}
	var rows []row
	require.NoError(t, db.Table("knowledge_processing_spans").
		Select("name, kind, parent_span_id").
		Where("knowledge_id = ? AND name = ?", "kid", "processing.call").
		Find(&rows).Error)
	require.Len(t, rows, 1)
	assert.Equal(t, types.SpanKindGeneration, rows[0].Kind)
	assert.Equal(t, parent.SpanID, rows[0].ParentSpanID, "subspan must reference parent stage's span_id")
}

// TestSpanTracker_BeginStage_ReentryIsIdempotent guarantees that a second
// BeginStage call for the same (kid, attempt, stage) reuses the existing
// span row instead of inserting a duplicate. Without this, an asynq retry
// or any code path that begins a stage twice would produce two timeline
// segments for the same stage, and LookupStage would resolve to whichever
// row sorts first — both regressions the original implementation had.
func TestSpanTracker_BeginStage_ReentryIsIdempotent(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)

	first := tracker.BeginStage(ctx, "kid", attempt, types.StageDocReader, types.JSONMap{"pages": 1})
	require.NotNil(t, first)
	// Simulate an intermediate End so the row is in a terminal state when
	// the re-entry happens (mirrors retry-after-failure ordering).
	tracker.FailSpan(ctx, first, "TEST", "first failure", errors.New("boom"))

	second := tracker.BeginStage(ctx, "kid", attempt, types.StageDocReader, types.JSONMap{"pages": 2})
	require.NotNil(t, second)
	assert.Equal(t, first.SpanID, second.SpanID,
		"re-entry must reuse the existing stage span_id")

	type row struct {
		SpanID, Status string
	}
	var rows []row
	require.NoError(t, db.Table("knowledge_processing_spans").
		Select("span_id, status").
		Where("knowledge_id = ? AND attempt = ? AND name = ?", "kid", attempt, types.StageDocReader).
		Find(&rows).Error)
	require.Len(t, rows, 1, "exactly one row per (knowledge, attempt, stage)")
	assert.Equal(t, types.SpanStatusRunning, rows[0].Status,
		"row must transition back to running after re-entry")
}

// TestSpanTracker_FailSpan_CascadesDependentSubspans verifies that when a
// chunking failure flips Embedding to "cancelled" (sibling cascade),
// embedding's already-running subspan (e.g. embedding.batch[0]) is ALSO
// cancelled. Without this, the UI rendered a cancelled stage with an
// orphan running batch hanging underneath.
func TestSpanTracker_FailSpan_CascadesDependentSubspans(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()

	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)

	chunking := tracker.BeginStage(ctx, "kid", attempt, types.StageChunking, nil)
	embedding := tracker.BeginStage(ctx, "kid", attempt, types.StageEmbedding, nil)
	require.NotNil(t, embedding)
	// Subspan attached to the dependent (sibling) stage that's about to
	// be cascade-cancelled.
	batch := tracker.BeginSubSpan(ctx, embedding, "embedding.batch[0]", types.SpanKindGeneration, nil)
	require.NotNil(t, batch)

	tracker.FailSpan(ctx, chunking, "CHUNKING_FAILED", "synthetic", errors.New("boom"))

	type row struct {
		Name, Status string
	}
	var rows []row
	require.NoError(t, db.Table("knowledge_processing_spans").
		Select("name, status").
		Where("knowledge_id = ?", "kid").
		Find(&rows).Error)
	statusBy := map[string]string{}
	for _, r := range rows {
		statusBy[r.Name] = r.Status
	}
	assert.Equal(t, types.SpanStatusCancelled, statusBy[types.StageEmbedding],
		"dependent stage cascades to cancelled")
	assert.Equal(t, types.SpanStatusCancelled, statusBy["embedding.batch[0]"],
		"subspan under the cascaded stage must also be cancelled")
}

// TestProcessingSubspan_AttachesUnderClosedProcessingStage verifies that
// looking up a closed processing stage preserves its parent relationship
// when a caller records additional entity resolution trace detail.
func TestProcessingSubspan_AttachesUnderClosedProcessingStage(t *testing.T) {
	tracker, db := setupSpanTrackerTest(t)
	ctx := context.Background()
	repo := repository.NewKnowledgeSpanRepository(db)

	// Set up the parent attempt with a closed processing stage.
	// LookupStage must still recover the persisted parent handle.
	_, attempt, err := tracker.OpenAttempt(ctx, "kid")
	require.NoError(t, err)

	processing := tracker.BeginStage(ctx, "kid", attempt, types.StageProcessing, types.JSONMap{
		"title": "Example article",
	})
	require.NotNil(t, processing)
	tracker.EndSpan(ctx, processing, types.JSONMap{"content_type": "event"})

	// Recover the persisted stage and record an entity resolution subspan.
	parent := tracker.LookupStage(ctx, "kid", attempt, types.StageProcessing)
	require.NotNil(t, parent, "lookup must succeed even after EndSpan closed the parent")
	assert.Equal(t, types.StageProcessing, parent.Name)
	assert.Equal(t, types.SpanKindStage, parent.Kind)

	entitySpan := tracker.BeginSubSpan(ctx, parent, "processing.entity_resolution", types.SpanKindSubSpan,
		types.JSONMap{"entity_count": 2})
	require.NotNil(t, entitySpan)
	assert.Equal(t, parent.SpanID, entitySpan.ParentSpanID,
		"subspan must hang off the processing stage's span_id")
	assert.Equal(t, types.SpanKindSubSpan, entitySpan.Kind)

	tracker.EndSpan(ctx, entitySpan, types.JSONMap{
		"entities_resolved": 2,
	})

	// Verify the row landed under the right parent with the right name.
	rows, err := repo.ListByAttempt(ctx, "kid", attempt)
	require.NoError(t, err)
	var found *types.KnowledgeProcessingSpan
	for i := range rows {
		if rows[i].Name == "processing.entity_resolution" {
			cp := rows[i]
			found = &cp
			break
		}
	}
	require.NotNil(t, found, "processing subspan row must persist")
	assert.Equal(t, parent.SpanID, found.ParentSpanID,
		"persisted parent_span_id matches LookupStage result")
	assert.Equal(t, types.SpanStatusDone, found.Status)
	assert.NotNil(t, found.Output, "EndSpan must record the output map")
}

// TestProcessingSubspan_MissingParentFallsThrough verifies that absent
// attempts and processing stage rows return nil. Callers can skip span
// recording when no parent exists and continue normal processing.
func TestProcessingSubspan_MissingParentFallsThrough(t *testing.T) {
	tracker, _ := setupSpanTrackerTest(t)
	ctx := context.Background()

	// No OpenAttempt → no rows for kid. LookupStage must return nil.
	parent := tracker.LookupStage(ctx, "kid-without-attempt", 7, types.StageProcessing)
	assert.Nil(t, parent, "missing parent attempt yields nil, not an error")

	// Open an attempt but never begin processing. Lookup must still return nil.
	_, attempt, err := tracker.OpenAttempt(ctx, "kid-no-processing")
	require.NoError(t, err)
	parent = tracker.LookupStage(ctx, "kid-no-processing", attempt, types.StageProcessing)
	assert.Nil(t, parent, "missing processing stage row yields nil")
}
