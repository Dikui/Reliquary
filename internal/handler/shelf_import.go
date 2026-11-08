package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntityHandler) ImportShelfWorkbook(c *gin.Context) {
	filename, data, err := readShelfWorkbookUpload(c)
	if err != nil {
		c.Error(err)
		return
	}
	var date *time.Time
	if raw := c.PostForm("file_date"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			c.Error(apperrors.NewBadRequestError("file_date must be RFC3339"))
			return
		}
		date = &parsed
	}
	result, err := h.service.ImportShelfWorkbook(c.Request.Context(), c.Param("id"), c.PostForm("plan_key"), filename, date, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) SuggestShelfImportPlan(c *gin.Context) {
	filename, data, err := readShelfWorkbookUpload(c)
	if err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.ShelfImportPlanSuggestion(c.Request.Context(), c.Param("id"), c.PostForm("plan_key"), c.PostForm("file_kind"), filename, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func readShelfWorkbookUpload(c *gin.Context) (string, []byte, error) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (32<<20)+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		return "", nil, apperrors.NewBadRequestError("invalid multipart workbook upload")
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		return "", nil, apperrors.NewBadRequestError("xlsx file is required")
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	return header.Filename, data, err
}

func (h *EntityHandler) SuggestShelfImportText(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	var input types.ShelfImportTextInput
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid import text suggestion request"))
		return
	}
	result, err := h.service.ShelfImportTextSuggestion(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}
