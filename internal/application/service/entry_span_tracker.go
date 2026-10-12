// Package service: span tracker.
//
// SpanTracker is the pipeline-facing facade for recording per-attempt
// progress trees. The root, stage, subspan and generation
// records describe the pipeline for the frontend progress timeline.
//
// Lifecycle:
//
//	attempt := tracker.OpenAttempt(ctx, entryID)
//	  // creates the root span; every subsequent Begin* call uses this attempt
//
//	stage := tracker.BeginStage(ctx, entryID, attempt, types.StageDocReader, input)
//	  // ...do work...
//	tracker.EndSpan(ctx, stage, output)         // success
//	tracker.FailSpan(ctx, stage, code, msg, err) // error
//	tracker.SkipSpan(ctx, stage, reason)        // intentionally not run
//
//	sub := tracker.BeginSubSpan(ctx, parentSpan, "processing.call", types.SpanKindGeneration, input)
//	  // ...
//
// All operations are best-effort: a DB error is logged and swallowed so a
// tracker hiccup never breaks the parsing pipeline. Entry.parse_status
// remains the authoritative source of truth for completion.
package service

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// maxSpanNameLen matches entry_processing_spans.name (varchar(255)).
const maxSpanNameLen = 255

// fitSpanName ensures a span name fits the DB column. Long source names
// receive an 8-hex hash suffix to keep concurrent subspans distinct. Truncation is rune-aware to
// match PostgreSQL VARCHAR(255) character semantics and avoid splitting
// multi-byte UTF-8 sequences.
func fitSpanName(name string) string {
	runes := []rune(name)
	if len(runes) <= maxSpanNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(name))
	suffix := fmt.Sprintf("~%x", sum[:4])
	suffixRunes := []rune(suffix)
	keep := maxSpanNameLen - len(suffixRunes)
	if keep < 1 {
		if len(suffixRunes) > maxSpanNameLen {
			return string(suffixRunes[:maxSpanNameLen])
		}
		return suffix
	}
	return string(runes[:keep]) + suffix
}

// Span is the in-memory handle the pipeline holds while a stage / subspan
// is executing. It carries enough context for End/Fail/Skip to write back
// without re-querying the DB. Returned (and required) from every Begin*.
type Span struct {
	EntryID      string
	Attempt      int
	SpanID       string
	ParentSpanID string
	Name         string
	Kind         string
	Status       string
	StartedAt    time.Time
}

