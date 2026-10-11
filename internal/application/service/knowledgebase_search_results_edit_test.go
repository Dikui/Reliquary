package service

import (
	"testing"

	"reliquary/internal/types"
)

func TestIsSearchableChunkSkipsDisabledChunk(t *testing.T) {
	service := &knowledgeBaseService{}
	chunk := &types.Chunk{
		ChunkType: types.ChunkTypeText,
		IsEnabled: false,
	}
	if service.isSearchableChunk(chunk) {
		t.Fatal("disabled document chunk should never be searchable")
	}
}
