package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

type GraphReplicaHandler struct {
	service *graphreplica.Service
}

func NewGraphReplicaHandler(service *graphreplica.Service) *GraphReplicaHandler {
	return &GraphReplicaHandler{service: service}
}
func (h *GraphReplicaHandler) Rebuild(c *gin.Context) {
	principal, ok := types.PrincipalFromContext(c.Request.Context())
	if !ok || principal.Type != types.PrincipalWebUser {
		c.Error(apperrors.NewForbiddenError("Graph rebuild requires a signed-in administrator."))
		return
	}
	info, err := h.service.EnqueueRebuild(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": gin.H{"operation_id": info.ID}})
}
