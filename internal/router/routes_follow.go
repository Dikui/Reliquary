package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterFollowRoutes(r *gin.RouterGroup, h *handler.FollowHandler) {
	follows := r.Group("/follows")
	follows.GET("", h.List)
	follows.GET("/state", h.State)
	follows.POST("/feed", h.Feed)
	follows.POST("/view", h.MarkViewed)
	follows.PUT("/:type/:id", h.Follow)
	follows.DELETE("/:type/:id", h.Unfollow)
}
