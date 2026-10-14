package handler

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"path/filepath"
	"reliquary/internal/utils"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
)

type vaultDocuments interface {
	Status(context.Context) (service.VaultStatus, error)
	CreateReportRequest(context.Context, service.ReportInput) (*service.ReportWriteResult, error)
}

type VaultHandler struct {
	documents vaultDocuments
	articles  *service.VaultService
}

func (h *VaultHandler) SetArticles(v *service.VaultService) { h.articles = v }

func (h *VaultHandler) Attachment(c *gin.Context) {
	if h.articles == nil {
		c.Error(werrors.NewNotFoundError("vault attachment service unavailable"))
		return
	}
	file, err := h.articles.OpenAttachment(c.Request.Context(), c.Param("id"), c.Query("path"))
	if err != nil {
		c.Error(err)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		c.Error(err)
		return
	}
	name := filepath.Base(file.Name())
	contentType, inline := utils.SafeContentTypeByFilename(name)
	disposition := "attachment"
	if inline {
		disposition = "inline"
	}
	c.Header("Content-Type", contentType)
	c.Header("Content-Disposition", mime.FormatMediaType(disposition, map[string]string{"filename": name}))
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Cache-Control", "private, max-age=3600")
	http.ServeContent(c.Writer, c.Request, name, info.ModTime(), file)
}

func NewVaultHandler(documents *service.VaultDocumentService) *VaultHandler {
	return &VaultHandler{documents: documents}
}

func (h *VaultHandler) Status(c *gin.Context) {
	state, err := h.documents.Status(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": state})
}

func (h *VaultHandler) CreateReport(c *gin.Context) {
	input, err := service.DecodeReportInput(c.Request.Body)
	if err != nil {
		c.Error(err)
		return
	}
	report, err := h.documents.CreateReportRequest(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	status := http.StatusCreated
	if report.Existing {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"success": true, "data": report})
}

func (h *VaultHandler) RebuildFiles(c *gin.Context) {
	if h.articles == nil {
		c.Error(werrors.NewServiceUnavailableError("vault file rebuild unavailable"))
		return
	}
	state, err := h.articles.StartFileRebuild(c.Request.Context())
	if errors.Is(err, service.ErrVaultRebuildRunning) {
		c.Error(werrors.NewConflictError(err.Error()))
		return
	}
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": state})
}

func (h *VaultHandler) FileRebuildStatus(c *gin.Context) {
	if h.articles == nil {
		c.Error(werrors.NewServiceUnavailableError("vault file rebuild unavailable"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.articles.FileRebuildStatus()})
}
