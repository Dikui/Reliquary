package handler

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	taskqueue "reliquary/internal/queue"

	"github.com/gin-gonic/gin"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	secutils "reliquary/internal/utils"
)

// EntryHandler processes HTTP requests related to entry resources
type EntryHandler struct {
	cfg            *config.Config
	kgService      interfaces.EntryService
	libraryService interfaces.LibraryService
	taskClient     interfaces.TaskEnqueuer
	spanRepo       repository.EntrySpanRepository
}

// NewEntryHandler creates a new entry handler instance
func NewEntryHandler(
	cfg *config.Config,
	kgService interfaces.EntryService,
	libraryService interfaces.LibraryService,
	taskClient interfaces.TaskEnqueuer,
	spanRepo repository.EntrySpanRepository,
) *EntryHandler {
	return &EntryHandler{
		cfg:            cfg,
		kgService:      kgService,
		libraryService: libraryService,
		taskClient:     taskClient,
		spanRepo:       spanRepo,
	}
}

// validateLibraryAccess validates access permissions to a library
// using the ":id" URL path parameter. It delegates to validateLibraryAccessWithLibraryID.
func (h *EntryHandler) validateLibraryAccess(c *gin.Context) (*types.Library, string, uint64, error) {
	libraryID := secutils.SanitizeForLog(c.Param("id"))
	return h.validateLibraryAccessWithLibraryID(c, libraryID)
}

// validateLibraryAccessWithLibraryID validates access to the given library ID (e.g. from query or body).
// Checks that the library belongs to the current workspace.
// Returns the library, its ID, the space ID, and any error.
func (h *EntryHandler) validateLibraryAccessWithLibraryID(c *gin.Context, libraryID string) (*types.Library, string, uint64, error) {
	ctx := c.Request.Context()
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		logger.Error(ctx, "Failed to get space ID")
		return nil, "", 0, errors.NewUnauthorizedError("Unauthorized")
	}
	userID, userExists := c.Get(types.UserIDContextKey.String())
	libraryID = secutils.SanitizeForLog(libraryID)
	var library *types.Library
	var err error
	if libraryID == "" {
		return nil, "", spaceID, nil
	} else {
		library, err = h.libraryService.GetLibraryByID(ctx, libraryID)
	}
	if err != nil {
		// Same not-found-vs-real-error split as library.go's
		// validateAndGetLibrary: ErrLibraryNotFound is the
		// expected outcome for a probed/stale library id and must surface as
		// 404, not the generic 500 the original code produced.
		if goerrors.Is(err, repository.ErrLibraryNotFound) {
			return nil, libraryID, 0, errors.NewNotFoundError("library not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, libraryID, 0, errors.NewInternalServerError(err.Error())
	}
	if library.SpaceID == spaceID {
		return library, library.ID, spaceID, nil
	}

	_ = userID
	_ = userExists
	logger.Warnf(ctx, "Permission denied to access Library %s, space ID: %d, Library space: %d", libraryID, spaceID, library.SpaceID)
	return nil, libraryID, 0, errors.NewForbiddenError("Permission denied to access this library")
}

// resolveEntryAndValidateLibraryAccess resolves entry within the current workspace.
// Returns the entry, context with effectiveSpaceID set for downstream service calls, and error.
func (h *EntryHandler) resolveEntryAndValidateLibraryAccess(c *gin.Context, entryID string) (*types.Entry, context.Context, error) {
	ctx := c.Request.Context()
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		return nil, ctx, errors.NewUnauthorizedError("Unauthorized")
	}
	userID, userExists := c.Get(types.UserIDContextKey.String())

	entry, err := h.kgService.GetEntryByIDOnly(ctx, entryID)
	if err != nil {
		return nil, ctx, errors.NewNotFoundError("Entry not found")
	}

	// Entry must belong to the current workspace.
	if entry.SpaceID == spaceID {
		return entry, context.WithValue(ctx, types.SpaceIDContextKey, spaceID), nil
	}

	_ = userID
	_ = userExists
	return nil, ctx, errors.NewForbiddenError("Permission denied to access this entry")
}

