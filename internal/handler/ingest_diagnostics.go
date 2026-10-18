package handler

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/diagnostics"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"strconv"
	"strings"
	"time"
)

type IngestDiagnosticsHandler struct{ service *diagnostics.Service }

func (h *EntryHandler) SetIngestDiagnostics(s *diagnostics.Service) { h.ingestDiagnostics = s }

func (h *EntryHandler) uploadReadFailure(c *gin.Context, name string, data []byte, err error) {
	failure := apperrors.NewInternalServerError("Unable to read uploaded image")
	if h.ingestDiagnostics != nil {
		ref := h.ingestDiagnostics.RecordUploadFailure(c.Request.Context(), name, data, err, false)
		failure = failure.WithDetails(map[string]any{"diagnostics": ref})
	}
	c.Error(failure)
}

func NewIngestDiagnosticsHandler(s *diagnostics.Service) *IngestDiagnosticsHandler {
	return &IngestDiagnosticsHandler{service: s}
}
func diagnosticError(c *gin.Context, err error) {
	if errors.Is(err, diagnostics.ErrNotFound) {
		c.Error(apperrors.NewNotFoundError("Diagnostic material not found"))
		return
	}
	if errors.Is(err, diagnostics.ErrExpired) {
		c.Error(&apperrors.AppError{Code: apperrors.ErrNotFound, Message: "Diagnostic material expired", HTTPCode: http.StatusGone})
		return
	}
	c.Error(apperrors.NewInternalServerError("Diagnostic storage is unavailable"))
}
func (h *IngestDiagnosticsHandler) List(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	q := types.FailureQuery{APIKeyID: c.Query("api_key_id"), LibraryID: c.Query("library_id"), ErrorCode: c.Query("error_code"), RequestID: c.Query("request_id"), ID: c.Query("id")}
	for name, dest := range map[string]*int{"page": &q.Page, "page_size": &q.PageSize} {
		if raw := c.Query(name); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 1 {
				c.Error(apperrors.NewBadRequestError(name + " must be positive"))
				return
			}
			*dest = n
		}
	}
	for name, dest := range map[string]**time.Time{"from": &q.From, "to": &q.To} {
		if raw := c.Query(name); raw != "" {
			v, e := time.Parse(time.RFC3339, raw)
			if e != nil {
				c.Error(apperrors.NewBadRequestError(name + " must be RFC3339"))
				return
			}
			*dest = &v
		}
	}
	if q.PageSize > 100 || q.From != nil && q.To != nil && q.From.After(*q.To) {
		c.Error(apperrors.NewBadRequestError("invalid diagnostic pagination or time range"))
		return
	}
	data, err := h.service.List(c.Request.Context(), q)
	if err != nil {
		diagnosticError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
func (h *IngestDiagnosticsHandler) Get(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	data, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		diagnosticError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
func (h *IngestDiagnosticsHandler) Read(c *gin.Context) {
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; sandbox")
	offset, length := 0, 0
	for name, dest := range map[string]*int{"offset": &offset, "length": &length} {
		if raw := c.Query(name); raw != "" {
			n, e := strconv.Atoi(raw)
			if e != nil || n < 0 {
				c.Error(apperrors.NewBadRequestError(name + " must be nonnegative"))
				return
			}
			*dest = n
		}
	}
	if length > 100000 {
		c.Error(apperrors.NewBadRequestError("length must be at most 100000"))
		return
	}
	read, err := h.service.ReadText(c.Request.Context(), c.Param("id"), c.Param("material_id"), offset, length)
	if err != nil {
		if strings.Contains(err.Error(), "offset") {
			c.Error(apperrors.NewBadRequestError(err.Error()))
		} else {
			diagnosticError(c, err)
		}
		return
	}
	if strings.HasPrefix(read.Material.Type, "image/") {
		c.Data(http.StatusOK, read.Material.Type, read.Data)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": read})
}