// SpanTracker is the only public surface — kept as an interface so tests
// can swap in a no-op without spinning up a database.
type SpanTracker interface {
	// OpenAttempt creates a new root span for (entryID,
	// nextAttempt) and returns its number plus the root *Span. Call
	// at the start of a parse / reparse, before any other Begin*.
	OpenAttempt(ctx context.Context, entryID string) (root *Span, attempt int, err error)

	// LatestAttempt returns the highest attempt number recorded for
	// the entry, or 0 if it's never been parsed. Used by the API
	// layer to default to "show me the most recent run".
	LatestAttempt(ctx context.Context, entryID string) int

	// BeginStage starts one of the canonical stages. Looks up the
	// root span for (kid, attempt) — caller passes attempt to make
	// the wiring explicit and let cross-process workers join an
	// existing attempt without new repo lookups.
	BeginStage(ctx context.Context, entryID string, attempt int, stage string, input types.JSONMap) *Span

	// BeginSubSpan creates a child span under parent. parent may be a
	// stage span (for processing.call / embedding.batch[i]) or
	// another subspan. kind is "subspan" or "generation".
	BeginSubSpan(ctx context.Context, parent *Span, name, kind string, input types.JSONMap) *Span

	// EndSpan marks span as done with optional output. Safe with nil.
	EndSpan(ctx context.Context, span *Span, output types.JSONMap)

	// FailSpan marks span as failed and cascade-cancels its
	// descendants. errorDetail (a Go error) is recorded verbatim in
	// error_detail (truncated to 8 KB) for admin views.
	FailSpan(ctx context.Context, span *Span, errorCode, errorMessage string, errorDetail error)

	// SkipSpan marks an intentionally not-run span (e.g. embeddings
	// for a summary-only article). Distinct from cancelled: skipped is
	// "we chose not to" while cancelled is "an upstream broke".
	SkipSpan(ctx context.Context, span *Span, reason string)

	// LookupStage returns the stage's *Span for an attempt so workers
	// can attach subspans to a stage recorded by another process.
	LookupStage(ctx context.Context, entryID string, attempt int, stage string) *Span

	// LookupSpanByName returns the first span of any kind matching name
	// for (entryID, attempt), letting a fan-out worker attach its subspan
	// under a grouping span created earlier by the orchestrator. Returns
	// nil when no such span exists (caller should fall back to the stage).
	LookupSpanByName(ctx context.Context, entryID string, attempt int, name string) *Span

	// FinalizeAttempt closes the root span for (entryID, attempt)
	// with the given terminal status (done | failed). Idempotent:
	// re-closing an already-terminal root is a no-op so callers from
	// multiple paths (success orchestrator, dead-letter handler,
	// housekeeping) can fire without coordination. status defaults to
	// done; output/error are written verbatim.
	FinalizeAttempt(ctx context.Context, entryID string, attempt int, status string,
		output types.JSONMap, errorCode, errorMessage string)

	// AbortAttempt cascade-cancels every still-running descendant of the
	// attempt's root span and then closes the root as cancelled. Used by
	// the user-initiated cancel path so the trace viewer doesn't leave
	// stranded subspans (model calls, embedding batches)
	// looking like they're still in flight forever after the user
	// stopped the parse. Idempotent.
	AbortAttempt(ctx context.Context, entryID string, attempt int, errorCode, errorMessage, reason string)
}

type spanTracker struct {
	repo repository.EntrySpanRepository
	// db is held purely for the heartbeat side-channel: root and stage
	// state transitions poke entry.updated_at so the housekeeping
	// sweep can tell "actively running long stage" from "abandoned".
	// nil-safe — when missing (test harness) the heartbeat is skipped.
	db *gorm.DB

	// startsMu guards the in-process duration cache. Cross-process
	// workers won't find their parent's start here — that's fine,
	// duration_ms falls back to (FinishedAt - row.StartedAt) computed
	// at write time when the cache misses.
	startsMu sync.Mutex
	starts   map[string]time.Time // span_id → started_at
}

// NewSpanTracker constructs the GORM-backed tracker. A nil repo collapses
// to a no-op so test harnesses don't need to spin up a database. The db
// is optional too: it's used only for the housekeeping heartbeat (see
// touchEntryHeartbeat) and a nil db just disables that side-channel.
func NewSpanTracker(repo repository.EntrySpanRepository, db *gorm.DB) SpanTracker {
	if repo == nil {
		return noopSpanTracker{}
	}
	return &spanTracker{
		repo:   repo,
		db:     db,
		starts: make(map[string]time.Time),
	}
}

// touchEntryHeartbeat advances entry.updated_at to the current
// wall-clock so the housekeeping sweep treats this row as actively
// progressing. Called on root / stage span transitions only. Subspan and
// generation transitions skip this side-channel because:
//
//   - The spans table itself is updated on every transition, and the
//     housekeeping sweep already reads MAX(spans.updated_at) per
//     entry, so subspan progress is observable without poking the
//     parent row.
//   - Embedding batches would produce extra UPDATEs on the same parent
//     row for every Begin/End and retry, increasing row contention
//     during concurrent uploads.
//
// Best-effort. We deliberately do NOT bump status here: the parse_status
// column remains under the pipeline's control. Only the timestamp gets
// nudged, which is exactly what housekeeping reads as the fallback.
func (t *spanTracker) touchEntryHeartbeat(ctx context.Context, entryID, kind string) {
	if t.db == nil || entryID == "" {
		return
	}
	// Subspan / generation transitions are observable through the spans
	// table directly; skip the parent-row UPDATE to avoid write
	// amplification on fan-out workloads.
	if kind != types.SpanKindRoot && kind != types.SpanKindStage {
		return
	}
	if err := t.db.WithContext(ctx).Model(&types.Entry{}).
		Where("id = ?", entryID).
		Update("updated_at", time.Now()).Error; err != nil {
		// Don't log every failure — heartbeat is best-effort and
		// noisy logs would drown out real errors. Single line at
		// warn level is enough for ops to spot a chronic outage.
		logger.Warnf(ctx, "[SpanTracker] heartbeat update failed kid=%s: %v", entryID, err)
	}
}