// enqueueEntryListDelete enqueues an async batch-delete task for the
// given entry IDs and returns the background task ID.
func (h *EntryHandler) enqueueEntryListDelete(
	ctx context.Context, spaceID uint64, ids []string,
) (string, error) {
	entries, err := h.kgService.GetEntryBatch(ctx, spaceID, ids)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		intake, err := entry.IntakeMetadata()
		if err != nil {
			return "", err
		}
		if intake == nil || intake.Decision != "rejected" {
			continue
		}
		_, err = h.libraryService.GetLibraryByID(ctx, entry.LibraryID)
		if err != nil {
			return "", err
		}
		if _, ok := types.PrincipalFromContext(ctx); !ok {
			return "", errors.NewForbiddenError("an authenticated user is required to discard intake")
		}
	}
	payload := types.EntryListDeletePayload{
		SpaceID:   spaceID,
		EntryIDs:  ids,
		Initiator: types.TaskInitiatorFromContext(ctx),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := taskqueue.NewTask(types.TypeEntryListDelete, payloadBytes,
		taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(2*time.Hour))
	info, err := h.taskClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// enqueueEntryListReparse enqueues an async batch-reparse task for the
// given entry IDs and returns the background task ID.
func (h *EntryHandler) enqueueEntryListReparse(
	ctx context.Context, spaceID uint64, ids []string, processConfig *types.EntryProcessOverrides,
) (string, error) {
	payload := types.EntryListReparsePayload{
		SpaceID:       spaceID,
		EntryIDs:      ids,
		ProcessConfig: processConfig,
		Initiator:     types.TaskInitiatorFromContext(ctx),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := taskqueue.NewTask(types.TypeEntryListReparse, payloadBytes,
		taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(time.Hour))
	info, err := h.taskClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// GetEntry godoc
func (h *EntryHandler) GetEntry(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving entry")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	// Resolve entry and validate its workspace.
	entry, effCtx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Re-fetch with the space-scoped service to populate joined source fields.
	if entry, err = h.kgService.GetEntryByID(effCtx, id); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewNotFoundError("Entry not found"))
		return
	}

	logger.Infof(ctx, "Entry retrieved successfully, ID: %s, title: %s",
		secutils.SanitizeForLog(entry.ID), secutils.SanitizeForLog(entry.Title))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entry,
	})
}

