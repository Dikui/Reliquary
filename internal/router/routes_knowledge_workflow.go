package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
)

func RegisterKnowledgeWorkflowRoutes(r *gin.RouterGroup, h *handler.KnowledgeWorkflowHandler, g *rbacGuards) {
	if h == nil {
		return
	}
	// Workflows can research and write knowledge. Access is limited to the KB
	// owner/admin; the service rechecks current membership for queued jobs too.
	w := g.apiKeyGroup(r.Group("/knowledge-bases/:id/workflows"), apiKeyFullAccess())
	w.grp.Use(g.Contributor(), g.KBAccessWrite("id"))
	w.GET("", h.List)
	w.POST("", h.Create)
	w.GET("/runs/:run_id", h.GetRun)
	w.POST("/runs/:run_id/decision", h.Decide)
	w.POST("/runs/:run_id/resume", h.Resume)
	w.POST("/runs/:run_id/retry", h.Retry)
	w.GET("/:workflow_id", h.Get)
	w.PUT("/:workflow_id", h.Update)
	w.DELETE("/:workflow_id", h.Delete)
	w.POST("/:workflow_id/run", h.Run)
	w.GET("/:workflow_id/runs", h.ListRuns)
	rp := g.apiKeyGroup(r.Group("/knowledge-bases/:id/reports"), apiKeyFullAccess())
	rp.grp.Use(g.Contributor(), g.KBAccessRead("id"))
	rp.GET("", h.ListReports)
	rp.GET("/:report_id", h.GetReport)
	rp.GET("/:report_id/download", h.DownloadReport)
}