func newSpanID() string {
	// Stripping the dashes saves 4 bytes per row — JSON parsers don't
	// care, and operators paste these into queries where a
	// hex-only ID is friendlier.
	return strings.ReplaceAll(uuid.NewString(), "-", "")
}

func (t *spanTracker) recordStart(spanID string, at time.Time) {
	t.startsMu.Lock()
	t.starts[spanID] = at
	t.startsMu.Unlock()
}

func (t *spanTracker) takeStart(spanID string) (time.Time, bool) {
	t.startsMu.Lock()
	defer t.startsMu.Unlock()
	v, ok := t.starts[spanID]
	if ok {
		delete(t.starts, spanID)
	}
	return v, ok
}

func (t *spanTracker) OpenAttempt(ctx context.Context, entryID string) (*Span, int, error) {
	attempt, err := t.repo.NextAttempt(ctx, entryID)
	if err != nil {
		return nil, 0, err
	}
	now := time.Now()
	rootID := newSpanID()
	row := &types.EntryProcessingSpan{
		EntryID:   entryID,
		Attempt:   attempt,
		SpanID:    rootID,
		Name:      "knowledge_processing",
		Kind:      types.SpanKindRoot,
		Status:    types.SpanStatusRunning,
		StartedAt: &now,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] OpenAttempt failed kid=%s: %v", entryID, err)
		return nil, attempt, err
	}
	t.recordStart(rootID, now)
	t.touchEntryHeartbeat(ctx, entryID, types.SpanKindRoot)
	return &Span{
		EntryID:   entryID,
		Attempt:   attempt,
		SpanID:    rootID,
		Name:      "knowledge_processing",
		Kind:      types.SpanKindRoot,
		Status:    types.SpanStatusRunning,
		StartedAt: now,
	}, attempt, nil
}

func (t *spanTracker) LatestAttempt(ctx context.Context, entryID string) int {
	n, err := t.repo.LatestAttempt(ctx, entryID)
	if err != nil {
		logger.Warnf(ctx, "[SpanTracker] LatestAttempt failed kid=%s: %v", entryID, err)
		return 0
	}
	return n
}