// GetEntrySpans godoc
//
// Always returns the canonical processing timeline; missing stage rows are
// synthesized as "pending" so the frontend timeline always renders all
// segments. Processing subspans ride along under
// each stage as children when present.
func (h *EntryHandler) GetEntrySpans(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	entry, _, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Pick attempt: explicit ?attempt=N wins; otherwise pull the
	// latest attempt from the spans table. in-memory fixtures / fresh installs
	// with zero rows fall through to attempt=0, in which case we
	// return a placeholder tree (5 pending stages, no root, no
	// children) so the UI still renders.
	requestedAttempt := 0
	if v := strings.TrimSpace(c.Query("attempt")); v != "" {
		if n, perr := strconv.Atoi(v); perr == nil && n > 0 {
			requestedAttempt = n
		}
	}

	rows := []types.EntryProcessingSpan{}
	currentAttempt := 0
	latestAttempt := 0
	if h.spanRepo != nil {
		latest, lerr := h.spanRepo.LatestAttempt(ctx, entry.ID)
		if lerr != nil {
			logger.Warnf(ctx, "spans LatestAttempt failed for %s: %v", entry.ID, lerr)
		} else {
			latestAttempt = latest
		}
		if requestedAttempt > 0 {
			currentAttempt = requestedAttempt
		} else {
			currentAttempt = latestAttempt
		}
		if currentAttempt > 0 {
			rows, err = h.spanRepo.ListByAttempt(ctx, entry.ID, currentAttempt)
			if err != nil {
				logger.Warnf(ctx, "spans ListByAttempt failed kid=%s attempt=%d: %v",
					entry.ID, currentAttempt, err)
				rows = nil
			}
		}
	}

	// Build tree: index by SpanID, then attach to parents. Stages
	// missing from the DB are synthesized as "pending" placeholders
	// under a synthetic (or real, if present) root so the timeline
	// always renders all processing stages. parse_status threads through so
	// pre-tracker historical entry (no rows but parse_status is
	// already terminal) renders as done/failed instead of pending —
	// otherwise legacy completed documents would forever look like
	// they're still waiting in the queue.
	tree, currentStageName, lastErr := buildSpanTree(entry.ID, currentAttempt, rows, entry.ParseStatus)

	resp := gin.H{
		"entry_id":        entry.ID,
		"attempt":         currentAttempt,
		"latest_attempt":  latestAttempt,
		"parse_status":    entry.ParseStatus,
		"current_attempt": currentAttempt,
		"current_stage":   currentStageName,
		"trace":           tree,
	}
	if lastError := entrySpansLastError(
		currentAttempt,
		latestAttempt,
		entry.ParseStatus,
		entry.ErrorMessage,
		entry.UpdatedAt,
		lastErr,
	); lastError != nil {
		resp["last_error"] = lastError
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// entrySpansLastError builds the last_error payload for GetEntrySpans.
// Span failures win when present; otherwise a failed entry row without a
// matching span error (recovery / dead-letter paths) surfaces ErrorMessage.
func entrySpansLastError(
	currentAttempt, latestAttempt int,
	parseStatus, entryErrorMessage string,
	entryUpdatedAt time.Time,
	spanFailure *types.EntryProcessingSpan,
) gin.H {
	if spanFailure != nil {
		return gin.H{
			"stage":         spanFailure.Name,
			"code":          spanFailure.ErrorCode,
			"message":       spanFailure.ErrorMessage,
			"name":          spanFailure.Name,
			"error_code":    spanFailure.ErrorCode,
			"error_message": spanFailure.ErrorMessage,
			"finished_at":   spanFailure.FinishedAt,
		}
	}
	if currentAttempt != latestAttempt || parseStatus != types.ParseStatusFailed || entryErrorMessage == "" {
		return nil
	}
	errorCode := "UNKNOWN"
	if strings.EqualFold(strings.TrimSpace(entryErrorMessage),
		"Task interrupted due to application restart") {
		errorCode = "SERVER_RESTART"
	}
	return gin.H{
		"stage":         "knowledge_processing",
		"code":          errorCode,
		"message":       entryErrorMessage,
		"name":          "knowledge_processing",
		"error_code":    errorCode,
		"error_message": entryErrorMessage,
		"finished_at":   entryUpdatedAt,
	}
}

// buildSpanTree assembles a flat list of span rows into a parent-child
// tree rooted at the (entry, attempt)'s root span. Missing canonical
// stages are filled in with pending placeholders so the UI always renders
// all processing stages. Returns the root, the current_stage name
// (the running stage if any), and the most recent failed span if one
// exists.
//
// parseStatus is the entry.parse_status string. When the spans table
// has zero rows for this attempt (legacy data parsed before tracking, or
// a fresh entry before the pipeline starts), the placeholder status
// is inferred from parseStatus: completed → done, failed → failed,
// otherwise pending. Without this, every historical entry would
// render as "all stages pending" forever despite having actually
// completed parsing.
func buildSpanTree(entryID string, attempt int, rows []types.EntryProcessingSpan, parseStatus string) (
	root *types.SpanTreeNode, currentStage string, lastFailure *types.EntryProcessingSpan,
) {
	now := time.Now()
	// Build node lookup, identify root.
	nodes := make(map[string]*types.SpanTreeNode, len(rows))
	var rootRow *types.EntryProcessingSpan
	stageRowByName := map[string]*types.EntryProcessingSpan{}
	for i := range rows {
		r := rows[i]
		nodes[r.SpanID] = &types.SpanTreeNode{EntryProcessingSpan: r}
		if r.Kind == types.SpanKindRoot && rootRow == nil {
			cp := r
			rootRow = &cp
		}
		if r.Kind == types.SpanKindStage {
			cp := r
			stageRowByName[r.Name] = &cp
		}
		if r.Status == types.SpanStatusRunning && r.Kind == types.SpanKindStage && currentStage == "" {
			currentStage = r.Name
		}
		if r.Status == types.SpanStatusFailed {
			cp := r
			lastFailure = &cp
		}
	}

	// Pick the synthesized stage status from parse_status. Without this,
	// historical entry that completed before span tracking was wired
	// would render as "5 pending stages" forever — the rows simply
	// weren't recorded, but parse_status correctly reads "completed".
	// The synthesized stages don't carry duration/timing data; they
	// just communicate the inferred terminal state.
	syntheticStatus := types.SpanStatusPending
	switch parseStatus {
	case types.ParseStatusCompleted:
		syntheticStatus = types.SpanStatusDone
	case types.ParseStatusFailed:
		syntheticStatus = types.SpanStatusFailed
	}

	// Synthesize root if no rows came back so the API contract stays
	// stable (frontend always expects a `trace` object).
	if rootRow == nil {
		root = &types.SpanTreeNode{EntryProcessingSpan: types.EntryProcessingSpan{
			EntryID:   entryID,
			Attempt:   attempt,
			SpanID:    "",
			Name:      "knowledge_processing",
			Kind:      types.SpanKindRoot,
			Status:    syntheticStatus,
			CreatedAt: now,
			UpdatedAt: now,
		}}
	} else {
		root = nodes[rootRow.SpanID]
	}

	// Link real children to their parents. Walk `rows` (not the
	// `nodes` map!) so the append order matches the repo's stable
	// `ORDER BY id ASC`. Iterating the map directly would give
	// callers a different child ordering on every request — Go map
	// iteration is intentionally randomised — and the UI would
	// flicker subspans into a different order on each refresh.
	for i := range rows {
		r := &rows[i]
		n := nodes[r.SpanID]
		if n == nil || n == root {
			continue
		}
		if r.ParentSpanID == "" {
			// Real top-level row with no parent and not the root
			// itself — attach to root so it doesn't dangle.
			root.Children = append(root.Children, n)
			continue
		}
		parent, ok := nodes[r.ParentSpanID]
		if !ok {
			// Unknown parent (orphan); attach to root.
			root.Children = append(root.Children, n)
			continue
		}
		parent.Children = append(parent.Children, n)
	}

	// Synthesize missing stage rows as children of root so the timeline
	// always shows all stages. Status mirrors the synthesized root,
	// pending while the pipeline is still running, done/failed for
	// historical entry whose terminal state we know but whose
	// per-stage timing was never recorded. Appended in AllStages order
	// so the canonical stage layout is deterministic regardless of
	// which rows are missing.
	for _, name := range types.AllStages {
		if _, ok := stageRowByName[name]; ok {
			continue
		}
		placeholder := types.EntryProcessingSpan{
			EntryID:   entryID,
			Attempt:   attempt,
			Name:      name,
			Kind:      types.SpanKindStage,
			Status:    syntheticStatus,
			CreatedAt: now,
			UpdatedAt: now,
		}
		root.Children = append(root.Children, &types.SpanTreeNode{EntryProcessingSpan: placeholder})
	}

	return root, currentStage, lastFailure
}

// ListEntry godoc
func (h *EntryHandler) ListEntry(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving entry list")

	// Validate access to the library (read access - any permission level)
	_, libraryID, effectiveSpaceID, err := h.validateLibraryAccess(c)
	if err != nil {
		c.Error(err)
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Parse pagination parameters from query string
	var pagination types.Pagination
	if err := c.ShouldBindQuery(&pagination); err != nil {
		logger.Error(ctx, "Failed to parse pagination parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	if !h.applyLibraryReadScope(c) {
		return
	}
	ctx = c.Request.Context()
	filter := types.EntryListFilter{
		LibraryIDs:  types.LibraryReadScope(ctx),
		SourceKind:  strings.TrimSpace(c.Query("source_kind")),
		ContentType: strings.TrimSpace(c.Query("content_type")),
		Keyword:     c.Query("keyword"),
		FileType:    c.Query("file_type"),
		ParseStatus: c.Query("parse_status"),
		Source:      c.Query("source"),
	}
	if raw, supplied := c.GetQuery("collapse_similar"); supplied {
		value, err := strconv.ParseBool(raw)
		if err != nil {
			c.Error(errors.NewBadRequestError("collapse_similar must be true or false"))
			return
		}
		filter.CollapseSimilar = &value
	}
	switch filter.SourceKind {
	case "", "article", "mine", "report":
	default:
		c.Error(errors.NewBadRequestError("invalid source_kind"))
		return
	}
	if raw, supplied := c.GetQuery("starred"); supplied {
		if raw != "true" && raw != "false" {
			c.Error(errors.NewBadRequestError("starred must be true or false"))
			return
		}
		value := raw == "true"
		filter.Starred = &value
	}
	if raw, supplied := c.GetQuery("incomplete"); supplied {
		if raw != "true" && raw != "false" {
			c.Error(errors.NewBadRequestError("incomplete must be true or false"))
			return
		}
		value := raw == "true"
		filter.Incomplete = &value
	}

	if raw := c.Query("start_time"); raw != "" {
		t, err := parseFilterTime(raw)
		if err != nil {
			c.Error(errors.NewBadRequestError("invalid start_time: " + err.Error()))
			return
		}
		filter.UpdatedFrom = t
	}
	if raw := c.Query("end_time"); raw != "" {
		t, err := parseFilterTime(raw)
		if err != nil {
			c.Error(errors.NewBadRequestError("invalid end_time: " + err.Error()))
			return
		}
		filter.UpdatedTo = t
	}
	logger.Infof(
		ctx,
		"Retrieving entry list under library, library_id=%s keyword=%s file_type=%s parse_status=%s source=%s start_time=%s end_time=%s page=%d page_size=%d effectiveSpaceID=%d",
		secutils.SanitizeForLog(libraryID),
		secutils.SanitizeForLog(filter.Keyword),
		secutils.SanitizeForLog(filter.FileType),
		secutils.SanitizeForLog(filter.ParseStatus),
		secutils.SanitizeForLog(filter.Source),
		secutils.SanitizeForLog(c.Query("start_time")),
		secutils.SanitizeForLog(c.Query("end_time")),
		pagination.Page,
		pagination.PageSize,
		effectiveSpaceID,
	)

	// Retrieve paginated entry entries
	result, err := h.kgService.ListPagedEntryByLibraryID(ctx, libraryID, &pagination, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Entry list retrieved successfully, library ID: %s, total: %d",
		secutils.SanitizeForLog(libraryID),
		result.Total,
	)
	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"data":      result.Data,
		"total":     result.Total,
		"page":      result.Page,
		"page_size": result.PageSize,
	})
}

// dedupeEntryIDs trims, drops empty and de-duplicates a batch of IDs while
// preserving the caller's order.
func dedupeEntryIDs(raw []string) []string {
	seen := make(map[string]struct{}, len(raw))
	ids := make([]string, 0, len(raw))
	for _, item := range raw {
		id := strings.TrimSpace(item)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// requireEntryWriteAccess resolves a library from a request body and
// validates its workspace for the batch routes.
func (h *EntryHandler) requireEntryWriteAccess(
	c *gin.Context,
	requestedLibraryID string,
) (string, uint64, error) {
	_, libraryID, effectiveSpaceID, err := h.validateLibraryAccessWithLibraryID(c, requestedLibraryID)
	if err != nil {
		return "", 0, err
	}

	return libraryID, effectiveSpaceID, nil
}

// requireEntryInLibrary verifies every ID exists and belongs to the given
// library, so a batch operation cannot reach across libraries.
func (h *EntryHandler) requireEntryInLibrary(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
	ids []string,
) error {
	entryList, err := h.kgService.GetEntryBatch(ctx, spaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return errors.NewInternalServerError(err.Error())
	}
	if len(entryList) != len(ids) {
		return errors.NewBadRequestError("One or more entry entries not found")
	}
	for _, k := range entryList {
		if libraryID != "" && k.LibraryID != libraryID {
			return errors.NewBadRequestError(
				fmt.Sprintf("Entry %s does not belong to library %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(libraryID)))
		}
	}
	return nil
}

// DeleteEntry godoc
func (h *EntryHandler) DeleteEntry(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start deleting entry")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	entry, effCtx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	if entry.SourceKind == "mine" {
		collector := h.ingestService(c)
		if collector == nil {
			return
		}
		if err := collector.DeleteDocument(effCtx, id); err != nil {
			c.Error(err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"document_id": id, "status": "deleted"}})
		return
	}

	// Reuse the batch async pipeline so single-item delete shares the same
	// hardening (queue retries, business-aware queue routing, marking-as-deleting
	// inside the worker) as BatchDeleteEntry / ClearLibraryContents.
	effectiveSpaceID, _ := effCtx.Value(types.SpaceIDContextKey).(uint64)
	if effectiveSpaceID == 0 {
		logger.Error(ctx, "Effective space ID missing after access validation")
		c.Error(errors.NewInternalServerError("workspace context unavailable"))
		return
	}

	logger.Infof(ctx, "Enqueuing entry delete, ID: %s", secutils.SanitizeForLog(id))
	taskID, err := h.enqueueEntryListDelete(effCtx, effectiveSpaceID, []string{id})
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue entry delete task: %v", err)
		c.Error(err)
		return
	}

	logger.Infof(ctx, "Entry delete task enqueued: %s, entry_id: %s", taskID, secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Delete task submitted",
		"data": gin.H{
			"task_id": taskID,
		},
	})
}

// BatchDeleteEntryRequest is the body schema for POST /entries/batch-delete.
type BatchDeleteEntryRequest struct {
	IDs []string `json:"ids"  binding:"required"`
}

// BatchDeleteEntry godoc
func (h *EntryHandler) BatchDeleteEntry(c *gin.Context) {
	ctx := c.Request.Context()

	var req BatchDeleteEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	ids := dedupeEntryIDs(req.IDs)
	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("ids cannot be empty"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	// Batch operations resolve each entry in the authenticated space.
	_, libraryID, effectiveSpaceID, err := h.validateLibraryAccessWithLibraryID(c, "")
	if err != nil {
		c.Error(err)
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Single batch fetch to validate that every id exists and belongs to the
	// requested Library. The service-layer DeleteEntryList only enforces space
	// scope, not Library scope, so the handler must guard against cross-Library deletion.
	entryList, err := h.kgService.GetEntryBatch(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if len(entryList) != len(ids) {
		c.Error(errors.NewBadRequestError("One or more entry entries not found"))
		return
	}
	for _, k := range entryList {
		if libraryID != "" && k.LibraryID != libraryID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Entry %s does not belong to library %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(libraryID))))
			return
		}
	}

	taskID, err := h.enqueueEntryListDelete(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch entry delete task: %v", err)
		c.Error(err)
		return
	}

	logger.Infof(ctx, "Batch entry delete task enqueued: %s, library_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(libraryID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch delete task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"deleted_count": len(ids),
		},
	})
}

