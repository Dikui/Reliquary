package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// VectorStoreHandler reports the read-only application PostgreSQL store.
type VectorStoreHandler struct{ service interfaces.VectorStoreService }

// NewVectorStoreHandler creates a handler for the application PostgreSQL store.
func NewVectorStoreHandler(_ interfaces.VectorStoreRepository, service interfaces.VectorStoreService) *VectorStoreHandler {
	return &VectorStoreHandler{service: service}
}

func (h *VectorStoreHandler) storeResponse(c *gin.Context) types.VectorStoreResponse {
	store := types.BuildEnvVectorStores("", nil)[0]
	version, err := h.service.TestConnection(c.Request.Context(), store.EngineType, store.ConnectionConfig)
	response := types.NewVectorStoreResponse(&store, types.StoreSourceEnv, true)
	response.Status = "available"
	if err != nil {
		response.Status = "unavailable"
	}
	response.ConnectionConfig.Version = version
	return response
}

// ListStores godoc
// @Summary      Get PostgreSQL store status
// @Description  Report the read-only application PostgreSQL store without exposing credentials
// @Tags         VectorStore
// @Produce      json
// @Success      200  {object}  map[string]interface{}  "PostgreSQL store status"
// @Failure      401  {object}  map[string]interface{}  "Unauthorized"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /vector-stores [get]
func (h *VectorStoreHandler) ListStores(c *gin.Context) {
	if c.GetUint64(types.TenantIDContextKey.String()) == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized: workspace context missing"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": []types.VectorStoreResponse{h.storeResponse(c)}})
}

// GetStore godoc
// @Summary      Get PostgreSQL store status
// @Description  Report the read-only application PostgreSQL store by its virtual ID
// @Tags         VectorStore
// @Produce      json
// @Param        id   path      string  true  "PostgreSQL store ID (__env_postgres__)"
// @Success      200  {object}  map[string]interface{}  "PostgreSQL store status"
// @Failure      401  {object}  map[string]interface{}  "Unauthorized"
// @Failure      404  {object}  map[string]interface{}  "Vector store not found"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /vector-stores/{id} [get]
func (h *VectorStoreHandler) GetStore(c *gin.Context) {
	if c.GetUint64(types.TenantIDContextKey.String()) == 0 {
		c.JSON(http.StatusUnauthorized, gin.H{"success": false, "error": "unauthorized: workspace context missing"})
		return
	}
	if c.Param("id") != "__env_postgres__" {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "vector store not found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": h.storeResponse(c)})
}
