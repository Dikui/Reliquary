package handler

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/maintenanceops"
	"reliquary/internal/types"
)

type MaintenanceHandler struct{ db *gorm.DB }

func NewMaintenanceHandler(db *gorm.DB) *MaintenanceHandler { return &MaintenanceHandler{db: db} }

// CollectionGate rejects intake before multipart uploads or JSON bodies are read.
// The service transaction repeats this check under the collection advisory lock.
func (h *MaintenanceHandler) CollectionGate(c *gin.Context) {
	path := c.FullPath()
	intake := c.Request.Method == http.MethodPost && (path == "/api/v1/ingest" ||
		strings.HasPrefix(path, "/api/v1/knowledge-bases/:id/entries/") ||
		(path == "/api/v1/data-sources/:id/sync"))
	if intake {
		state, err := maintenanceops.Status(c.Request.Context(), h.db)
		if err != nil {
			c.Error(err)
			c.Abort()
			return
		}
		if state.Paused {
			c.Error(apperrors.NewServiceUnavailableError("Collection is paused for maintenance. Retry after maintenance completes."))
			c.Abort()
			return
		}
	}
	c.Next()
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
