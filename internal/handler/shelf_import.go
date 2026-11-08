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
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (32<<20)+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid multipart workbook upload"))
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError("xlsx file is required"))
		return
	}
	defer file.Close()
	var date *time.Time
	if raw := c.PostForm("file_date"); raw != "" {
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			c.Error(apperrors.NewBadRequestError("file_date must be RFC3339"))
			return
		}
		date = &parsed
	}
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.ImportShelfWorkbook(c.Request.Context(), c.Param("id"), c.PostForm("plan_key"), header.Filename, date, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) SuggestShelfImportPlan(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, (32<<20)+(1<<20))
	if err := c.Request.ParseMultipartForm(1 << 20); err != nil {
		c.Error(apperrors.NewBadRequestError("invalid multipart workbook upload"))
		return
	}
	defer c.Request.MultipartForm.RemoveAll()
	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.Error(apperrors.NewBadRequestError("xlsx file is required"))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
	if err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.ShelfImportPlanSuggestion(c.Request.Context(), c.Param("id"), c.PostForm("plan_key"), c.PostForm("file_kind"), header.Filename, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
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
