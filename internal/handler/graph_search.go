package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/graphreplica"
	"reliquary/internal/types"
)

type GraphSearchHandler struct{ service *service.GraphSearchService }

func NewGraphSearchHandler(s *service.GraphSearchService) *GraphSearchHandler {
	return &GraphSearchHandler{service: s}
}
func (h *GraphSearchHandler) Search(c *gin.Context) {
	var request types.GraphSearchRequest
	if err := decodeSearchJSON(c, &request); err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	out, err := h.service.Search(c.Request.Context(), request)
	if err != nil {
		if errors.Is(err, graphreplica.ErrUnavailable) {
			err = apperrors.NewServiceUnavailableError(graphreplica.ErrUnavailable.Error())
		}
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
