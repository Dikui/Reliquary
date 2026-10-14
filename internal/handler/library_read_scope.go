package handler

import (
	"strings"

	"github.com/gin-gonic/gin"
	"reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntryHandler) applyLibraryReadScope(c *gin.Context) bool {
	raw, supplied := c.GetQuery("libraries")
	if !supplied {
		return true
	}
	ids := strings.Split(raw, ",")
	if len(ids) > 100 {
		c.Error(errors.NewBadRequestError("libraries accepts at most 100 IDs"))
		return false
	}
	for _, id := range ids {
		if strings.TrimSpace(id) == "" {
			c.Error(errors.NewBadRequestError("libraries cannot contain an empty ID"))
			return false
		}
		if _, _, _, err := h.validateLibraryAccessWithLibraryID(c, id); err != nil {
			c.Error(err)
			return false
		}
	}
	c.Request = c.Request.WithContext(types.WithLibraryReadScope(c.Request.Context(), ids))
	return true
}
