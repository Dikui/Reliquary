package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"reliquary/internal/datasource"
	"reliquary/internal/handler/dto"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// DataSourceHandler handles HTTP requests for data source management
type DataSourceHandler struct {
	service   interfaces.DataSourceService
	kbService interfaces.KnowledgeBaseService
}

// NewDataSourceHandler creates a new data source handler
func NewDataSourceHandler(
	service interfaces.DataSourceService,
	kbService interfaces.KnowledgeBaseService,
) *DataSourceHandler {
	return &DataSourceHandler{
		service:   service,
		kbService: kbService,
	}
}

// getTenantID safely extracts and validates tenant ID from context
// Returns 0 if tenant ID is not found (caller should return 401)
func (h *DataSourceHandler) getTenantID(c *gin.Context) uint64 {
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	return tenantID
}

// Data source settings contain connector credentials, so only the owning tenant can access them.
func (h *DataSourceHandler) getOwnedKnowledgeBase(
	ctx context.Context,
	tenantID uint64,
	kbID string,
) (*types.KnowledgeBase, int, string) {
	if kbID == "" {
		return nil, http.StatusBadRequest, "kb_id is required"
	}

	kb, err := h.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil || kb == nil {
		return nil, http.StatusNotFound, "knowledge base not found"
	}

	if kb.TenantID != tenantID {
		return nil, http.StatusForbidden, "access denied"
	}

	return kb, http.StatusOK, ""
}

func (h *DataSourceHandler) getOwnedDataSource(
	ctx context.Context,
	tenantID uint64,
	id string,
) (*types.DataSource, int, string) {
	ds, err := h.service.GetDataSource(ctx, id)
	if err != nil {
		return nil, http.StatusNotFound, "data source not found"
	}

	if _, status, msg := h.getOwnedKnowledgeBase(ctx, tenantID, ds.KnowledgeBaseID); status != http.StatusOK {
		return nil, status, msg
	}

	return ds, http.StatusOK, ""
}

// CreateDataSource godoc
func (h *DataSourceHandler) CreateDataSource(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant ID from context (set by auth middleware)
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
		return
	}

	var req types.DataSource
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	if _, status, msg := h.getOwnedKnowledgeBase(ctx, tenantID, req.KnowledgeBaseID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	// Enforce tenant isolation
	req.TenantID = tenantID

	ds, err := h.service.CreateDataSource(ctx, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, dto.NewDataSourceResponse(ds))
}

// GetDataSource godoc
func (h *DataSourceHandler) GetDataSource(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	ds, status, msg := h.getOwnedDataSource(ctx, tenantID, id)
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, dto.NewDataSourceResponse(ds))
}

// ListDataSources godoc
func (h *DataSourceHandler) ListDataSources(c *gin.Context) {
	ctx := c.Request.Context()

	// Extract tenant ID from context
	tenantID := c.GetUint64(types.TenantIDContextKey.String())
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized: workspace context missing"})
		return
	}

	kbID := c.Query("kb_id")
	if _, status, msg := h.getOwnedKnowledgeBase(ctx, tenantID, kbID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}
	dataSources, err := h.service.ListDataSources(ctx, kbID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to list data sources"})
		return
	}

	if dataSources == nil {
		dataSources = make([]*types.DataSource, 0)
	}
	c.JSON(http.StatusOK, dto.NewDataSourceResponses(dataSources))
}

// UpdateDataSource godoc
func (h *DataSourceHandler) UpdateDataSource(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	var req types.DataSource
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	existing, status, msg := h.getOwnedDataSource(ctx, tenantID, id)
	if status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	req.ID = id
	req.TenantID = existing.TenantID
	req.KnowledgeBaseID = existing.KnowledgeBaseID
	ds, err := h.service.UpdateDataSource(ctx, &req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, dto.NewDataSourceResponse(ds))
}

// DeleteDataSource godoc
func (h *DataSourceHandler) DeleteDataSource(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	if err := h.service.DeleteDataSource(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to delete data source"})
		return
	}

	c.Status(http.StatusNoContent)
}

// ValidateConnection godoc
func (h *DataSourceHandler) ValidateConnection(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	if err := h.service.ValidateConnection(ctx, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "connected"})
}

// ValidateCredentials godoc
//
//	without creating or updating any database records.
//	Used by the frontend "Test Connection" button during data source creation.
func (h *DataSourceHandler) ValidateCredentials(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	var req struct {
		Type        string                 `json:"type" binding:"required"`
		Credentials map[string]interface{} `json:"credentials" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request: type and credentials are required"})
		return
	}

	if err := h.service.ValidateCredentials(ctx, req.Type, req.Credentials); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "connected"})
}

func (h *DataSourceHandler) ListAvailableResources(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")
	parentID := c.Query("parent_id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	resources, err := h.service.ListAvailableResources(ctx, id, parentID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if resources == nil {
		resources = make([]types.Resource, 0)
	}
	c.JSON(http.StatusOK, resources)
}

func (h *DataSourceHandler) ResolveResourceAncestors(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	var req resolveAncestorsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ancestors, err := h.service.ResolveResourceAncestors(ctx, id, req.ResourceIDs)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if ancestors == nil {
		ancestors = make([]string, 0)
	}
	c.JSON(http.StatusOK, gin.H{"ancestors": ancestors})
}

// resolveAncestorsRequest is the body for ResolveResourceAncestors.
type resolveAncestorsRequest struct {
	ResourceIDs []string `json:"resource_ids"`
}

// ManualSync godoc
func (h *DataSourceHandler) ManualSync(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	syncLog, err := h.service.ManualSync(ctx, id)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, syncLog)
}

// PauseDataSource godoc
func (h *DataSourceHandler) PauseDataSource(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	if err := h.service.PauseDataSource(ctx, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "paused"})
}

// ResumeDataSource godoc
func (h *DataSourceHandler) ResumeDataSource(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	if err := h.service.ResumeDataSource(ctx, id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "active"})
}

// GetSyncLogs godoc
func (h *DataSourceHandler) GetSyncLogs(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	id := c.Param("id")

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, id); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	limit := 10
	offset := 0

	if l := c.Query("limit"); l != "" {
		v, err := strconv.Atoi(l)
		if err != nil || v <= 0 || v > maxListPageSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": "limit must be between 1 and " + strconv.Itoa(maxListPageSize)})
			return
		}
		limit = v
	}

	if o := c.Query("offset"); o != "" {
		if v, err := strconv.Atoi(o); err == nil && v >= 0 {
			offset = v
		}
	}

	logs, err := h.service.GetSyncLogs(ctx, id, limit, offset)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if logs == nil {
		logs = make([]*types.SyncLog, 0)
	}
	c.JSON(http.StatusOK, logs)
}

// GetSyncLog godoc
func (h *DataSourceHandler) GetSyncLog(c *gin.Context) {
	ctx := c.Request.Context()
	tenantID := h.getTenantID(c)
	if tenantID == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}

	logID := c.Param("log_id")

	log, err := h.service.GetSyncLog(ctx, logID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "sync log not found"})
		return
	}

	if _, status, msg := h.getOwnedDataSource(ctx, tenantID, log.DataSourceID); status != http.StatusOK {
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, log)
}

// GetAvailableConnectors godoc
func (h *DataSourceHandler) GetAvailableConnectors(c *gin.Context) {
	connectors := datasource.ListAvailableConnectors()
	c.JSON(http.StatusOK, connectors)
}
