package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterChunkerDebugRoutes(r *gin.RouterGroup, g *accessGuards) {
	r.Handle(http.MethodPost, "/chunker/preview", handler.PreviewChunking)
}

func RegisterChunkRoutes(r *gin.RouterGroup, handler *handler.ChunkHandler, g *accessGuards) {
	// 分块路由组，校验所属知识库与当前个人空间一致。
	chunks := r.Group("/chunks")
	chunkRead := chunks
	{
		chunkRead.GET("/:knowledge_id", g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListKnowledgeChunks)
		chunkRead.GET("/by-id/:id", g.KBAccessReadFromChunkIDParam("id"), handler.GetChunkByIDOnly)
		chunkRead.GET("/:knowledge_id/:id/revisions", g.KBAccessReadFromKnowledgeIDParam("knowledge_id"), handler.ListChunkRevisions)
		chunks.DELETE("/:knowledge_id/:id", g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunk)
		chunks.DELETE("/:knowledge_id", g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunksByKnowledgeID)
		chunks.PUT("/:knowledge_id/:id", g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.UpdateChunk)
		chunks.POST("/:knowledge_id/:id/revert", g.KBAccessWriteFromKnowledgeIDParam("knowledge_id"), handler.RevertChunk)
		chunks.DELETE("/by-id/:id/questions", g.KBAccessWriteFromChunkIDParam("id"), handler.DeleteGeneratedQuestion)
		chunks.PUT("/by-id/:id/questions", g.KBAccessWriteFromChunkIDParam("id"), handler.UpsertGeneratedQuestion)
		chunks.POST("/by-id/:id/questions/regenerate", g.KBAccessWriteFromChunkIDParam("id"), handler.RegenerateGeneratedQuestions)
	}
}

