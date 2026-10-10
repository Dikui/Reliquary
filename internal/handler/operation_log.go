package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
)

type OperationLogHandler struct{ service *service.OperationLogService }

func NewOperationLogHandler(s *service.OperationLogService) *OperationLogHandler {
	return &OperationLogHandler{service: s}
}
func (h *OperationLogHandler) List(c *gin.Context) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "30"))
	if e1 != nil || e2 != nil {
		c.Error(werrors.NewBadRequestError("invalid pagination"))
		return
	}
	result, err := h.service.List(c.Request.Context(), c.Query("operation_type"), page, size)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
