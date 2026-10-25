package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

// uploadReadFailure logs the failed upload without keeping its bytes.
func (h *EntryHandler) uploadReadFailure(c *gin.Context, name string, data []byte, err error) {
	logger.Warnf(c.Request.Context(), "ingest image upload read failed: file=%s bytes=%d error=%v", utils.SanitizeForLog(name), len(data), err)
	c.Error(werrors.NewInternalServerError("Unable to read uploaded image"))
}

func (h *EntryHandler) ingestService(c *gin.Context) interfaces.IngestService {
	collector, ok := h.kgService.(interfaces.IngestService)
	if !ok {
		c.Error(werrors.NewInternalServerError("ingest service unavailable"))
		return nil
	}
	return collector
}

func decodeIngestJSON(c *gin.Context, input any) bool {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil {
		c.Error(werrors.NewBadRequestError("请求格式无效：" + err.Error()))
		return false
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		c.Error(werrors.NewBadRequestError("请求必须只有一个 JSON 对象"))
		return false
	}
	return true
}

func (h *EntryHandler) Ingest(c *gin.Context) {
	input, err := types.DecodeIngestBatch(c.Request.Body)
	if err != nil {
		c.Error(werrors.NewBadRequestError(err.Error()))
		return
	}
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	result, err := collector.IngestBatch(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntryHandler) CheckIngestURLs(c *gin.Context) {
	var input types.IngestCheckRequest
	if !decodeIngestJSON(c, &input) {
		return
	}
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	items, err := collector.CheckURLs(c.Request.Context(), input.URLs, input.Library)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": types.IngestCheckResponse{Items: items}})
}

func (h *EntryHandler) UploadIngestAsset(c *gin.Context) {
	maxBytes := utils.GetMaxFileSizeMB() * 1024 * 1024
	limitUploadBody(c, maxBytes)
	defer func() {
		if c.Request.MultipartForm != nil {
			_ = c.Request.MultipartForm.RemoveAll()
		}
	}()
	part, err := c.FormFile("file")
	if err != nil {
		if isRequestBodyTooLarge(err) {
			c.Error(werrors.NewBadRequestError(fmt.Sprintf("文件大小不能超过%dMB", utils.GetMaxFileSizeMB())))
		} else {
			c.Error(werrors.NewBadRequestError("请提供 file 附件"))
		}
		return
	}
	if part.Size > maxBytes {
		c.Error(werrors.NewBadRequestError(fmt.Sprintf("文件大小不能超过%dMB", utils.GetMaxFileSizeMB())))
		return
	}
	if !docparser.IsImageFormat(filepath.Ext(part.Filename)) {
		c.Error(werrors.NewBadRequestError("only image attachments are accepted; convert the source to Markdown before ingestion"))
		return
	}
	file, err := part.Open()
	if err != nil {
		h.uploadReadFailure(c, part.Filename, nil, err)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		h.uploadReadFailure(c, part.Filename, data, err)
		return
	}
	if int64(len(data)) > maxBytes {
		c.Error(werrors.NewBadRequestError("附件超过大小上限"))
		return
	}
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	result, err := collector.UploadIngestAsset(c.Request.Context(), part.Filename, data)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}

func (h *EntryHandler) DiscardIngest(c *gin.Context) {
	var input struct {
		Reason string `json:"reason"`
	}
	if c.Request.ContentLength != 0 && !decodeIngestJSON(c, &input) {
		return
	}
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	if err := collector.DiscardIngest(c.Request.Context(), c.Param("id"), input.Reason); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"entry_id": c.Param("id"), "status": "discarded", "message": "已丢弃"}})
}

func (h *EntryHandler) DeleteDocument(c *gin.Context) {
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	if err := collector.DeleteDocument(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"document_id": c.Param("id"), "status": "deleted", "message": "本人文档已删除"}})
}

func (h *EntryHandler) UpdateEntryContent(c *gin.Context) {
	var input types.EntryContentUpdate
	c.Request.Body = struct {
		io.Reader
		io.Closer
	}{types.NewIngestJSONReader(c.Request.Body, types.MaxIngestItemBytes), c.Request.Body}
	if !decodeIngestJSON(c, &input) {
		return
	}
	collector := h.ingestService(c)
	if collector == nil {
		return
	}
	result, err := collector.UpdateEntryContent(c.Request.Context(), c.Param("id"), &input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
