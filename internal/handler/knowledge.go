package handler

import (
	"context"
	"encoding/json"
	goerrors "errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	taskqueue "reliquary/internal/queue"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
	secutils "reliquary/internal/utils"
)

// KnowledgeHandler processes HTTP requests related to knowledge resources
type KnowledgeHandler struct {
	cfg        *config.Config
	kgService  interfaces.KnowledgeService
	kbService  interfaces.KnowledgeBaseService
	taskClient interfaces.TaskEnqueuer
	spanRepo   repository.KnowledgeSpanRepository
}

// NewKnowledgeHandler creates a new knowledge handler instance
func NewKnowledgeHandler(
	cfg *config.Config,
	kgService interfaces.KnowledgeService,
	kbService interfaces.KnowledgeBaseService,
	taskClient interfaces.TaskEnqueuer,
	spanRepo repository.KnowledgeSpanRepository,
) *KnowledgeHandler {
	return &KnowledgeHandler{
		cfg:        cfg,
		kgService:  kgService,
		kbService:  kbService,
		taskClient: taskClient,
		spanRepo:   spanRepo,
	}
}

// validateKnowledgeBaseAccess validates access permissions to a knowledge base
// using the ":id" URL path parameter. It delegates to validateKnowledgeBaseAccessWithKBID.
func (h *KnowledgeHandler) validateKnowledgeBaseAccess(c *gin.Context) (*types.KnowledgeBase, string, uint64, error) {
	kbID := secutils.SanitizeForLog(c.Param("id"))
	return h.validateKnowledgeBaseAccessWithKBID(c, kbID)
}

// validateKnowledgeBaseAccessWithKBID validates access to the given knowledge base ID (e.g. from query or body).
// Checks that the knowledge base belongs to the current workspace.
// Returns the knowledge base, its ID, the space ID, and any error.
func (h *KnowledgeHandler) validateKnowledgeBaseAccessWithKBID(c *gin.Context, kbID string) (*types.KnowledgeBase, string, uint64, error) {
	ctx := c.Request.Context()
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		logger.Error(ctx, "Failed to get space ID")
		return nil, "", 0, errors.NewUnauthorizedError("Unauthorized")
	}
	userID, userExists := c.Get(types.UserIDContextKey.String())
	kbID = secutils.SanitizeForLog(kbID)
	var kb *types.KnowledgeBase
	var err error
	if kbID == "" {
		kb, err = h.kbService.GetKnowledgeBase(ctx)
	} else {
		kb, err = h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	}
	if err != nil {
		// Same not-found-vs-real-error split as knowledgebase.go's
		// validateAndGetKnowledgeBase: ErrKnowledgeBaseNotFound is the
		// expected outcome for a probed/stale kb id and must surface as
		// 404, not the generic 500 the original code produced.
		if goerrors.Is(err, repository.ErrKnowledgeBaseNotFound) {
			return nil, kbID, 0, errors.NewNotFoundError("knowledge base not found")
		}
		logger.ErrorWithFields(ctx, err, nil)
		return nil, kbID, 0, errors.NewInternalServerError(err.Error())
	}
	if kb.SpaceID == spaceID {
		return kb, kb.ID, spaceID, nil
	}

	_ = userID
	_ = userExists
	logger.Warnf(ctx, "Permission denied to access KB %s, space ID: %d, KB space: %d", kbID, spaceID, kb.SpaceID)
	return nil, kbID, 0, errors.NewForbiddenError("Permission denied to access this knowledge base")
}

// resolveKnowledgeAndValidateKBAccess resolves knowledge within the current workspace.
// Returns the knowledge, context with effectiveSpaceID set for downstream service calls, and error.
func (h *KnowledgeHandler) resolveKnowledgeAndValidateKBAccess(c *gin.Context, knowledgeID string) (*types.Knowledge, context.Context, error) {
	ctx := c.Request.Context()
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if spaceID == 0 {
		return nil, ctx, errors.NewUnauthorizedError("Unauthorized")
	}
	userID, userExists := c.Get(types.UserIDContextKey.String())

	knowledge, err := h.kgService.GetKnowledgeByIDOnly(ctx, knowledgeID)
	if err != nil {
		return nil, ctx, errors.NewNotFoundError("Knowledge not found")
	}

	// Knowledge must belong to the current workspace.
	if knowledge.SpaceID == spaceID {
		return knowledge, context.WithValue(ctx, types.SpaceIDContextKey, spaceID), nil
	}

	_ = userID
	_ = userExists
	return nil, ctx, errors.NewForbiddenError("Permission denied to access this knowledge")
}