func (t *spanTracker) BeginStage(ctx context.Context, entryID string, attempt int, stage string, input types.JSONMap) *Span {
	if entryID == "" || stage == "" {
		return nil
	}
	// Find root span — we need its span_id as parent for stage rows. We
	// also reuse this scan to detect an existing row for the same stage
	// name in this attempt: re-entry (queue retry, double-call from
	// adjacent code paths) MUST NOT create a second row, otherwise the
	// timeline shows two segments for the same stage and LookupStage
	// becomes ambiguous.
	rows, err := t.repo.ListByAttempt(ctx, entryID, attempt)
	if err != nil {
		logger.Warnf(ctx, "[SpanTracker] BeginStage list failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		return nil
	}
	var (
		rootID   string
		existing *types.EntryProcessingSpan
	)
	for i := range rows {
		r := rows[i]
		if r.Kind == types.SpanKindRoot && rootID == "" {
			rootID = r.SpanID
		}
		if r.Kind == types.SpanKindStage && r.Name == stage {
			cp := r
			existing = &cp
		}
	}
	if rootID == "" {
		// Pipeline started before tracker was wired (legacy data,
		// or the OpenAttempt repo write failed). Synthesize a
		// rootless stage so we still record SOMETHING.
		logger.Warnf(ctx, "[SpanTracker] BeginStage: no root for kid=%s attempt=%d, recording rootless",
			entryID, attempt)
	}
	now := time.Now()
	// Re-entry path: keep the original span_id so any subspan that
	// already references it stays attached. Reset state to running and
	// refresh started_at; clear terminal-only fields so the row reads
	// cleanly as "running again". Output/error fields go through
	// Upsert's DoUpdates list, which only writes the columns we set —
	// any nil JSONMap / empty string explicitly clears the column.
	if existing != nil {
		row := &types.EntryProcessingSpan{
			EntryID:      existing.EntryID,
			Attempt:      existing.Attempt,
			SpanID:       existing.SpanID,
			ParentSpanID: existing.ParentSpanID,
			Name:         existing.Name,
			Kind:         existing.Kind,
			Status:       types.SpanStatusRunning,
			Input:        input,
			Output:       nil,
			StartedAt:    &now,
			FinishedAt:   nil,
			DurationMs:   0,
		}
		if err := t.repo.Upsert(ctx, row); err != nil {
			logger.Warnf(ctx, "[SpanTracker] BeginStage re-enter failed kid=%s stage=%s: %v",
				entryID, stage, err)
			return nil
		}
		t.recordStart(existing.SpanID, now)
		t.touchEntryHeartbeat(ctx, entryID, types.SpanKindStage)
		return &Span{
			EntryID:      existing.EntryID,
			Attempt:      existing.Attempt,
			SpanID:       existing.SpanID,
			ParentSpanID: existing.ParentSpanID,
			Name:         existing.Name,
			Kind:         existing.Kind,
			Status:       types.SpanStatusRunning,
			StartedAt:    now,
		}
	}
	id := newSpanID()
	row := &types.EntryProcessingSpan{
		EntryID:      entryID,
		Attempt:      attempt,
		SpanID:       id,
		ParentSpanID: rootID,
		Name:         stage,
		Kind:         types.SpanKindStage,
		Status:       types.SpanStatusRunning,
		Input:        input,
		StartedAt:    &now,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] BeginStage failed kid=%s stage=%s: %v",
			entryID, stage, err)
		return nil
	}
	t.recordStart(id, now)
	t.touchEntryHeartbeat(ctx, entryID, types.SpanKindStage)
	return &Span{
		EntryID:      entryID,
		Attempt:      attempt,
		SpanID:       id,
		ParentSpanID: rootID,
		Name:         stage,
		Kind:         types.SpanKindStage,
		Status:       types.SpanStatusRunning,
		StartedAt:    now,
	}
}

func (t *spanTracker) BeginSubSpan(ctx context.Context, parent *Span, name, kind string, input types.JSONMap) *Span {
	if parent == nil || name == "" {
		return nil
	}
	name = fitSpanName(name)
	if kind != types.SpanKindGeneration && kind != types.SpanKindSubSpan {
		kind = types.SpanKindSubSpan
	}
	// PostgreSQL queue retry / server restart can re-run the same handler while the
	// previous invocation's span is still status=running (worker died
	// without EndSpan). Cancel same-name open rows so the UI shows one
	// logical subspan per (attempt, name) instead of duplicate stripes.
	if _, err := t.repo.CancelOpenSpansByName(ctx, parent.EntryID, parent.Attempt, name,
		"TASK_SUPERSEDED", "superseded by a new run of the same subtask"); err != nil {
		logger.Warnf(ctx, "[SpanTracker] supersede %s before BeginSubSpan failed: %v", name, err)
	}
	now := time.Now()
	id := newSpanID()
	row := &types.EntryProcessingSpan{
		EntryID:      parent.EntryID,
		Attempt:      parent.Attempt,
		SpanID:       id,
		ParentSpanID: parent.SpanID,
		Name:         name,
		Kind:         kind,
		Status:       types.SpanStatusRunning,
		Input:        input,
		StartedAt:    &now,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] BeginSubSpan failed parent=%s name=%s: %v",
			parent.SpanID, name, err)
		return nil
	}
	t.recordStart(id, now)
	t.touchEntryHeartbeat(ctx, parent.EntryID, kind)
	return &Span{
		EntryID:      parent.EntryID,
		Attempt:      parent.Attempt,
		SpanID:       id,
		ParentSpanID: parent.SpanID,
		Name:         name,
		Kind:         kind,
		Status:       types.SpanStatusRunning,
		StartedAt:    now,
	}
}

