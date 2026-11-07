package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntityHandler) ShelfFind(c *gin.Context) {
	limit, e1 := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, e2 := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if e1 != nil || e2 != nil {
		c.Error(apperrors.NewBadRequestError("invalid find pagination"))
		return
	}
	result, err := h.service.ShelfFind(c.Request.Context(), c.Param("id"), types.ShelfFindRequest{Query: c.Query("q"), Limit: limit, Offset: offset})
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfGet(c *gin.Context) {
	options := types.ShelfReadOptions{}
	if raw, ok := c.GetQuery("combination"); ok {
		if json.Unmarshal([]byte(raw), &options.Combination) != nil || options.Combination == nil {
			c.Error(apperrors.NewBadRequestError("combination must be a module-to-part object"))
			return
		}
	}
	if raw, ok := c.GetQuery("as_of"); ok {
		at, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil || at.IsZero() {
			c.Error(apperrors.NewBadRequestError("as_of must be an RFC3339 time"))
			return
		}
		options.AsOf = &at
	}
	result, err := h.service.ShelfGet(c.Request.Context(), c.Param("id"), c.Param("node_id"), options)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) ShelfQuery(c *gin.Context) {
	var input types.ShelfQueryRequest
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.ShelfQuery(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