// ClearLibraryContents godoc
func (h *EntryHandler) ClearLibraryContents(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start clearing library contents")

	library, libraryID, effectiveSpaceID, err := h.validateLibraryAccess(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Library contents can be cleared within the current workspace.
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if library.SpaceID != spaceID {
		c.Error(errors.NewForbiddenError("Library must belong to the current workspace"))
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	entryList, err := h.kgService.ListEntryByLibraryID(ctx, libraryID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to list entry entries").WithDetails(err.Error()))
		return
	}

	if len(entryList) == 0 {
		logger.Infof(ctx, "Library %s is already empty", secutils.SanitizeForLog(libraryID))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Library is already empty",
			"data":    gin.H{"deleted_count": 0},
		})
		return
	}

	entryIDs := make([]string, 0, len(entryList))
	for _, entry := range entryList {
		entryIDs = append(entryIDs, entry.ID)
	}

	taskID, err := h.enqueueEntryListDelete(ctx, effectiveSpaceID, entryIDs)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue entry list delete task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue cleanup task"))
		return
	}

	logger.Infof(ctx, "Library contents clear task enqueued: %s, library_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(libraryID), len(entryIDs))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Library contents clear task submitted",
		"data":    gin.H{"deleted_count": len(entryIDs)},
	})
}

