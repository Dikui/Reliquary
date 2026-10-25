package handler

import (
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/types"

	"github.com/gin-gonic/gin"
	werrors "reliquary/internal/errors"
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
