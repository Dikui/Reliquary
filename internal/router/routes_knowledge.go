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
		kb.DELETE("", g.KBExists("id"), handler.ClearKnowledgeBaseContents)
	}

	// 知识路由组（URL :id is a knowledge id; the guard walks it to the parent KB）
	kgrp := r.Group("/knowledge")
	k := kgrp
	kRead := k
	{
		kRead.GET("/batch", handler.GetKnowledgeBatch)
		kRead.GET("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledge)
		kRead.GET("/:id/stages", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		kRead.GET("/:id/spans", g.KBExistsFromKnowledgeIDParam("id"), handler.GetKnowledgeSpans)
		k.DELETE("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.DeleteKnowledge)
		k.PUT("/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.UpdateKnowledge)
		k.POST("/:id/reprocess", g.KBExistsFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.PUT("/manual/:id", g.KBExistsFromKnowledgeIDParam("id"), handler.UpdateManualKnowledge)
		k.POST("/:id/reparse", g.KBExistsFromKnowledgeIDParam("id"), handler.ReparseKnowledge)
		k.POST("/:id/cancel-parse", g.KBExistsFromKnowledgeIDParam("id"), handler.CancelKnowledgeParse)
		k.POST("/:id/review", g.KBExistsFromKnowledgeIDParam("id"), handler.ReviewKnowledgeIntake)
		kRead.GET("/:id/download", g.KBExistsFromKnowledgeIDParam("id"), handler.DownloadKnowledgeFile)
		kRead.GET("/:id/preview", g.KBExistsFromKnowledgeIDParam("id"), handler.PreviewKnowledgeFile)
		kRead.GET("/search", handler.SearchKnowledge)
		k.POST("/batch-reparse", handler.BatchReparseKnowledge)
		k.POST("/batch-delete", handler.BatchDeleteKnowledge)
	}
}

// RegisterKnowledgeBaseRoutes exposes one settings resource and ID-based child resources.
func RegisterKnowledgeBaseRoutes(r *gin.RouterGroup, h *handler.KnowledgeBaseHandler, g *accessGuards) {
	r.GET("/knowledge-base", h.GetKnowledgeBase)
	r.PUT("/knowledge-base", h.UpdateKnowledgeBase)
	r.POST("/knowledge-bases/:id/hybrid-search", g.KBExists("id"), h.HybridSearch)
	r.GET("/knowledge-bases/:id/hybrid-search", g.KBExists("id"), h.HybridSearch)
}
