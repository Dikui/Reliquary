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
func (g *accessGuards) KBExists(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromParam(param), g.kbService)
}
func (g *accessGuards) KBExistsFromKnowledgeIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromKnowledgeIDParam(param, g.knowledgeService), g.kbService)
}
func (g *accessGuards) KBExistsFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBExists(middleware.KBIDFromChunkIDParam(param, g.chunkService), g.kbService)
}
