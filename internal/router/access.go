package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/middleware"
	"reliquary/internal/types/interfaces"
)

type accessGuards struct {
	libraryService middleware.LibraryLookup
	entryService   middleware.EntryLookup
	chunkService   middleware.ChunkLookup
}

func newAccessGuards(library interfaces.LibraryService, entry interfaces.EntryService, chunks interfaces.ChunkService) *accessGuards {
	return &accessGuards{libraryService: library, entryService: entry, chunkService: chunks}
}
func (g *accessGuards) LibraryExists(param string) gin.HandlerFunc {
	return middleware.RequireLibraryExists(middleware.LibraryIDFromParam(param), g.libraryService)
}
func (g *accessGuards) LibraryExistsFromEntryIDParam(param string) gin.HandlerFunc {
	return middleware.RequireLibraryExists(middleware.LibraryIDFromEntryIDParam(param, g.entryService), g.libraryService)
}
func (g *accessGuards) LibraryExistsFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireLibraryExists(middleware.LibraryIDFromChunkIDParam(param, g.chunkService), g.libraryService)
}
