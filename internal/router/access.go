package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/middleware"
	"reliquary/internal/types/interfaces"
)

type accessGuards struct {
	kbService        middleware.KBLookup
	knowledgeService middleware.KnowledgeLookup
	chunkService     middleware.ChunkLookup
}

func newAccessGuards(kb interfaces.KnowledgeBaseService, knowledge interfaces.KnowledgeService, chunks interfaces.ChunkService) *accessGuards {
	return &accessGuards{kbService: kb, knowledgeService: knowledge, chunkService: chunks}
}
func (g *accessGuards) PathTenantMatch() gin.HandlerFunc {
	return middleware.RequirePathTenantMatch(nil)
}
func (g *accessGuards) KBAccessRead(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(middleware.KBIDFromParam(param), g.kbService)
}
func (g *accessGuards) KBAccessWrite(param string) gin.HandlerFunc { return g.KBAccessRead(param) }
func (g *accessGuards) KBAccessReadFromKnowledgeIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(middleware.KBIDFromKnowledgeIDParam(param, g.knowledgeService), g.kbService)
}
func (g *accessGuards) KBAccessWriteFromKnowledgeIDParam(param string) gin.HandlerFunc {
	return g.KBAccessReadFromKnowledgeIDParam(param)
}
func (g *accessGuards) KBAccessReadFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(middleware.KBIDFromChunkIDParam(param, g.chunkService), g.kbService)
}
func (g *accessGuards) KBAccessWriteFromChunkIDParam(param string) gin.HandlerFunc {
	return g.KBAccessReadFromChunkIDParam(param)
}