// handleDuplicateKnowledgeError handles cases where duplicate knowledge is detected
// Returns true if the error was a duplicate error and was handled, false otherwise
func (h *KnowledgeHandler) handleDuplicateKnowledgeError(c *gin.Context,
	err error, knowledge *types.Knowledge, duplicateType string,
) bool {
	if dupErr, ok := err.(*types.DuplicateKnowledgeError); ok {
		ctx := c.Request.Context()
		logger.Warnf(ctx, "Detected duplicate %s: %s", duplicateType, secutils.SanitizeForLog(dupErr.Error()))
		if knowledge == nil {
			knowledge = dupErr.Knowledge
		}
		response := gin.H{
			"success": false,
			"message": dupErr.Error(),
			"data":    knowledge, // knowledge contains the existing document
			"code":    fmt.Sprintf("duplicate_%s", duplicateType),
		}
		if knowledge != nil && knowledge.DuplicateMatchMethod != "" {
			response["match_method"] = knowledge.DuplicateMatchMethod
		}
		c.JSON(http.StatusConflict, response)
		return true
	}
	return false
}

// enqueueKnowledgeListDelete enqueues an async batch-delete task for the
// given knowledge IDs and returns the background task ID.
func (h *KnowledgeHandler) enqueueKnowledgeListDelete(
	ctx context.Context, spaceID uint64, ids []string,
) (string, error) {
	entries, err := h.kgService.GetKnowledgeBatch(ctx, spaceID, ids)
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
		_, err = h.kbService.GetKnowledgeBaseByID(ctx, entry.KnowledgeBaseID)
		if err != nil {
			return "", err
		}
		if _, ok := types.PrincipalFromContext(ctx); !ok {
			return "", errors.NewForbiddenError("an authenticated user is required to discard intake")
		}
	}
	payload := types.KnowledgeListDeletePayload{
		SpaceID:      spaceID,
		KnowledgeIDs: ids,
		Initiator:    types.TaskInitiatorFromContext(ctx),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := taskqueue.NewTask(types.TypeKnowledgeListDelete, payloadBytes,
		taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(2*time.Hour))
	info, err := h.taskClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// enqueueKnowledgeListReparse enqueues an async batch-reparse task for the
// given knowledge IDs and returns the background task ID.
func (h *KnowledgeHandler) enqueueKnowledgeListReparse(
	ctx context.Context, spaceID uint64, ids []string, processConfig *types.KnowledgeProcessOverrides,
) (string, error) {
	payload := types.KnowledgeListReparsePayload{
		SpaceID:       spaceID,
		KnowledgeIDs:  ids,
		ProcessConfig: processConfig,
		Initiator:     types.TaskInitiatorFromContext(ctx),
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}
	task := taskqueue.NewTask(types.TypeKnowledgeListReparse, payloadBytes,
		taskqueue.Queue(types.QueueMaintenance), taskqueue.MaxRetry(3), taskqueue.Timeout(time.Hour))
	info, err := h.taskClient.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("enqueue task: %w", err)
	}
	return info.ID, nil
}

// CreateKnowledgeFromFile godoc
func (h *KnowledgeHandler) CreateKnowledgeFromFile(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating knowledge from file")

	// Validate that the knowledge base belongs to the current workspace.
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Check write permission

	// Validate file size — read MAX_FILE_SIZE_MB env (50MB default).
	// Deliberately not a runtime system_setting; see filesize.go for the
	// rationale (nginx / browser bundle all cache this at
	// container startup, so a UI knob would silently mismatch).
	maxSizeMB := utils.GetMaxFileSizeMB()
	maxSize := maxSizeMB * 1024 * 1024
	// Capped before the multipart parse, not after: FormFile buffers the whole
	// body first, so the size check below only ever sees an upload we already
	// accepted. nginx location /api/ still enforces MAX_FILE_SIZE; this is the
	// same cap for requests that reach the app without that proxy.
	limitUploadBody(c, maxSize)

	// Get the uploaded file
	file, err := c.FormFile("file")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			logger.Error(ctx, "File size too large")
			c.Error(errors.NewBadRequestError(fmt.Sprintf("文件大小不能超过%dMB", maxSizeMB)))
			return
		}
		logger.Error(ctx, "File upload failed", err)
		c.Error(errors.NewBadRequestError("File upload failed").WithDetails(err.Error()))
		return
	}
	if file.Size > maxSize {
		logger.Error(ctx, "File size too large")
		c.Error(errors.NewBadRequestError(fmt.Sprintf("文件大小不能超过%dMB", maxSizeMB)))
		return
	}

	// Get custom filename if provided
	customFileName := c.PostForm("fileName")
	customFileName = secutils.SanitizeForLog(customFileName)
	displayFileName := file.Filename
	displayFileName = secutils.SanitizeForLog(displayFileName)
	if customFileName != "" {
		displayFileName = customFileName
		logger.Infof(ctx, "Using custom filename: %s (original: %s)", customFileName, displayFileName)
	}

	logger.Infof(ctx, "File upload successful, filename: %s, size: %.2f KB", displayFileName, float64(file.Size)/1024)
	logger.Infof(ctx, "Creating knowledge, knowledge base ID: %s, filename: %s", kbID, displayFileName)

	var metadata map[string]string
	if raw := c.PostForm("metadata"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			c.Error(errors.NewBadRequestError("Invalid metadata format"))
			return
		}
	}
	if raw := c.PostForm("source_info"); raw != "" {
		var source types.SourceInfo
		if err := json.Unmarshal([]byte(raw), &source); err != nil {
			c.Error(errors.NewBadRequestError("Invalid source_info format"))
			return
		}
		if metadata == nil {
			metadata = make(map[string]string)
		}
		metadata["author"], metadata["publisher"], metadata["original_url"], metadata["published_at"] = source.Author, source.Publisher, source.OriginalURL, source.PublishedAt
	}

	var processOverrides *types.KnowledgeProcessOverrides
	if raw := c.PostForm("process_config"); raw != "" {
		processOverrides = &types.KnowledgeProcessOverrides{}
		if err := json.Unmarshal([]byte(raw), processOverrides); err != nil {
			logger.Error(ctx, "Failed to parse process_config", err)
			c.Error(errors.NewBadRequestError("Invalid process_config format").WithDetails(err.Error()))
			return
		}
	}

	channel := c.PostForm("channel")

	// Create knowledge entry from the file
	knowledge, err := h.kgService.CreateKnowledgeFromFile(ctx, kbID, file, metadata, nil, customFileName, channel, processOverrides)
	// Check for duplicate knowledge error
	if err != nil {
		if h.handleDuplicateKnowledgeError(c, err, knowledge, "file") {
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge created successfully, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID),
		secutils.SanitizeForLog(knowledge.Title),
	)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// CreateKnowledgeFromURL godoc
