package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterKnowledgeWorkflowRoutes(r *gin.RouterGroup, h *handler.KnowledgeWorkflowHandler, g *accessGuards) {
	if h == nil {
		return
	}
	w := r.Group("/knowledge-bases/:id/workflows")
	w.Use(g.KBAccessWrite("id"))
	w.GET("", h.List)
	w.POST("", h.Create)
	w.GET("/runs/:run_id", h.GetRun)
	w.POST("/runs/:run_id/resume", h.Resume)
	w.POST("/runs/:run_id/retry", h.Retry)
	w.GET("/:workflow_id", h.Get)
	w.PUT("/:workflow_id", h.Update)
	w.DELETE("/:workflow_id", h.Delete)
	w.POST("/:workflow_id/run", h.Run)
	w.GET("/:workflow_id/runs", h.ListRuns)
	rp := r.Group("/knowledge-bases/:id/reports")
	rp.Use(g.KBAccessRead("id"))
	rp.GET("", h.ListReports)
	rp.GET("/:report_id", h.GetReport)
	rp.GET("/:report_id/download", h.DownloadReport)
}