// DownloadEntryFile godoc
func mimeTypeByExt(filename string) string {
	ct, _ := secutils.SafeContentTypeByFilename(filename)
	return ct
}

// PreviewEntryFile godoc
func (h *EntryHandler) ReviewEntryIntake(c *gin.Context) {
	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}
	_, ctx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	var req struct {
		Action     string           `json:"action"`
		SourceInfo types.SourceInfo `json:"source_info"`
		Confirmed  bool             `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	req.SourceInfo.Confirmed = req.Confirmed
	entry, err := h.kgService.ReviewEntryIntake(ctx, id, req.Action, req.SourceInfo)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entry})
}

// GetEntryBatchRequest defines parameters for batch entry retrieval
type GetEntryBatchRequest struct {
	IDs []string `form:"ids" binding:"required"` // List of entry IDs
}

func (h *EntryHandler) GetEntryBatch(c *gin.Context) {
	ctx := c.Request.Context()

	spaceID, ok := c.Get(types.SpaceIDContextKey.String())
	if !ok {
		logger.Error(ctx, "Failed to get space ID")
		c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	effectiveSpaceID := spaceID.(uint64)

	var req GetEntryBatchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}

	_, libraryID, _, err := h.validateLibraryAccessWithLibraryID(c, "")
	if err != nil {
		c.Error(err)
		return
	}
	entries, err := h.kgService.GetEntryBatch(ctx, effectiveSpaceID, req.IDs)
	if libraryID != "" {
		entries = filterEntriesByLibraryAllowSet(entries, map[string]bool{libraryID: true})
	}

	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve entry list").WithDetails(err.Error()))
		return
	}

	logger.Infof(ctx, "Batch entry retrieval successful, requested count: %d, returned count: %d",
		len(req.IDs), len(entries))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    entries,
	})
}

// UpdateEntryRequest defines the partial-update body for PUT /entries/:id.
// Omitted fields are left unchanged; an explicit empty description clears the summary.
type UpdateEntryRequest struct {
	LibraryID      *string         `json:"library_id"`
	Title          *string         `json:"title"`
	Description    *string         `json:"description"`
	CustomMetadata json.RawMessage `json:"custom_metadata"`
}

// UpdateEntry godoc
func (h *EntryHandler) UpdateEntry(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	var req UpdateEntryRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	if req.LibraryID != nil {
		c.Error(errors.NewBadRequestError("entry library_id is immutable"))
		return
	}
	entry := types.Entry{ID: id, CustomMetadata: types.JSON(req.CustomMetadata)}
	if req.Title != nil {
		entry.Title = *req.Title
	}
	if req.Description != nil {
		entry.Description = *req.Description
		entry.DescriptionSpecified = true
	}

	if err := h.kgService.UpdateEntry(effCtx, &entry); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	updated, getErr := h.kgService.GetEntryByID(effCtx, id)
	if getErr != nil {
		logger.Warnf(ctx, "Entry updated but failed to reload status for %s: %v", id, getErr)
	}

	logger.Infof(ctx, "Entry updated successfully, entry ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Entry updated successfully",
		"data":    updated,
	})
}

// ReparseEntry godoc
func (h *EntryHandler) ReparseEntry(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start re-parsing entry")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	// Validate the library workspace before reparsing.
	_, effCtx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Optional per-reparse parse config override. Empty body keeps the
	// overrides stored at upload time.
	var processOverrides *types.EntryProcessOverrides
	if c.Request.ContentLength != 0 {
		var req struct {
			ProcessConfig *types.EntryProcessOverrides `json:"process_config"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error(ctx, "Failed to parse reparse request body", err)
			c.Error(errors.NewBadRequestError("Invalid reparse request body").WithDetails(err.Error()))
			return
		}
		processOverrides = req.ProcessConfig
	}

	// Call service to reparse entry
	entry, err := h.kgService.ReparseEntry(effCtx, id, processOverrides)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Entry reparse task submitted successfully, entry ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Entry reparse task submitted",
		"data":    entry,
	})
}