func (h *KnowledgeHandler) CreateKnowledgeFromURL(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating knowledge from URL")

	// Validate that the knowledge base belongs to the current workspace.
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Check write permission

	// Parse URL from request body
	var req struct {
		URL           string                           `json:"url" binding:"required"`
		FileName      string                           `json:"file_name"`
		FileType      string                           `json:"file_type"`
		Title         string                           `json:"title"`
		Channel       string                           `json:"channel"`
		ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config"`
		SourceInfo    types.SourceInfo                 `json:"source_info"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse URL request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	logger.Infof(ctx, "Received URL request: %s, file_name: %s, file_type: %s",
		secutils.SanitizeForLog(req.URL),
		secutils.SanitizeForLog(req.FileName),
		secutils.SanitizeForLog(req.FileType),
	)

	// SSRF validation for user-supplied URL
	if err := secutils.ValidateURLForSSRF(req.URL); err != nil {
		logger.Warnf(ctx, "SSRF validation failed for knowledge URL: %v", err)
		c.Error(errors.NewBadRequestError(secutils.FormatSSRFError("URL", req.URL, err)))
		return
	}

	logger.Infof(ctx,
		"Creating knowledge from URL, knowledge base ID: %s, URL: %s",
		secutils.SanitizeForLog(kbID),
		secutils.SanitizeForLog(req.URL),
	)

	// Create knowledge entry from the URL
	knowledge, err := h.kgService.CreateKnowledgeFromURL(
		ctx, kbID, req.URL, req.FileName, req.FileType, nil, req.Title, req.Channel, req.ProcessConfig, req.SourceInfo,
	)
	// Check for duplicate knowledge error
	if err != nil {
		if h.handleDuplicateKnowledgeError(c, err, knowledge, "url") {
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge created successfully from URL, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID),
		secutils.SanitizeForLog(knowledge.Title),
	)
	c.JSON(http.StatusCreated, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// CreateManualKnowledge godoc
func (h *KnowledgeHandler) CreateManualKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start creating manual knowledge")

	// Validate that the knowledge base belongs to the current workspace.
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Check write permission

	var req types.ManualKnowledgePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse manual knowledge request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	knowledge, err := h.kgService.CreateKnowledgeFromManual(ctx, kbID, &req, req.Channel)
	if err != nil {
		if h.handleDuplicateKnowledgeError(c, err, knowledge, "manual") {
			return
		}
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"kb_id": kbID,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Manual knowledge created successfully, knowledge ID: %s",
		secutils.SanitizeForLog(knowledge.ID))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// GetKnowledge godoc
func (h *KnowledgeHandler) GetKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Resolve knowledge and validate its workspace.
	knowledge, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Re-fetch with the space-scoped service to populate joined source fields.
	if knowledge, err = h.kgService.GetKnowledgeByID(effCtx, id); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewNotFoundError("Knowledge not found"))
		return
	}

	logger.Infof(ctx, "Knowledge retrieved successfully, ID: %s, title: %s",
		secutils.SanitizeForLog(knowledge.ID), secutils.SanitizeForLog(knowledge.Title))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// GetKnowledgeSpans godoc
//
// Always returns the canonical processing timeline; missing stage rows are
// synthesized as "pending" so the frontend timeline always renders all
// segments. Processing subspans ride along under
// each stage as children when present.
func (h *KnowledgeHandler) GetKnowledgeSpans(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	knowledge, _, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
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

	rows := []types.KnowledgeProcessingSpan{}
	currentAttempt := 0
	latestAttempt := 0
	if h.spanRepo != nil {
		latest, lerr := h.spanRepo.LatestAttempt(ctx, knowledge.ID)
		if lerr != nil {
			logger.Warnf(ctx, "spans LatestAttempt failed for %s: %v", knowledge.ID, lerr)
		} else {
			latestAttempt = latest
		}
		if requestedAttempt > 0 {
			currentAttempt = requestedAttempt
		} else {
			currentAttempt = latestAttempt
		}
		if currentAttempt > 0 {
			rows, err = h.spanRepo.ListByAttempt(ctx, knowledge.ID, currentAttempt)
			if err != nil {
				logger.Warnf(ctx, "spans ListByAttempt failed kid=%s attempt=%d: %v",
					knowledge.ID, currentAttempt, err)
				rows = nil
			}
		}
	}

	// Build tree: index by SpanID, then attach to parents. Stages
	// missing from the DB are synthesized as "pending" placeholders
	// under a synthetic (or real, if present) root so the timeline
	// always renders all processing stages. parse_status threads through so
	// pre-tracker historical knowledge (no rows but parse_status is
	// already terminal) renders as done/failed instead of pending —
	// otherwise legacy completed documents would forever look like
	// they're still waiting in the queue.
	tree, currentStageName, lastErr := buildSpanTree(knowledge.ID, currentAttempt, rows, knowledge.ParseStatus)

	resp := gin.H{
		"knowledge_id":    knowledge.ID,
		"attempt":         currentAttempt,
		"latest_attempt":  latestAttempt,
		"parse_status":    knowledge.ParseStatus,
		"current_attempt": currentAttempt,
		"current_stage":   currentStageName,
		"trace":           tree,
	}
	if lastError := knowledgeSpansLastError(
		currentAttempt,
		latestAttempt,
		knowledge.ParseStatus,
		knowledge.ErrorMessage,
		knowledge.UpdatedAt,
		lastErr,
	); lastError != nil {
		resp["last_error"] = lastError
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    resp,
	})
}

// knowledgeSpansLastError builds the last_error payload for GetKnowledgeSpans.
// Span failures win when present; otherwise a failed knowledge row without a
// matching span error (recovery / dead-letter paths) surfaces ErrorMessage.
func knowledgeSpansLastError(
	currentAttempt, latestAttempt int,
	parseStatus, knowledgeErrorMessage string,
	knowledgeUpdatedAt time.Time,
	spanFailure *types.KnowledgeProcessingSpan,
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
	if currentAttempt != latestAttempt || parseStatus != types.ParseStatusFailed || knowledgeErrorMessage == "" {
		return nil
	}
	errorCode := "UNKNOWN"
	if strings.EqualFold(strings.TrimSpace(knowledgeErrorMessage),
		"Task interrupted due to application restart") {
		errorCode = "SERVER_RESTART"
	}
	return gin.H{
		"stage":         "knowledge_processing",
		"code":          errorCode,
		"message":       knowledgeErrorMessage,
		"name":          "knowledge_processing",
		"error_code":    errorCode,
		"error_message": knowledgeErrorMessage,
		"finished_at":   knowledgeUpdatedAt,
	}
}

// buildSpanTree assembles a flat list of span rows into a parent-child
// tree rooted at the (knowledge, attempt)'s root span. Missing canonical
// stages are filled in with pending placeholders so the UI always renders
// all processing stages. Returns the root, the current_stage name
// (the running stage if any), and the most recent failed span if one
// exists.
//
// parseStatus is the knowledge.parse_status string. When the spans table
// has zero rows for this attempt (legacy data parsed before tracking, or
// a fresh knowledge before the pipeline starts), the placeholder status
// is inferred from parseStatus: completed → done, failed → failed,
// otherwise pending. Without this, every historical knowledge would
// render as "all stages pending" forever despite having actually
// completed parsing.
func buildSpanTree(knowledgeID string, attempt int, rows []types.KnowledgeProcessingSpan, parseStatus string) (
	root *types.SpanTreeNode, currentStage string, lastFailure *types.KnowledgeProcessingSpan,
) {
	now := time.Now()
	// Build node lookup, identify root.
	nodes := make(map[string]*types.SpanTreeNode, len(rows))
	var rootRow *types.KnowledgeProcessingSpan
	stageRowByName := map[string]*types.KnowledgeProcessingSpan{}
	for i := range rows {
		r := rows[i]
		nodes[r.SpanID] = &types.SpanTreeNode{KnowledgeProcessingSpan: r}
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
	// historical knowledge that completed before span tracking was wired
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
		root = &types.SpanTreeNode{KnowledgeProcessingSpan: types.KnowledgeProcessingSpan{
			KnowledgeID: knowledgeID,
			Attempt:     attempt,
			SpanID:      "",
			Name:        "knowledge_processing",
			Kind:        types.SpanKindRoot,
			Status:      syntheticStatus,
			CreatedAt:   now,
			UpdatedAt:   now,
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
	// historical knowledge whose terminal state we know but whose
	// per-stage timing was never recorded. Appended in AllStages order
	// so the canonical stage layout is deterministic regardless of
	// which rows are missing.
	for _, name := range types.AllStages {
		if _, ok := stageRowByName[name]; ok {
			continue
		}
		placeholder := types.KnowledgeProcessingSpan{
			KnowledgeID: knowledgeID,
			Attempt:     attempt,
			Name:        name,
			Kind:        types.SpanKindStage,
			Status:      syntheticStatus,
			CreatedAt:   now,
			UpdatedAt:   now,
		}
		root.Children = append(root.Children, &types.SpanTreeNode{KnowledgeProcessingSpan: placeholder})
	}

	return root, currentStage, lastFailure
}

// ListKnowledge godoc
func (h *KnowledgeHandler) ListKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start retrieving knowledge list")

	// Validate access to the knowledge base (read access - any permission level)
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccess(c)
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

	filter := types.KnowledgeListFilter{
		ContentType: strings.TrimSpace(c.Query("content_type")),
		Keyword:     c.Query("keyword"),
		FileType:    c.Query("file_type"),
		ParseStatus: c.Query("parse_status"),
		Source:      c.Query("source"),
	}
	if raw, supplied := c.GetQuery("starred"); supplied {
		if raw != "true" && raw != "false" {
			c.Error(errors.NewBadRequestError("starred must be true or false"))
			return
		}
		value := raw == "true"
		filter.Starred = &value
	}
	if filter.ContentType != "" && filter.ContentType != "unclassified" && types.NormalizeContentType(filter.ContentType) == "" {
		c.Error(errors.NewBadRequestError("invalid content_type"))
		return
	}
	if filter.ContentType != "unclassified" {
		filter.ContentType = types.NormalizeContentType(filter.ContentType)
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
		"Retrieving knowledge list under knowledge base, kb_id=%s keyword=%s file_type=%s parse_status=%s source=%s start_time=%s end_time=%s page=%d page_size=%d effectiveSpaceID=%d",
		secutils.SanitizeForLog(kbID),
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

	// Retrieve paginated knowledge entries
	result, err := h.kgService.ListPagedKnowledgeByKnowledgeBaseID(ctx, kbID, &pagination, filter)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(
		ctx,
		"Knowledge list retrieved successfully, knowledge base ID: %s, total: %d",
		secutils.SanitizeForLog(kbID),
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

// dedupeKnowledgeIDs trims, drops empty and de-duplicates a batch of IDs while
// preserving the caller's order.
func dedupeKnowledgeIDs(raw []string) []string {
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

// requireKnowledgeWriteAccess resolves a knowledge base from a request body and
// validates its workspace for the batch routes.
func (h *KnowledgeHandler) requireKnowledgeWriteAccess(
	c *gin.Context,
	requestedKBID string,
) (string, uint64, error) {
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccessWithKBID(c, requestedKBID)
	if err != nil {
		return "", 0, err
	}

	return kbID, effectiveSpaceID, nil
}

// requireKnowledgeInKB verifies every ID exists and belongs to the given
// knowledge base, so a batch operation cannot reach across knowledge bases.
func (h *KnowledgeHandler) requireKnowledgeInKB(
	ctx context.Context,
	spaceID uint64,
	kbID string,
	ids []string,
) error {
	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, spaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		return errors.NewInternalServerError(err.Error())
	}
	if len(knowledgeList) != len(ids) {
		return errors.NewBadRequestError("One or more knowledge entries not found")
	}
	for _, k := range knowledgeList {
		if k.KnowledgeBaseID != kbID {
			return errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID)))
		}
	}
	return nil
}

// DeleteKnowledge godoc
func (h *KnowledgeHandler) DeleteKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start deleting knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Reuse the batch async pipeline so single-item delete shares the same
	// hardening (queue retries, business-aware queue routing, marking-as-deleting
	// inside the worker) as BatchDeleteKnowledge / ClearKnowledgeBaseContents.
	effectiveSpaceID, _ := effCtx.Value(types.SpaceIDContextKey).(uint64)
	if effectiveSpaceID == 0 {
		logger.Error(ctx, "Effective space ID missing after access validation")
		c.Error(errors.NewInternalServerError("workspace context unavailable"))
		return
	}

	logger.Infof(ctx, "Enqueuing knowledge delete, ID: %s", secutils.SanitizeForLog(id))
	taskID, err := h.enqueueKnowledgeListDelete(effCtx, effectiveSpaceID, []string{id})
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue knowledge delete task: %v", err)
		c.Error(err)
		return
	}

	logger.Infof(ctx, "Knowledge delete task enqueued: %s, knowledge_id: %s", taskID, secutils.SanitizeForLog(id))
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Delete task submitted",
		"data": gin.H{
			"task_id": taskID,
		},
	})
}

// BatchDeleteKnowledgeRequest is the body schema for POST /knowledge/batch-delete.
type BatchDeleteKnowledgeRequest struct {
	IDs []string `json:"ids"  binding:"required"`
}

// BatchDeleteKnowledge godoc
func (h *KnowledgeHandler) BatchDeleteKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	var req BatchDeleteKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	ids := dedupeKnowledgeIDs(req.IDs)
	if len(ids) == 0 {
		c.Error(errors.NewBadRequestError("ids cannot be empty"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	// Resolve the sole knowledge base before applying the batch operation.
	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccessWithKBID(c, "")
	if err != nil {
		c.Error(err)
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	// Single batch fetch to validate that every id exists and belongs to the
	// requested KB. The service-layer DeleteKnowledgeList only enforces space
	// scope, not KB scope, so the handler must guard against cross-KB deletion.
	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	if len(knowledgeList) != len(ids) {
		c.Error(errors.NewBadRequestError("One or more knowledge entries not found"))
		return
	}
	for _, k := range knowledgeList {
		if k.KnowledgeBaseID != kbID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID))))
			return
		}
	}

	taskID, err := h.enqueueKnowledgeListDelete(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch knowledge delete task: %v", err)
		c.Error(err)
		return
	}

	logger.Infof(ctx, "Batch knowledge delete task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch delete task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"deleted_count": len(ids),
		},
	})
}

// ClearKnowledgeBaseContents godoc
func (h *KnowledgeHandler) ClearKnowledgeBaseContents(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start clearing knowledge base contents")

	kb, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccess(c)
	if err != nil {
		c.Error(err)
		return
	}

	// Knowledge base contents can be cleared within the current workspace.
	spaceID := c.GetUint64(types.SpaceIDContextKey.String())
	if kb.SpaceID != spaceID {
		c.Error(errors.NewForbiddenError("Knowledge base must belong to the current workspace"))
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	knowledgeList, err := h.kgService.ListKnowledgeByKnowledgeBaseID(ctx, kbID)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to list knowledge entries").WithDetails(err.Error()))
		return
	}

	if len(knowledgeList) == 0 {
		logger.Infof(ctx, "Knowledge base %s is already empty", secutils.SanitizeForLog(kbID))
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "Knowledge base is already empty",
			"data":    gin.H{"deleted_count": 0},
		})
		return
	}

	knowledgeIDs := make([]string, 0, len(knowledgeList))
	for _, knowledge := range knowledgeList {
		knowledgeIDs = append(knowledgeIDs, knowledge.ID)
	}

	taskID, err := h.enqueueKnowledgeListDelete(ctx, effectiveSpaceID, knowledgeIDs)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue knowledge list delete task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue cleanup task"))
		return
	}

	logger.Infof(ctx, "Knowledge base contents clear task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(knowledgeIDs))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge base contents clear task submitted",
		"data":    gin.H{"deleted_count": len(knowledgeIDs)},
	})
}

// DownloadKnowledgeFile godoc
func (h *KnowledgeHandler) DownloadKnowledgeFile(c *gin.Context) {
	ctx := c.Request.Context()

	logger.Info(ctx, "Start downloading knowledge file")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}
	logger.Infof(ctx, "Retrieving knowledge file, ID: %s", secutils.SanitizeForLog(id))

	file, filename, err := h.kgService.GetKnowledgeFile(effCtx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve file").WithDetails(err.Error()))
		return
	}
	defer file.Close()

	logger.Infof(
		ctx,
		"Knowledge file retrieved successfully, ID: %s, filename: %s",
		secutils.SanitizeForLog(id),
		secutils.SanitizeForLog(filename),
	)

	// Set response headers for file download
	c.Header("Content-Description", "File Transfer")
	c.Header("Content-Transfer-Encoding", "binary")
	cd := mime.FormatMediaType("attachment", map[string]string{"filename": filename})
	c.Header("Content-Disposition", cd)
	c.Header("Content-Type", mimeTypeByExt(filename))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Expires", "0")
	c.Header("Cache-Control", "must-revalidate")
	c.Header("Pragma", "public")

	// Stream file content to response
	c.Stream(func(w io.Writer) bool {
		if _, err := io.Copy(w, file); err != nil {
			logger.Errorf(ctx, "Failed to send file: %v", err)
			return false
		}
		logger.Debug(ctx, "File sending completed")
		return false
	})
}

// mimeTypeByExt returns the MIME type for a given file extension.
func mimeTypeByExt(filename string) string {
	ct, _ := secutils.SafeContentTypeByFilename(filename)
	return ct
}

// PreviewKnowledgeFile godoc
func (h *KnowledgeHandler) PreviewKnowledgeFile(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	file, filename, err := h.kgService.GetKnowledgeFile(effCtx, id)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve file").WithDetails(err.Error()))
		return
	}
	defer file.Close()
	// ServeContent needs a seeker for byte ranges. Keep the temporary copy local
	// to this response; the stored source remains the authoritative original.
	tmp, err := os.CreateTemp("", "reliquary-preview-*")
	if err != nil {
		c.Error(errors.NewInternalServerError("Failed to prepare preview").WithDetails(err.Error()))
		return
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer tmp.Close()
	if _, err := io.Copy(tmp, file); err != nil {
		c.Error(errors.NewInternalServerError("Failed to prepare preview").WithDetails(err.Error()))
		return
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		c.Error(errors.NewInternalServerError("Failed to prepare preview").WithDetails(err.Error()))
		return
	}

	contentType, inline := secutils.SafeContentTypeByFilename(filename)
	c.Header("Content-Type", contentType)
	c.Header("X-Content-Type-Options", "nosniff")
	disposition := "inline"
	if !inline {
		disposition = "attachment"
	}
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": filename}))
	c.Header("Cache-Control", "private, max-age=3600")

	http.ServeContent(c.Writer, c.Request, filename, time.Now(), tmp)
}

// ReviewKnowledgeIntake retries a rejected intake after correcting source
// evidence, or removes it when the user discards it.
func (h *KnowledgeHandler) ReviewKnowledgeIntake(c *gin.Context) {
	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}
	_, ctx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
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
	knowledge, err := h.kgService.ReviewKnowledgeIntake(ctx, id, req.Action, req.SourceInfo)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": knowledge})
}

// GetKnowledgeBatchRequest defines parameters for batch knowledge retrieval
type GetKnowledgeBatchRequest struct {
	IDs []string `form:"ids" binding:"required"` // List of knowledge IDs
}

func (h *KnowledgeHandler) GetKnowledgeBatch(c *gin.Context) {
	ctx := c.Request.Context()

	spaceID, ok := c.Get(types.SpaceIDContextKey.String())
	if !ok {
		logger.Error(ctx, "Failed to get space ID")
		c.Error(errors.NewUnauthorizedError("Unauthorized"))
		return
	}
	effectiveSpaceID := spaceID.(uint64)

	var req GetKnowledgeBatchRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError("Invalid request parameters").WithDetails(err.Error()))
		return
	}

	_, kbID, _, err := h.validateKnowledgeBaseAccessWithKBID(c, "")
	if err != nil {
		c.Error(err)
		return
	}
	knowledges, err := h.kgService.GetKnowledgeBatch(ctx, effectiveSpaceID, req.IDs)
	knowledges = filterKnowledgesByKBAllowSet(knowledges, map[string]bool{kbID: true})

	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to retrieve knowledge list").WithDetails(err.Error()))
		return
	}

	logger.Infof(ctx, "Batch knowledge retrieval successful, requested count: %d, returned count: %d",
		len(req.IDs), len(knowledges))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledges,
	})
}

// UpdateKnowledgeRequest defines the partial-update body for PUT /knowledge/:id.
// Omitted fields are left unchanged; an explicit empty description clears the summary.
type UpdateKnowledgeRequest struct {
	Title          *string         `json:"title"`
	Description    *string         `json:"description"`
	CustomMetadata json.RawMessage `json:"custom_metadata"`
}

// UpdateKnowledge godoc
func (h *KnowledgeHandler) UpdateKnowledge(c *gin.Context) {
	ctx := c.Request.Context()

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	var req UpdateKnowledgeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse request parameters", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	knowledge := types.Knowledge{ID: id, CustomMetadata: types.JSON(req.CustomMetadata)}
	if req.Title != nil {
		knowledge.Title = *req.Title
	}
	if req.Description != nil {
		knowledge.Description = *req.Description
		knowledge.DescriptionSpecified = true
	}

	if err := h.kgService.UpdateKnowledge(effCtx, &knowledge); err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	updated, getErr := h.kgService.GetKnowledgeByID(effCtx, id)
	if getErr != nil {
		logger.Warnf(ctx, "Knowledge updated but failed to reload status for %s: %v", id, getErr)
	}

	logger.Infof(ctx, "Knowledge updated successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge updated successfully",
		"data":    updated,
	})
}

// UpdateManualKnowledge godoc
func (h *KnowledgeHandler) UpdateManualKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start updating manual knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	var req types.ManualKnowledgePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Error(ctx, "Failed to parse manual knowledge update request", err)
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}

	knowledge, err := h.kgService.UpdateManualKnowledge(effCtx, id, &req)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Manual knowledge updated successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data":    knowledge,
	})
}

// ReparseKnowledge godoc
func (h *KnowledgeHandler) ReparseKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start re-parsing knowledge")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Validate the knowledge base workspace before reparsing.
	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	// Optional per-reparse parse config override. Empty body keeps the
	// overrides stored at upload time.
	var processOverrides *types.KnowledgeProcessOverrides
	if c.Request.ContentLength != 0 {
		var req struct {
			ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			logger.Error(ctx, "Failed to parse reparse request body", err)
			c.Error(errors.NewBadRequestError("Invalid reparse request body").WithDetails(err.Error()))
			return
		}
		processOverrides = req.ProcessConfig
	}

	// Call service to reparse knowledge
	knowledge, err := h.kgService.ReparseKnowledge(effCtx, id, processOverrides)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge reparse task submitted successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge reparse task submitted",
		"data":    knowledge,
	})
}

// CancelKnowledgeParse godoc
func (h *KnowledgeHandler) CancelKnowledgeParse(c *gin.Context) {
	ctx := c.Request.Context()
	logger.Info(ctx, "Start cancelling knowledge parse")

	id := secutils.SanitizeForLog(c.Param("id"))
	if id == "" {
		logger.Error(ctx, "Knowledge ID is empty")
		c.Error(errors.NewBadRequestError("Knowledge ID cannot be empty"))
		return
	}

	// Validate the workspace as for reparsing and deleting knowledge.
	_, effCtx, err := h.resolveKnowledgeAndValidateKBAccess(c, id)
	if err != nil {
		c.Error(err)
		return
	}

	knowledge, err := h.kgService.CancelKnowledgeParse(effCtx, id)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, map[string]interface{}{
			"knowledge_id": id,
		})
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}

	logger.Infof(ctx, "Knowledge parse cancelled successfully, knowledge ID: %s", id)
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Knowledge parse cancelled",
		"data":    knowledge,
	})
}

func (h *KnowledgeHandler) SearchKnowledge(c *gin.Context) {
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

	knowledges, hasMore, total, err := h.kgService.SearchKnowledge(ctx, keyword, offset, limit, fileTypes)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError("Failed to search knowledge").WithDetails(err.Error()))
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"data":     knowledges,
		"has_more": hasMore,
		"total":    total,
	})
}

// parseFilterTime parses a query-string timestamp accepted by knowledge list
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

type batchReparseKnowledgeRequest struct {
	IDs           []string                         `json:"ids" binding:"required"`
	ProcessConfig *types.KnowledgeProcessOverrides `json:"process_config,omitempty"`
}

// BatchReparseKnowledge godoc
func (h *KnowledgeHandler) BatchReparseKnowledge(c *gin.Context) {
	ctx := c.Request.Context()
	var req batchReparseKnowledgeRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		logger.Errorf(ctx, "failed to parse batch reparse knowledge request: %v", err)
		c.Error(errors.NewBadRequestError("invalid batch reparse knowledge request parameters"))
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
		c.Error(errors.NewBadRequestError("no knowledge IDs provided for batch reparse"))
		return
	}
	const maxBatch = 200
	if len(ids) > maxBatch {
		c.Error(errors.NewBadRequestError(fmt.Sprintf("too many ids (max %d per batch)", maxBatch)))
		return
	}

	_, kbID, effectiveSpaceID, err := h.validateKnowledgeBaseAccessWithKBID(c, "")
	if err != nil {
		c.Error(err)
		return
	}

	ctx = context.WithValue(ctx, types.SpaceIDContextKey, effectiveSpaceID)

	knowledgeList, err := h.kgService.GetKnowledgeBatch(ctx, effectiveSpaceID, ids)
	if err != nil {
		logger.Errorf(ctx, "failed to get knowledge batch, kb_id: %s, size: %d, err: %v", kbID, len(ids), err)
		c.Error(errors.NewInternalServerError("failed to get knowledge batch"))
		return
	}
	if len(knowledgeList) != len(ids) {
		c.Error(errors.NewBadRequestError("some knowledge entries were not found"))
		return
	}
	for _, k := range knowledgeList {
		if k.KnowledgeBaseID != kbID {
			c.Error(errors.NewBadRequestError(
				fmt.Sprintf("Knowledge %s does not belong to knowledge base %s",
					secutils.SanitizeForLog(k.ID), secutils.SanitizeForLog(kbID))))
			return
		}
	}

	taskID, err := h.enqueueKnowledgeListReparse(ctx, effectiveSpaceID, ids, req.ProcessConfig)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue batch knowledge reparse task: %v", err)
		c.Error(errors.NewInternalServerError("Failed to enqueue batch reparse task"))
		return
	}

	logger.Infof(ctx, "Batch knowledge reparse task enqueued: %s, kb_id: %s, count: %d",
		taskID, secutils.SanitizeForLog(kbID), len(ids))

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Batch reparse task submitted",
		"data": gin.H{
			"task_id":       taskID,
			"reparse_count": len(ids),
		},
	})
}

func filterKnowledgesByKBAllowSet(knowledges []*types.Knowledge, allowed map[string]bool) []*types.Knowledge {
	if allowed == nil {
		return knowledges
	}
	filtered := make([]*types.Knowledge, 0, len(knowledges))
	for _, k := range knowledges {
		if k != nil && allowed[k.KnowledgeBaseID] {
			filtered = append(filtered, k)
		}
	}
	return filtered
}