func (t *spanTracker) EndSpan(ctx context.Context, span *Span, output types.JSONMap) {
	if span == nil {
		return
	}
	if calls := types.ModelCallCountsFromContext(ctx); calls != nil {
		if output == nil {
			output = types.JSONMap{}
		}
		output["model_calls"] = calls
	}
	now := time.Now()
	dur := durationSince(t, span, now)
	row := &types.EntryProcessingSpan{
		EntryID:      span.EntryID,
		Attempt:      span.Attempt,
		SpanID:       span.SpanID,
		ParentSpanID: span.ParentSpanID,
		Name:         span.Name,
		Kind:         span.Kind,
		Status:       types.SpanStatusDone,
		Output:       output,
		StartedAt:    &span.StartedAt,
		FinishedAt:   &now,
		DurationMs:   dur,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] EndSpan failed span=%s: %v", span.SpanID, err)
	}
	t.touchEntryHeartbeat(ctx, span.EntryID, span.Kind)
}

func (t *spanTracker) FailSpan(ctx context.Context, span *Span, errorCode, errorMessage string, errorDetail error) {
	if span == nil {
		return
	}
	now := time.Now()
	dur := durationSince(t, span, now)
	detail := ""
	if errorDetail != nil {
		detail = errorDetail.Error()
		if len(detail) > 8192 {
			detail = detail[:8192]
		}
	}
	if len(errorMessage) > 1024 {
		errorMessage = errorMessage[:1024]
	}
	row := &types.EntryProcessingSpan{
		EntryID:      span.EntryID,
		Attempt:      span.Attempt,
		SpanID:       span.SpanID,
		ParentSpanID: span.ParentSpanID,
		Name:         span.Name,
		Kind:         span.Kind,
		Status:       types.SpanStatusFailed,
		ErrorCode:    strings.TrimSpace(errorCode),
		ErrorMessage: errorMessage,
		ErrorDetail:  detail,
		StartedAt:    &span.StartedAt,
		FinishedAt:   &now,
		DurationMs:   dur,
	}
	if calls := types.ModelCallCountsFromContext(ctx); calls != nil {
		row.Output = types.JSONMap{"model_calls": calls}
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] FailSpan failed span=%s: %v", span.SpanID, err)
	}
	// Cascade: anything downstream of this span gets cancelled. The
	// reason string is what the UI surfaces under each cancelled
	// child's tooltip — keep it short and human.
	reason := "upstream " + span.Name + " failed"
	if errorCode != "" {
		reason = reason + " (" + errorCode + ")"
	}
	if _, err := t.repo.CancelDescendants(ctx, span.EntryID, span.Attempt, span.SpanID, reason); err != nil {
		logger.Warnf(ctx, "[SpanTracker] cancel descendants failed span=%s: %v", span.SpanID, err)
	}
	// For STAGE failures, also cascade to dependent stages declared
	// in StageDependencies (those are siblings, not descendants).
	if span.Kind == types.SpanKindStage {
		t.cascadeDependentStages(ctx, span, reason)
		// Any failure in a MAIN pipeline stage means the attempt is
		// done — the parse cannot succeed past this point. Close the
		// root span as failed so the UI doesn't show "进行中" forever.
		if isMainPipelineStage(span.Name) {
			t.FinalizeAttempt(ctx, span.EntryID, span.Attempt,
				types.SpanStatusFailed, nil, errorCode, errorMessage)
		}
	}
	t.touchEntryHeartbeat(ctx, span.EntryID, span.Kind)
}