func RegisterKnowledgeRoutes(r *gin.RouterGroup, handler *handler.KnowledgeHandler, g *accessGuards) {
	kb := r.Group("/knowledge-bases/:id/knowledge")
	kbRead := kb
	{
		kb.POST("/file", g.KBAccessWrite("id"), handler.CreateKnowledgeFromFile)
		kb.POST("/url", g.KBAccessWrite("id"), handler.CreateKnowledgeFromURL)
		kb.POST("/manual", g.KBAccessWrite("id"), handler.CreateManualKnowledge)
		kbRead.GET("", g.KBAccessRead("id"), handler.ListKnowledge)
		kbRead.GET("/folders", g.KBAccessRead("id"), handler.ListKnowledgeFolders)
		kb.PUT("/folders", g.KBAccessWrite("id"), handler.RenameKnowledgeFolder)
		kb.DELETE("", g.KBAccessWrite("id"), handler.ClearKnowledgeBaseContents)
	}

	// 知识路由组（URL :id is a knowledge id; the guard walks it to the parent KB）
	kgrp := r.Group("/knowledge")
	k := kgrp
	kRead := k
	{
		// Cross-knowledge endpoints (no :id) can't be gated on a single
		// KB via the URL — they accept a kb_id (or source/target KB) in the
		// body and the handler fans out the access check itself. /batch and
		// /search are read routes (retrieve). /move, /batch-delete,
		// /batch-reparse and /tags are content writes that each bound
		// themselves to a single (or source+target) KB and enforce the API
		// key's KB allow-list downstream — MoveKnowledge via
		// requireTenantAPIKeyKnowledgeBases(source,target); the batch ops via
		// validateKnowledgeBaseAccessWithKBID (requireTenantAPIKeyKnowledgeBase)
		// plus a per-item "belongs to the authorized KB" check in the handler
		// and service. They are therefore declared for API keys under the
		// ingest capability, matching their single-document siblings
		// (k.DELETE("/:id"), k.POST("/:id/reparse"), k.PUT("/:id")).
		kRead.GET("/batch", handler.GetKnowledgeBatch)
		kRead.GET("/:id", g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledge)
		kRead.GET("/:id/stages", g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		kRead.GET("/:id/spans", g.KBAccessReadFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		k.DELETE("/:id", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.DeleteKnowledge)
		k.PUT("/:id", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateKnowledge)
		k.POST("/:id/regenerate-summary", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.RegenerateKnowledgeSummary)
		k.PUT("/manual/:id", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateManualKnowledge)
		k.POST("/:id/reparse", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.POST("/:id/cancel-parse", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.CancelKnowledgeParse)
		k.POST("/:id/review", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.ReviewKnowledgeIntake)
		kRead.GET("/:id/download", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.DownloadKnowledgeFile)
		kRead.GET("/:id/preview", g.KBAccessReadFromKnowledgeIDParam("id"), handler.PreviewKnowledgeFile)
		k.PUT("/image/:id/:chunk_id", g.KBAccessWriteFromKnowledgeIDParam("id"), handler.UpdateImageInfo)
		kRead.GET("/search", handler.SearchKnowledge)
		kRead.GET("/move/progress/:task_id", handler.GetKnowledgeMoveProgress)
		k.PUT("/tags", handler.UpdateKnowledgeTagBatch)
		k.POST("/batch-reparse", handler.BatchReparseKnowledge)
		k.POST("/batch-delete", handler.BatchDeleteKnowledge)
		k.POST("/folder", handler.MoveKnowledgeToFolder)
		k.POST("/move", handler.MoveKnowledge)
	}
}

// RegisterKnowledgeBaseRoutes 注册知识库相关的路由
func RegisterKnowledgeBaseRoutes(r *gin.RouterGroup, handler *handler.KnowledgeBaseHandler, g *accessGuards) {
	kbgrp := r.Group("/knowledge-bases")
	kb := kbgrp
	kbManagement := kb
	{
		kbManagement.POST("", handler.CreateKnowledgeBase)
		kb.GET("", handler.ListKnowledgeBases)
		kb.GET("/:id", g.KBAccessRead("id"), handler.GetKnowledgeBase)
		kbManagement.PUT("/:id", g.KBAccessWrite("id"), handler.UpdateKnowledgeBase)
		kbManagement.DELETE("/:id", g.KBAccessWrite("id"), handler.DeleteKnowledgeBase)
		kb.PUT("/:id/pin", g.KBAccessRead("id"), handler.TogglePinKnowledgeBase)
		kb.POST("/:id/hybrid-search", g.KBAccessRead("id"), handler.HybridSearch)
		kb.GET("/:id/hybrid-search", g.KBAccessRead("id"), handler.HybridSearch)
		kbManagement.POST("/copy", handler.CopyKnowledgeBase)
		kbManagement.POST("/:id/duplicate", g.KBAccessRead("id"), handler.DuplicateKnowledgeBase)
		kb.
			GET("/copy/progress/:task_id", handler.GetKBCloneProgress)
		kb.GET("/:id/move-targets", g.KBAccessRead("id"), handler.ListMoveTargets)
	}
}

func RegisterKnowledgeAnomalyRoutes(r *gin.RouterGroup, h *handler.KnowledgeAnomalyHandler, g *accessGuards) {
	if h == nil {
		return
	}
	group := r.Group("/knowledge-bases/:id/anomalies")
	read := group
	read.GET("", g.KBAccessRead("id"), h.List)
	group.POST("", g.KBAccessRead("id"), h.Create)
	group.POST("/:anomaly_id/decision", g.KBAccessWrite("id"), h.Resolve)
}

func RegisterKnowledgeStrategyRoutes(r *gin.RouterGroup, h *handler.KnowledgeStrategyHandler, g *accessGuards) {
	if h == nil {
		return
	}
	// Strategy routes validate that the knowledge base belongs to the workspace.
	group := r.Group("/knowledge-bases/:id/strategies")
	group.GET("", g.KBAccessRead("id"), h.List)
	group.POST("", g.KBAccessWrite("id"), h.Propose)
	group.POST("/:version_id/activate", g.KBAccessWrite("id"), h.Activate)
	group.POST("/:version_id/evaluate", g.KBAccessWrite("id"), h.Evaluate)
	group.POST("/reprocess", g.KBAccessWrite("id"), h.Reprocess)
}

func RegisterKnowledgeTagRoutes(r *gin.RouterGroup, tagHandler *handler.TagHandler, g *accessGuards) {
	if tagHandler == nil {
		return
	}
	kbTags := r.Group("/knowledge-bases/:id/tags")
	kbTagsRead := kbTags
	{
		// KBAccessRead/Write resolve own/shared/agent-visible access and
		// rewrite the request's tenant context to the effective tenant
		// for the duration of the handler — so the handler no longer
		// needs its own effectiveCtxForKB helper.
		kbTagsRead.GET("", g.KBAccessRead("id"), tagHandler.ListTags)
		kbTags.POST("", g.KBAccessWrite("id"), tagHandler.CreateTag)
		kbTags.PUT("/:tag_id", g.KBAccessWrite("id"), tagHandler.UpdateTag)
		kbTags.DELETE("/:tag_id", g.KBAccessWrite("id"), tagHandler.DeleteTag)
	}
}

func RegisterWikiPageRoutes(r *gin.RouterGroup, wikiHandler *handler.WikiPageHandler, g *accessGuards) {
	wiki := r.Group("/knowledgebase/:kb_id/wiki")
	wikiRead := wiki
	{
		// Page CRUD
		wikiRead.GET("/pages", g.KBAccessRead("kb_id"), wikiHandler.ListPages)
		wiki.POST("/pages", g.KBAccessWrite("kb_id"), wikiHandler.CreatePage)
		wiki.PUT("/move-page", g.KBAccessWrite("kb_id"), wikiHandler.MovePage)
		wikiRead.GET("/pages/*slug", g.KBAccessRead("kb_id"), wikiHandler.GetPage)
		wiki.PUT("/pages/*slug", g.KBAccessWrite("kb_id"), wikiHandler.UpdatePage)
		wiki.DELETE("/pages/*slug", g.KBAccessWrite("kb_id"), wikiHandler.DeletePage)

		// Revision history (slug is a catch-all like /pages; revert carries
		// the slug in the body for the same reason move-page does)
		wikiRead.GET("/revisions/*slug", g.KBAccessRead("kb_id"), wikiHandler.ListRevisions)
		wiki.POST("/revert", g.KBAccessWrite("kb_id"), wikiHandler.RevertPage)
		wiki.POST("/source-changes/ignore", g.KBAccessWrite("kb_id"), wikiHandler.IgnoreSourceChanges)
		wiki.POST("/source-changes/regenerate", g.KBAccessWrite("kb_id"), wikiHandler.RegeneratePage)

		// Folder tree (directory nodes)
		wikiRead.GET("/folders", g.KBAccessRead("kb_id"), wikiHandler.ListFolders)
		wiki.POST("/folders", g.KBAccessWrite("kb_id"), wikiHandler.CreateFolder)
		wiki.PUT("/folders/:folder_id", g.KBAccessWrite("kb_id"), wikiHandler.UpdateFolder)
		wiki.DELETE("/folders/:folder_id", g.KBAccessWrite("kb_id"), wikiHandler.DeleteFolder)

		// Special pages
		wikiRead.GET("/index", g.KBAccessRead("kb_id"), wikiHandler.GetIndex)

		// Graph and stats
		wikiRead.GET("/graph", g.KBAccessRead("kb_id"), wikiHandler.GetGraph)
		wikiRead.GET("/stats", g.KBAccessRead("kb_id"), wikiHandler.GetStats)

		// Search and maintenance
		wikiRead.GET("/search", g.KBAccessRead("kb_id"), wikiHandler.SearchPages)
		wiki.POST("/rebuild-links", g.KBAccessWrite("kb_id"), wikiHandler.RebuildLinks)
		wikiRead.GET("/lint", g.KBAccessRead("kb_id"), wikiHandler.Lint)
		wiki.POST("/auto-fix", g.KBAccessWrite("kb_id"), wikiHandler.AutoFix)

		// Issues
		wikiRead.GET("/issues", g.KBAccessRead("kb_id"), wikiHandler.ListIssues)
		wiki.PUT("/issues/:issue_id/status", g.KBAccessWrite("kb_id"), wikiHandler.UpdateIssueStatus)
	}
}
