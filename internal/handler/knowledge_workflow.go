package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type KnowledgeWorkflowHandler struct {
	service *service.KnowledgeWorkflowService
}

func NewKnowledgeWorkflowHandler(s *service.KnowledgeWorkflowService) *KnowledgeWorkflowHandler {
	return &KnowledgeWorkflowHandler{service: s}
}

func workflowResponse(c *gin.Context, data any, err error) {
	if err != nil {
		switch {
		case errors.Is(err, service.ErrWorkflowForbidden):
			err = werrors.NewForbiddenError("workflow access denied")
		case errors.Is(err, service.ErrWorkflowNotFound):
			err = werrors.NewNotFoundError("workflow or report not found")
		case errors.Is(err, service.ErrWorkflowInvalid):
			err = werrors.NewBadRequestError(err.Error())
		}
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *KnowledgeWorkflowHandler) List(c *gin.Context) {
	data, err := h.service.List(c.Request.Context(), c.Param("id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Create(c *gin.Context) {
	var in types.KnowledgeWorkflowInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.Error(werrors.NewBadRequestError("invalid workflow"))
		return
	}
	data, err := h.service.Create(c.Request.Context(), c.Param("id"), in)
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Update(c *gin.Context) {
	var in types.KnowledgeWorkflowInput
	if err := c.ShouldBindJSON(&in); err != nil {
		c.Error(werrors.NewBadRequestError("invalid workflow"))
		return
	}
	data, err := h.service.Update(c.Request.Context(), c.Param("id"), c.Param("workflow_id"), in)
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Get(c *gin.Context) {
	data, err := h.service.Get(c.Request.Context(), c.Param("id"), c.Param("workflow_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Delete(c *gin.Context) {
	err := h.service.Delete(c.Request.Context(), c.Param("id"), c.Param("workflow_id"))
	workflowResponse(c, nil, err)
}
func (h *KnowledgeWorkflowHandler) Run(c *gin.Context) {
	data, err := h.service.Run(c.Request.Context(), c.Param("id"), c.Param("workflow_id"), "manual")
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) ListRuns(c *gin.Context) {
	data, err := h.service.ListRuns(c.Request.Context(), c.Param("id"), c.Param("workflow_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) GetRun(c *gin.Context) {
	data, err := h.service.GetRun(c.Request.Context(), c.Param("id"), c.Param("run_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Resume(c *gin.Context) {
	data, err := h.service.Resume(c.Request.Context(), c.Param("id"), c.Param("run_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) Retry(c *gin.Context) {
	data, err := h.service.Retry(c.Request.Context(), c.Param("id"), c.Param("run_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) ListReports(c *gin.Context) {
	data, err := h.service.ListReports(c.Request.Context(), c.Param("id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) GetReport(c *gin.Context) {
	data, err := h.service.GetReport(c.Request.Context(), c.Param("id"), c.Param("report_id"))
	workflowResponse(c, data, err)
}
func (h *KnowledgeWorkflowHandler) DownloadReport(c *gin.Context) {
	report, err := h.service.GetReport(c.Request.Context(), c.Param("id"), c.Param("report_id"))
	if err != nil {
		workflowResponse(c, nil, err)
		return
	}
	format := c.DefaultQuery("format", "markdown")
	if format != "markdown" && format != "json" {
		c.Error(werrors.NewBadRequestError("format must be markdown or json"))
		return
	}
	content := []byte(report.Markdown)
	contentType, suffix := "text/markdown; charset=utf-8", "md"
	if format == "json" {
		content, err = json.MarshalIndent(report, "", "  ")
		if err != nil {
			workflowResponse(c, nil, err)
			return
		}
		contentType, suffix = "application/json; charset=utf-8", "json"
	}
	// IDs are generated UUIDs; the title stays inside the document, out of headers.
	filename := strings.ReplaceAll(report.ID, "\"", "") + "." + suffix
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Data(http.StatusOK, contentType, content)
}
