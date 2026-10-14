package handler

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/modelcalls"
	"reliquary/internal/types"
)

func modelCallFilter(c *gin.Context) (modelcalls.Filter, error) {
	f := modelcalls.Filter{SpaceID: types.MustSpaceIDFromContext(c.Request.Context()), ModelID: c.Query("model_id"), EntryID: c.Query("entry_id"), Purpose: types.ModelCallPurpose(c.Query("purpose"))}
	for key, dest := range map[string]**time.Time{"from": &f.From, "to": &f.To} {
		if raw := c.Query(key); raw != "" {
			t, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				return f, apperrors.NewBadRequestError(key + " must be RFC3339")
			}
			*dest = &t
		}
	}
	return f, f.Validate()
}
func (h *ModelHandler) ModelCallSummary(c *gin.Context) { h.modelCallSummary(c, false) }
func (h *ModelHandler) ModelCallTrend(c *gin.Context)   { h.modelCallSummary(c, true) }
func (h *ModelHandler) modelCallSummary(c *gin.Context, trend bool) {
	if h.db == nil {
		c.Error(apperrors.NewServiceUnavailableError("accounting database unavailable"))
		return
	}
	f, err := modelCallFilter(c)
	if err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	period := c.DefaultQuery("period", "day")
	groups := []string{"model", "purpose"}
	if raw, ok := c.GetQuery("group_by"); ok {
		groups = strings.Split(raw, ",")
	}
	if trend {
		period = "day"
		groups = nil
	}
	rows, err := modelcalls.NewStore(h.db).Summary(c.Request.Context(), f, period, groups)
	if err != nil {
		if strings.Contains(err.Error(), "must") {
			c.Error(apperrors.NewBadRequestError(err.Error()))
		} else {
			c.Error(apperrors.NewInternalServerError(err.Error()))
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "timezone": "UTC"})
}
func (h *ModelHandler) ListModelCalls(c *gin.Context) {
	if h.db == nil {
		c.Error(apperrors.NewServiceUnavailableError("accounting database unavailable"))
		return
	}
	f, err := modelCallFilter(c)
	if err != nil {
		c.Error(apperrors.NewBadRequestError(err.Error()))
		return
	}
	if id := c.Param("id"); id != "" {
		f.EntryID = id
	}
	limit, err := strconv.Atoi(c.DefaultQuery("limit", "100"))
	if err != nil || limit < 1 || limit > 1000 {
		c.Error(apperrors.NewBadRequestError("limit must be 1 to 1000"))
		return
	}
	offset, err := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || offset < 0 {
		c.Error(apperrors.NewBadRequestError("offset must be nonnegative"))
		return
	}
	rows, total, err := modelcalls.NewStore(h.db).List(c.Request.Context(), f, limit, offset)
	if err != nil {
		c.Error(apperrors.NewInternalServerError(err.Error()))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows, "total": total, "limit": limit, "offset": offset})
}
func (h *ModelHandler) ModelCallPurposes(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"success": true, "data": types.ModelCallPurposes})
}