// CancelEntryParse godoc
func (h *EntryHandler) CancelEntryParse(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start cancelling entry parse")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Entry ID is empty")
		c.Error(errors.NewBadRequestError("Entry ID cannot be empty"))
		return
	}

	// Validate the workspace as for reparsing and deleting entry.
	_, effCtx, err := h.resolveEntryAndValidateLibraryAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	entry, err := h.kgService.CancelEntryParse(effCtx, id)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"entry_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Entry parse cancelled successfully, entry ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Entry parse cancelled",
		"data":    entry,
	})
}

func (h *EntryHandler) SearchEntry(c *gin.Context) {
	ctx := c.Request.Context()
	if userID, ok := c.Get(types.UserIDContextKey.String()); ok {
		ctx = context.WithValue(ctx, types.UserIDContextKey, userID)
	}
	// Accept both ?keyword= (legacy / upstream name) and ?query= (what most
	// API callers send). Empty input is only valid for an explicit
	// recent-file browse request; ordinary callers still get a clear 400 instead
	// of silently receiving the same newest cards for every missing query.
	keyword := c.Query("keyword")
	if keyword == "" {
		keyword = c.Query("query")
	}
	recent, _ := strconv.ParseBool(c.DefaultQuery("recent", "false"))
	if strings.TrimSpace(keyword) == "" && !recent {
		c.Error(errors.NewBadRequestError("missing search keyword: pass ?keyword=... or ?query=..."))
		return
	}
	keyword = strings.TrimSpace(keyword)
	offset, limit, ok := parseOffsetPagination(c)
	if !ok {
		return
	}

	var fileTypes []string
	if fileTypesStr := c.Query("file_types"); fileTypesStr != "" {
		for _, ft := range strings.Split(fileTypesStr, ",") {
			ft = strings.TrimSpace(ft)
			if ft != "" {
				fileTypes = append(fileTypes, ft)
			}
		}
	}

	entries, hasMore, total, err := h.kgService.SearchEntry(ctx, keyword, offset, limit, fileTypes)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to search entry").WithDetails(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"data":     entries,
		"has_more": hasMore,
		"total":    total,
	})
}

