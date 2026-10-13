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
		chunkRead.GET("/:entry_id", g.KBExistsFromEntryIDParam("entry_id"), handler.ListEntryChunks)
		chunkRead.GET("/by-id/:id", g.KBExistsFromChunkIDParam("id"), handler.GetChunkByIDOnly)
	}
}

func RegisterEntryRoutes(r *gin.RouterGroup, handler *handler.EntryHandler, g *accessGuards) {
	r.POST("/ingest", handler.Ingest)
	r.POST("/ingest/check", handler.CheckIngestURLs)
	r.POST("/ingest/assets", handler.UploadIngestAsset)
	r.GET("/ingest/incomplete", handler.ListIncomplete)
	r.POST("/ingest/:id/accept-incomplete", g.KBExistsFromEntryIDParam("id"), handler.AcceptIncomplete)
	r.POST("/ingest/:id/discard", g.KBExistsFromEntryIDParam("id"), handler.DiscardIngest)
	r.DELETE("/documents/:id", g.KBExistsFromEntryIDParam("id"), handler.DeleteDocument)
	kb := r.Group("/knowledge-bases/:id/entries")
	kbRead := kb
	{
		kbRead.GET("", g.KBExists("id"), handler.ListEntry)
		kb.DELETE("", g.KBExists("id"), handler.ClearKnowledgeBaseContents)
	}

	// 条目路由组，按条目 ID 校验所属知识库。
	kgrp := r.Group("/entries")
	k := kgrp
	kRead := k
	{
		kRead.GET("/pending", handler.ListPending)
		kRead.GET("/batch", handler.GetEntryBatch)
		kRead.GET("/:id", g.KBExistsFromEntryIDParam("id"), handler.GetEntry)
		kRead.GET("/:id/stages", g.KBExistsFromEntryIDParam("id"), handler.GetEntrySpans)
		kRead.GET("/:id/spans", g.KBExistsFromEntryIDParam("id"), handler.GetEntrySpans)
		k.DELETE("/:id", g.KBExistsFromEntryIDParam("id"), handler.DeleteEntry)
		k.PUT("/:id", g.KBExistsFromEntryIDParam("id"), handler.UpdateEntry)
		k.PUT("/:id/star", g.KBExistsFromEntryIDParam("id"), handler.SetStarred)
		k.PUT("/:id/content-type", g.KBExistsFromEntryIDParam("id"), handler.CorrectContentType)
		k.POST("/:id/reprocess", g.KBExistsFromEntryIDParam("id"), handler.ReparseEntry)
		k.POST("/:id/reparse", g.KBExistsFromEntryIDParam("id"), handler.ReparseEntry)
		k.POST("/:id/cancel-parse", g.KBExistsFromEntryIDParam("id"), handler.CancelEntryParse)
		k.POST("/:id/review", g.KBExistsFromEntryIDParam("id"), handler.ReviewEntryIntake)
		k.POST("/:id/processing-review", g.KBExistsFromEntryIDParam("id"), handler.ResolveProcessingReview)
		kRead.GET("/:id/download", g.KBExistsFromEntryIDParam("id"), handler.DownloadEntryFile)
		kRead.GET("/:id/preview", g.KBExistsFromEntryIDParam("id"), handler.PreviewEntryFile)
		kRead.GET("/search", handler.SearchEntry)
		k.POST("/batch-reparse", handler.BatchReparseEntry)
		k.POST("/batch-delete", handler.BatchDeleteEntry)
	}
}

// RegisterKnowledgeBaseRoutes exposes one settings resource and ID-based child resources.
func RegisterKnowledgeBaseRoutes(r *gin.RouterGroup, h *handler.KnowledgeBaseHandler, g *accessGuards) {
	r.GET("/knowledge-base", h.GetKnowledgeBase)
	r.PUT("/knowledge-base", h.UpdateKnowledgeBase)
	r.POST("/knowledge-bases/:id/hybrid-search", g.KBExists("id"), h.HybridSearch)
	r.GET("/knowledge-bases/:id/hybrid-search", g.KBExists("id"), h.HybridSearch)
}
