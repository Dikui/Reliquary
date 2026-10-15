package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type FollowHandler struct{ service *service.FollowService }

func NewFollowHandler(s *service.FollowService) *FollowHandler { return &FollowHandler{service: s} }

func (h *FollowHandler) List(c *gin.Context) {
	items, err := h.service.ListFollows(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}

func (h *FollowHandler) Follow(c *gin.Context) {
	item, err := h.service.Follow(c.Request.Context(), c.Param("type"), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

func (h *FollowHandler) Unfollow(c *gin.Context) {
	if err := h.service.Unfollow(c.Request.Context(), c.Param("type"), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *FollowHandler) Feed(c *gin.Context) {
	var input types.FollowFeedRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid follow feed request"))
		return
	}
	result, err := h.service.Feed(c.Request.Context(), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *FollowHandler) State(c *gin.Context) {
	result, err := h.service.State(c.Request.Context(), followLibraries(c.Query("libraries"))...)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *FollowHandler) MarkViewed(c *gin.Context) {
	var input struct {
		ViewedAt  *time.Time `json:"viewed_at"`
		Libraries []string   `json:"libraries"`
	}
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&input); err != nil {
			c.Error(werrors.NewBadRequestError("viewed_at must be RFC3339"))
			return
		}
	}
	result, err := h.service.MarkViewed(c.Request.Context(), input.ViewedAt, input.Libraries...)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func followLibraries(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(value, ",")
}
