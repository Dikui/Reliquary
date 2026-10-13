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
	// 分块路由组，校验所属库与当前个人空间一致。
	chunks := r.Group("/chunks")
	chunkRead := chunks
	{
		chunkRead.GET("/:entry_id", g.LibraryExistsFromEntryIDParam("entry_id"), handler.ListEntryChunks)
		chunkRead.GET("/by-id/:id", g.LibraryExistsFromChunkIDParam("id"), handler.GetChunkByIDOnly)
	}
}

func RegisterEntryRoutes(r *gin.RouterGroup, handler *handler.EntryHandler, g *accessGuards) {
	r.POST("/ingest", handler.Ingest)
	r.POST("/ingest/check", handler.CheckIngestURLs)
	r.POST("/ingest/assets", handler.UploadIngestAsset)
	r.GET("/ingest/incomplete", handler.ListIncomplete)
	r.POST("/ingest/:id/accept-incomplete", g.LibraryExistsFromEntryIDParam("id"), handler.AcceptIncomplete)
	r.POST("/ingest/:id/discard", g.LibraryExistsFromEntryIDParam("id"), handler.DiscardIngest)
	r.DELETE("/documents/:id", g.LibraryExistsFromEntryIDParam("id"), handler.DeleteDocument)
	library := r.Group("/libraries/:id/entries")
	libraryRead := library
	{
		libraryRead.GET("", g.LibraryExists("id"), handler.ListEntry)
		library.DELETE("", g.LibraryExists("id"), handler.ClearLibraryContents)
	}

	// 条目路由组，按条目 ID 校验所属库。
	kgrp := r.Group("/entries")
	k := kgrp
	kRead := k
	{
		kRead.GET("/pending", handler.ListPending)
		kRead.GET("/batch", handler.GetEntryBatch)
		kRead.GET("/:id/body", g.LibraryExistsFromEntryIDParam("id"), handler.ReadEntryBody)
		kRead.GET("/:id", g.LibraryExistsFromEntryIDParam("id"), handler.GetEntry)
		kRead.GET("/:id/stages", g.LibraryExistsFromEntryIDParam("id"), handler.GetEntrySpans)
		kRead.GET("/:id/spans", g.LibraryExistsFromEntryIDParam("id"), handler.GetEntrySpans)
		k.DELETE("/:id", g.LibraryExistsFromEntryIDParam("id"), handler.DeleteEntry)
		k.PUT("/:id", g.LibraryExistsFromEntryIDParam("id"), handler.UpdateEntry)
		k.PUT("/:id/star", g.LibraryExistsFromEntryIDParam("id"), handler.SetStarred)
		k.PUT("/:id/content-type", g.LibraryExistsFromEntryIDParam("id"), handler.CorrectContentType)
		k.POST("/:id/reprocess", g.LibraryExistsFromEntryIDParam("id"), handler.ReparseEntry)
		k.POST("/:id/reparse", g.LibraryExistsFromEntryIDParam("id"), handler.ReparseEntry)
		k.POST("/:id/cancel-parse", g.LibraryExistsFromEntryIDParam("id"), handler.CancelEntryParse)
		k.POST("/:id/review", g.LibraryExistsFromEntryIDParam("id"), handler.ReviewEntryIntake)
		kRead.GET("/search", handler.SearchEntry)
		k.POST("/batch-reparse", handler.BatchReparseEntry)
		k.POST("/batch-delete", handler.BatchDeleteEntry)
	}
}

// RegisterLibraryRoutes exposes one settings resource and ID-based child resources.
func RegisterLibraryRoutes(r *gin.RouterGroup, h *handler.LibraryHandler, g *accessGuards) {
	r.GET("/library", h.GetLibrary)
	r.PUT("/library", h.UpdateLibrary)
	r.POST("/libraries/:id/hybrid-search", g.LibraryExists("id"), h.HybridSearch)
	r.GET("/libraries/:id/hybrid-search", g.LibraryExists("id"), h.HybridSearch)
}
