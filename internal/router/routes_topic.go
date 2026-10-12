package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterTopicRoutes(r *gin.RouterGroup, h *handler.TopicHandler, guards *accessGuards) {
	topics := r.Group("/topics")
	topics.GET("", h.List)
	topics.POST("", h.Create)
	topics.PUT("/:id", h.Update)
	topics.DELETE("/:id", h.Delete)
	topics.GET("/:id/backfill", h.Backfill)
	topics.POST("/:id/backfill/retry", h.RetryBackfill)
	r.PUT("/entries/:id/topics", guards.KBExistsFromEntryIDParam("id"), h.CorrectEntryTopics)
}
