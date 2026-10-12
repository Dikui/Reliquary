package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/middleware"
	"reliquary/internal/types/interfaces"
)

type accessGuards struct {
	kbService    middleware.KBLookup
	entryService middleware.EntryLookup
	chunkService middleware.ChunkLookup
}

func newAccessGuards(kb interfaces.KnowledgeBaseService, entry interfaces.EntryService, chunks interfaces.ChunkService) *accessGuards {
	return &accessGuards{kbService: kb, entryService: entry, chunkService: chunks}
}
func (g *accessGuards) KBExists(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromParam(param), g.kbService)
}
func (g *accessGuards) KBExistsFromEntryIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromEntryIDParam(param, g.entryService), g.kbService)
}
func (g *accessGuards) KBExistsFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromChunkIDParam(param, g.chunkService), g.kbService)
}
