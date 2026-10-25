package router

import (
	"reliquary/internal/handler"

	"github.com/gin-gonic/gin"
)

func RegisterFollowRoutes(r *gin.RouterGroup, h *handler.FollowHandler) {
	follows := r.Group("/follows")
	follows.GET("", h.List)
	follows.POST("/feed", h.Feed)
	follows.PUT("/:type/:id", h.Follow)
	follows.DELETE("/:type/:id", h.Unfollow)
}
