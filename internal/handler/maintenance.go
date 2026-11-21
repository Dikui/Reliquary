package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

type MaintenanceHandler struct{ db *gorm.DB }

func NewMaintenanceHandler(db *gorm.DB) *MaintenanceHandler { return &MaintenanceHandler{db: db} }

// BusinessWriteGate rejects writes before multipart uploads or JSON bodies are read.
// The service transaction repeats the check under the maintenance advisory lock.
func (h *MaintenanceHandler) BusinessWriteGate(c *gin.Context) {
	if err := maintenanceops.CheckBusinessWrite(c.Request.Context(), h.db); err != nil {
		c.Error(err)
		c.Abort()
		return
	}
	c.Next()
}

// BusinessWriteOperation also protects handler-owned queue operations, which
// can combine multiple commits with entry service calls.
func (h *MaintenanceHandler) BusinessWriteOperation(c *gin.Context) {
	request := c.Request
	if err := maintenanceops.WithBusinessWrite(request.Context(), h.db, func(ctx context.Context) error {
		c.Request = request.WithContext(ctx)
		defer func() { c.Request = request }()
		c.Next()
		return nil
	}); err != nil {
		c.Error(err)
		c.Abort()
	}
}

func (h *MaintenanceHandler) Status(c *gin.Context) {
	state, err := maintenanceops.Status(c.Request.Context(), h.db)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": state})
}

func (h *MaintenanceHandler) Configure(c *gin.Context) {
	principal, ok := types.PrincipalFromContext(c.Request.Context())
	if !ok || principal.Type != types.PrincipalWebUser {
		c.Error(apperrors.NewForbiddenError("Maintenance schedules require a signed-in user."))
		return
	}
	var input maintenanceops.ScheduleConfig
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(apperrors.NewBadRequestError("A maintenance schedule configuration is required."))
		return
	}
	if err := maintenanceops.Configure(c.Request.Context(), h.db, input); err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	h.Status(c)
}