func (t *spanTracker) SkipSpan(ctx context.Context, span *Span, reason string) {
	if span == nil {
		return
	}
	now := time.Now()
	row := &types.EntryProcessingSpan{
		EntryID:      span.EntryID,
		Attempt:      span.Attempt,
		SpanID:       span.SpanID,
		ParentSpanID: span.ParentSpanID,
		Name:         span.Name,
		Kind:         span.Kind,
		Status:       types.SpanStatusSkipped,
		ErrorMessage: reason,
		StartedAt:    &span.StartedAt,
		FinishedAt:   &now,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] SkipSpan failed span=%s: %v", span.SpanID, err)
	}
	t.touchEntryHeartbeat(ctx, span.EntryID, span.Kind)
}

func (t *spanTracker) LookupStage(ctx context.Context, entryID string, attempt int, stage string) *Span {
	rows, err := t.repo.ListByAttempt(ctx, entryID, attempt)
	if err != nil {
		logger.Warnf(ctx, "[SpanTracker] LookupStage list failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		return nil
	}
	for i := range rows {
		r := rows[i]
		if r.Kind != types.SpanKindStage || r.Name != stage {
			continue
		}
		started := time.Time{}
		if r.StartedAt != nil {
			started = *r.StartedAt
		}
		return &Span{
			EntryID:      r.EntryID,
			Attempt:      r.Attempt,
			SpanID:       r.SpanID,
			ParentSpanID: r.ParentSpanID,
			Name:         r.Name,
			Kind:         r.Kind,
			Status:       r.Status,
			StartedAt:    started,
		}
	}
	return nil
}

func (t *spanTracker) LookupSpanByName(ctx context.Context, entryID string, attempt int, name string) *Span {
	if name == "" || entryID == "" || attempt <= 0 {
		return nil
	}
	name = fitSpanName(name)
	rows, err := t.repo.ListByAttempt(ctx, entryID, attempt)
	if err != nil {
		logger.Warnf(ctx, "[SpanTracker] LookupSpanByName list failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		return nil
	}
	for i := range rows {
		r := rows[i]
		if r.Name != name {
			continue
		}
		started := time.Time{}
		if r.StartedAt != nil {
			started = *r.StartedAt
		}
		return &Span{
			EntryID:      r.EntryID,
			Attempt:      r.Attempt,
			SpanID:       r.SpanID,
			ParentSpanID: r.ParentSpanID,
			Name:         r.Name,
			Kind:         r.Kind,
			Status:       r.Status,
			StartedAt:    started,
		}
	}
	return nil
}

// cascadeDependentStages flips downstream STAGE rows to "cancelled" using
// types.StageDependencies. Without this, a Chunking failure leaves
// Embedding as "pending" forever, even though it cannot
// possibly run. After flipping a dependent stage we ALSO cascade-cancel
// any subspan/generation that already attached to it (e.g. an embedding
// batch that started before the chunking failure was observed) — without
// this second walk those subspans would remain in pending/running and
// surface as orphan spinners under a cancelled parent.
func (t *spanTracker) cascadeDependentStages(ctx context.Context, failedStage *Span, reason string) {
	rows, err := t.repo.ListByAttempt(ctx, failedStage.EntryID, failedStage.Attempt)
	if err != nil {
		return
	}
	dependents := stagesDependingOn(failedStage.Name)
	if len(dependents) == 0 {
		return
	}
	now := time.Now()
	for _, row := range rows {
		if row.Kind != types.SpanKindStage {
			continue
		}
		if row.Status != types.SpanStatusPending && row.Status != types.SpanStatusRunning {
			continue
		}
		if !contains(dependents, row.Name) {
			continue
		}
		updated := row // copy
		updated.Status = types.SpanStatusCancelled
		updated.ErrorCode = "UPSTREAM_FAILED"
		updated.ErrorMessage = reason
		updated.FinishedAt = &now
		if err := t.repo.Upsert(ctx, &updated); err != nil {
			logger.Warnf(ctx, "[SpanTracker] cascade dependent stage %s: %v", row.Name, err)
			continue
		}
		// Recurse into the cascaded stage's own subtree so any
		// in-flight subspan/generation is cancelled too. The
		// repo-level walk is iterative and cheap (small fan-out).
		if _, err := t.repo.CancelDescendants(ctx, row.EntryID, row.Attempt, row.SpanID, reason); err != nil {
			logger.Warnf(ctx, "[SpanTracker] cascade descendants of dependent %s: %v", row.Name, err)
		}
	}
}

// stagesDependingOn returns the transitive closure of stages that have
// `stage` as an upstream dependency (direct or indirect). Computed by
// reverse-walking StageDependencies; the result is bounded to four since
// AllStages has four members, so a naive O(N²) walk is fine.
func stagesDependingOn(stage string) []string {
	var out []string
	seen := map[string]bool{}
	frontier := []string{stage}
	for len(frontier) > 0 {
		var next []string
		for _, candidate := range types.AllStages {
			if seen[candidate] {
				continue
			}
			deps := types.StageDependencies[candidate]
			for _, d := range deps {
				if contains(frontier, d) {
					seen[candidate] = true
					out = append(out, candidate)
					next = append(next, candidate)
					break
				}
			}
		}
		frontier = next
	}
	return out
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

// isMainPipelineStage reports whether stage is one of the four pipeline
// stages (docreader / processing / chunking / embedding). A failure in
// any of these invalidates the attempt and must close the root as failed.
func isMainPipelineStage(name string) bool {
	for _, s := range types.AllStages {
		if s == name {
			return true
		}
	}
	return false
}

// durationSince computes elapsed ms preferring the in-process cache;
// falls back to the *Span's StartedAt for cross-process callers.
func durationSince(t *spanTracker, span *Span, now time.Time) int64 {
	if start, ok := t.takeStart(span.SpanID); ok {
		return now.Sub(start).Milliseconds()
	}
	if !span.StartedAt.IsZero() {
		return now.Sub(span.StartedAt).Milliseconds()
	}
	return 0
}

// FinalizeAttempt closes the root span for (entryID, attempt). The
// pipeline calls this from two places: the success orchestrator
// when the parse reaches Completed, and FailSpan when a
// MAIN stage fails terminally. Without this, the root row created by
// OpenAttempt would stay in "running" forever even after parse_status
// flips to completed/failed — operators would see a perpetually
// "running" trace despite a terminal entry state.
//
// Idempotent: re-closing a root that's already done/failed is a no-op
// so callers from different paths (success vs. cascade-fail vs.
// dead-letter) don't have to coordinate. We deliberately avoid the
// recordStart cache here (cross-process callers won't have it); we
// recompute duration from the persisted row's started_at.
func (t *spanTracker) FinalizeAttempt(ctx context.Context, entryID string, attempt int, status string,
	output types.JSONMap, errorCode, errorMessage string,
) {
	if entryID == "" || attempt <= 0 {
		return
	}
	if status == "" {
		status = types.SpanStatusDone
	}
	rows, err := t.repo.ListByAttempt(ctx, entryID, attempt)
	if err != nil {
		logger.Warnf(ctx, "[SpanTracker] FinalizeAttempt list failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		return
	}
	var root *types.EntryProcessingSpan
	for i := range rows {
		if rows[i].Kind == types.SpanKindRoot {
			cp := rows[i]
			root = &cp
			break
		}
	}
	if root == nil {
		// No root means nothing to close — likely an attempt that
		// predates the tracker or whose OpenAttempt write failed.
		return
	}
	if root.Status == types.SpanStatusDone || root.Status == types.SpanStatusFailed ||
		root.Status == types.SpanStatusCancelled || root.Status == types.SpanStatusSkipped {
		return
	}
	now := time.Now()
	var started time.Time
	if root.StartedAt != nil {
		started = *root.StartedAt
	}
	dur := int64(0)
	if !started.IsZero() {
		dur = now.Sub(started).Milliseconds()
	}
	if len(errorMessage) > 1024 {
		errorMessage = errorMessage[:1024]
	}
	row := &types.EntryProcessingSpan{
		EntryID:      root.EntryID,
		Attempt:      root.Attempt,
		SpanID:       root.SpanID,
		ParentSpanID: root.ParentSpanID,
		Name:         root.Name,
		Kind:         root.Kind,
		Status:       status,
		Input:        root.Input,
		Output:       output,
		Metadata:     root.Metadata,
		ErrorCode:    strings.TrimSpace(errorCode),
		ErrorMessage: errorMessage,
		StartedAt:    root.StartedAt,
		FinishedAt:   &now,
		DurationMs:   dur,
	}
	if err := t.repo.Upsert(ctx, row); err != nil {
		logger.Warnf(ctx, "[SpanTracker] FinalizeAttempt upsert failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		return
	}
	t.touchEntryHeartbeat(ctx, entryID, types.SpanKindRoot)
}

// AbortAttempt is the user-cancel counterpart to FinalizeAttempt. It
// flips every still-running / still-pending span for this attempt to
// cancelled — regardless of tree position — and then closes the root.
//
// Why a flat sweep instead of CancelDescendants' BFS: fan-out stages
// (e.g. 多模态识别) call EndSpan on the stage as soon as they finish
// DISPATCHING their async per-image work, so by the time the user
// hits cancel the stage row is already status=done but its image[*]
// children are still status=running. A BFS that stops at terminal
// parents would orphan those leaves. The flat sweep doesn't care
// about the tree shape — anything not-yet-terminal gets flipped.
func (t *spanTracker) AbortAttempt(ctx context.Context, entryID string, attempt int,
	errorCode, errorMessage, reason string,
) {
	if entryID == "" || attempt <= 0 {
		return
	}
	if reason == "" {
		reason = "user cancelled"
	}
	if errorCode == "" {
		errorCode = "USER_CANCELLED"
	}
	if n, err := t.repo.CancelAllOpenSpans(ctx, entryID, attempt, errorCode, reason); err != nil {
		logger.Warnf(ctx, "[SpanTracker] AbortAttempt sweep failed kid=%s attempt=%d: %v",
			entryID, attempt, err)
		// Fall through to FinalizeAttempt anyway — closing the root
		// is more important than perfectly closing every child.
	} else if n > 0 {
		logger.Infof(ctx,
			"[SpanTracker] AbortAttempt swept %d open span(s) for kid=%s attempt=%d",
			n, entryID, attempt)
	}
	t.FinalizeAttempt(ctx, entryID, attempt,
		types.SpanStatusCancelled, nil, errorCode, errorMessage)
}

// noopSpanTracker collapses every method to a no-op for tests.
type noopSpanTracker struct{}

func (noopSpanTracker) OpenAttempt(_ context.Context, _ string) (*Span, int, error) {
	return nil, 0, nil
}
func (noopSpanTracker) LatestAttempt(_ context.Context, _ string) int { return 0 }
func (noopSpanTracker) BeginStage(_ context.Context, _ string, _ int, _ string, _ types.JSONMap) *Span {
	return nil
}
func (noopSpanTracker) BeginSubSpan(_ context.Context, _ *Span, _, _ string, _ types.JSONMap) *Span {
	return nil
}
func (noopSpanTracker) EndSpan(_ context.Context, _ *Span, _ types.JSONMap)            {}
func (noopSpanTracker) FailSpan(_ context.Context, _ *Span, _, _ string, _ error)      {}
func (noopSpanTracker) SkipSpan(_ context.Context, _ *Span, _ string)                  {}
func (noopSpanTracker) LookupStage(_ context.Context, _ string, _ int, _ string) *Span { return nil }
func (noopSpanTracker) LookupSpanByName(_ context.Context, _ string, _ int, _ string) *Span {
	return nil
}
func (noopSpanTracker) FinalizeAttempt(_ context.Context, _ string, _ int, _ string, _ types.JSONMap, _, _ string) {
}
func (noopSpanTracker) AbortAttempt(_ context.Context, _ string, _ int, _, _, _ string) {}
