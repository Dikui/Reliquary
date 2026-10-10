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
		chunkRead.GET("/:knowledge_id", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.ListKnowledgeChunks)
		chunkRead.GET("/by-id/:id", g.KBExistsFromChunkIDParam("id"), handler.GetChunkByIDOnly)
		chunkRead.GET("/:knowledge_id/:id/revisions", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.ListChunkRevisions)
		chunks.DELETE("/:knowledge_id/:id", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunk)
		chunks.DELETE("/:knowledge_id", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.DeleteChunksByKnowledgeID)
		chunks.PUT("/:knowledge_id/:id", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.UpdateChunk)
		chunks.POST("/:knowledge_id/:id/revert", g.KBExistsFromKnowledgeIDParam("knowledge_id"), handler.RevertChunk)
		chunks.DELETE("/by-id/:id/questions", g.KBExistsFromChunkIDParam("id"), handler.DeleteGeneratedQuestion)
		chunks.PUT("/by-id/:id/questions", g.KBExistsFromChunkIDParam("id"), handler.UpsertGeneratedQuestion)
		chunks.POST("/by-id/:id/questions/regenerate", g.KBExistsFromChunkIDParam("id"), handler.RegenerateGeneratedQuestions)
	}
}

func RegisterKnowledgeRoutes(r *gin.RouterGroup, handler *handler.KnowledgeHandler, g *accessGuards) {
	kb := r.Group("/knowledge-bases/:id/knowledge")
	kbRead := kb
	{
		kb.POST("/file", g.KBExists("id"), handler.CreateKnowledgeFromFile)
		kb.POST("/url", g.KBExists("id"), handler.CreateKnowledgeFromURL)
		kb.POST("/manual", g.KBExists("id"), handler.CreateManualKnowledge)
		kbRead.GET("", g.KBExists("id"), handler.ListKnowledge)
		kbRead.GET("/folders", g.KBExists("id"), handler.ListKnowledgeFolders)
		kb.PUT("/folders", g.KBExists("id"), handler.RenameKnowledgeFolder)
		kb.DELETE("", g.KBExists("id"), handler.ClearKnowledgeBaseContents)
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
		kRead.GET("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledge)
		kRead.GET("/:id/stages", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		kRead.GET("/:id/spans", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		k.DELETE("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.DeleteKnowledge)
		k.PUT("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.UpdateKnowledge)
		k.POST("/:id/regenerate-summary", g.KBExistsFromKnowledgeIDParam("id"), handler.RegenerateKnowledgeSummary)
		k.PUT("/manual/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.UpdateManualKnowledge)
		k.POST("/:id/reparse", g.KBExistsFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.POST("/:id/cancel-parse", g.KBExistsFromKnowledgeIDParam("id"), handler.CancelKnowledgeParse)
		k.POST("/:id/review", g.KBExistsFromKnowledgeIDParam("id"), handler.ReviewKnowledgeIntake)
		kRead.GET("/:id/download", g.KBExistsFromKnowledgeIDParam("id"), handler.DownloadKnowledgeFile)
		kRead.GET("/:id/preview", g.KBExistsFromKnowledgeIDParam("id"), handler.PreviewKnowledgeFile)
		k.PUT("/image/:id/:chunk_id", g.KBExistsFromKnowledgeIDParam("id"), handler.UpdateImageInfo)
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
		kb.GET("/:id", g.KBExists("id"), handler.GetKnowledgeBase)
		kbManagement.PUT("/:id", g.KBExists("id"), handler.UpdateKnowledgeBase)
		kbManagement.DELETE("/:id", g.KBExists("id"), handler.DeleteKnowledgeBase)
		kb.PUT("/:id/pin", g.KBExists("id"), handler.TogglePinKnowledgeBase)
		kb.POST("/:id/hybrid-search", g.KBExists("id"), handler.HybridSearch)
		kb.GET("/:id/hybrid-search", g.KBExists("id"), handler.HybridSearch)
		kbManagement.POST("/:id/duplicate", g.KBExists("id"), handler.DuplicateKnowledgeBase)
		kb.GET("/:id/move-targets", g.KBExists("id"), handler.ListMoveTargets)
	}
}

func RegisterKnowledgeAnomalyRoutes(r *gin.RouterGroup, h *handler.KnowledgeAnomalyHandler, g *accessGuards) {
	if h == nil {
		return
	}
	group := r.Group("/knowledge-bases/:id/anomalies")
	read := group
	read.GET("", g.KBExists("id"), h.List)
	group.POST("", g.KBExists("id"), h.Create)
	group.POST("/:anomaly_id/decision", g.KBExists("id"), h.Resolve)
}

func RegisterKnowledgeStrategyRoutes(r *gin.RouterGroup, h *handler.KnowledgeStrategyHandler, g *accessGuards) {
	if h == nil {
		return
	}
	// Strategy routes validate that the knowledge base belongs to the workspace.
	group := r.Group("/knowledge-bases/:id/strategies")
	group.GET("", g.KBExists("id"), h.List)
	group.POST("", g.KBExists("id"), h.Propose)
	group.POST("/:version_id/activate", g.KBExists("id"), h.Activate)
	group.POST("/:version_id/evaluate", g.KBExists("id"), h.Evaluate)
	group.POST("/reprocess", g.KBExists("id"), h.Reprocess)
}

func RegisterKnowledgeTagRoutes(r *gin.RouterGroup, tagHandler *handler.TagHandler, g *accessGuards) {
	if tagHandler == nil {
		return
	}
	kbTags := r.Group("/knowledge-bases/:id/tags")
	kbTagsRead := kbTags
	{
		// Check the knowledge base exists before looking up or changing its tags.
		kbTagsRead.GET("", g.KBExists("id"), tagHandler.ListTags)
		kbTags.POST("", g.KBExists("id"), tagHandler.CreateTag)
		kbTags.PUT("/:tag_id", g.KBExists("id"), tagHandler.UpdateTag)
		kbTags.DELETE("/:tag_id", g.KBExists("id"), tagHandler.DeleteTag)
	}
}