// parseFilterTime parses a query-string timestamp accepted by entry list
// filters. It supports RFC3339, RFC3339 with milliseconds, and the date-only
// "2006-01-02" form (interpreted at start of day in the local timezone).
func parseFilterTime(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	layouts := []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	var lastErr error
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t, nil
		} else {
			lastErr = err
		}
	}
	return time.Time{}, lastErr
}

type batchReparseEntryRequest struct {
	IDs           []string                     `json:"ids" binding:"required"`
	ProcessConfig *types.EntryProcessOverrides `json:"process_config,omitempty"`
}

// ReviewLibrary schedules a fresh admission decision for the selected scope.
func (h *EntryHandler) ReviewLibrary(c *gin.Context) {
	library, _, spaceID, err := h.validateLibraryAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	var request struct {
		Scope string `json:"scope" binding:"required,oneof=all pending"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError("scope must be all or pending"))
		return
	}
	ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, spaceID)
	result, err := h.kgService.ReviewLibrary(ctx, library.ID, request.Scope)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": result})
}

// RebuildLibrary schedules source-preserving reprocessing in the selected scope.
func (h *EntryHandler) RebuildLibrary(c *gin.Context) {
	library, _, spaceID, err := h.validateLibraryAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	var request struct {
		Scope string `json:"scope"`
	}
	if err := c.ShouldBindJSON(&request); err != nil && !goerrors.Is(err, io.EOF) {
		c.Error(errors.NewBadRequestError("scope must be failed or all"))
		return
	}
	ctx := context.WithValue(c.Request.Context(), types.SpaceIDContextKey, spaceID)
	result, err := h.kgService.RebuildLibrary(ctx, library.ID, request.Scope)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
		} else {
			logger.ErrorWithFields(ctx, err, map[string]interface{}{"library_id": library.ID})
			c.Error(errors.NewInternalServerError("Failed to submit library rebuild"))
		}
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": result})
}

// BatchReparseEntry godoc
func (h *EntryHandler) BatchReparseEntry(c *gin.Context) {
	ctx := c.Request.Context()
	var req batchReparseEntryRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf(ctx, "failed to parse batch reparse entry request: %v", err)
		c.Error(errors.NewBadRequestError("invalid batch reparse entry request parameters"))
		return
	}

	seen := make(map[string]struct{}, len(req.IDs))
	ids := make([]string, 0, len(req.IDs))
	for _, raw := range req.IDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("no entry IDs provided for batch reparse"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	_, libraryID, effectiveSpaceID, err := h.validateLibraryAccessWithLibraryID(c, "")
	if err != nil {
		c.Error(err)
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	entryList, err := h.kgService.GetEntryBatch(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.Errorf(ctx, "failed to get entry batch, library_id: %s, size: %d, err: %v", libraryID, len(ids), err)
		c.Error(errors.NewInternalServerError("failed to get entry batch"))
		return
	}
	if len(entryList) != len(ids) {
		c.Error(errors.NewBadRequestError("some entry entries were not found"))
		return
	}
	for _, k := range entryList {
		if libraryID != "" && k.LibraryID != libraryID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Entry %s does not belong to library %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(libraryID))))
			return
		}
	}

	taskID, err := h.enqueueEntryListReparse(ctx, effectiveSpaceID, ids, req.ProcessConfig)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch entry reparse task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue batch reparse task"))
		return
	}

	logger.Infof(ctx, "Batch entry reparse task enqueued: %s, library_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(libraryID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch reparse task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"reparse_count": len(ids),
		},
	})
}

func filterEntriesByLibraryAllowSet(entries []*types.Entry, allowed map[string]bool) []*types.Entry {
	if allowed == nil {
		return entries
	}
	filtered := make([]*types.Entry, 0, len(entries))
	for _, k := range entries {
		if k != nil && allowed[k.LibraryID] {
			filtered = append(filtered, k)
		}
	}
	return filtered
}
