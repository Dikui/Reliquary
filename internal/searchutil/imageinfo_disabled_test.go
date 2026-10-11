package searchutil

import (
	"context"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type bodyImagesRepo struct {
	interfaces.ChunkRepository
	chunks   map[string]*types.Chunk
	children map[string][]*types.Chunk
}

func (r bodyImagesRepo) ListChunksByID(_ context.Context, _ uint64, ids []string) ([]*types.Chunk, error) {
	var out []*types.Chunk
	for _, id := range ids {
		if c := r.chunks[id]; c != nil {
			out = append(out, c)
		}
	}
	return out, nil
}
func (r bodyImagesRepo) ListChunksByParentIDs(_ context.Context, _ uint64, ids []string) ([]*types.Chunk, error) {
	var out []*types.Chunk
	for _, id := range ids {
		out = append(out, r.children[id]...)
	}
	return out, nil
}
func TestCollectBodyImageInfoSkipsDisabledChunks(t *testing.T) {
	repo := bodyImagesRepo{chunks: map[string]*types.Chunk{
		"hidden": {ID: "hidden", ChunkType: types.ChunkTypeText, ImageInfo: `[{"url":"hidden.png"}]`},
		"shown":  {ID: "shown", ChunkType: types.ChunkTypeText, IsEnabled: true, ImageInfo: `[{"url":"shown.png"}]`},
	}}
	got := CollectImageInfoByChunkIDs(context.Background(), repo, 1, []string{"hidden", "shown"})
	if len(got) != 1 || got["shown"] == "" {
		t.Fatalf("unexpected image references: %v", got)
	}
}
func TestCollectParentBodyImageInfoSkipsDisabledChildren(t *testing.T) {
	repo := bodyImagesRepo{
		chunks: map[string]*types.Chunk{"parent": {ID: "parent", ChunkType: types.ChunkTypeParentText, IsEnabled: true}},
		children: map[string][]*types.Chunk{"parent": {
			{ID: "hidden", ParentChunkID: "parent", ChunkType: types.ChunkTypeText, ImageInfo: `[{"url":"hidden.png"}]`},
			{ID: "shown", ParentChunkID: "parent", ChunkType: types.ChunkTypeText, IsEnabled: true, ImageInfo: `[{"url":"shown.png"}]`},
		}},
	}
	got := CollectImageInfoByChunkIDs(context.Background(), repo, 1, []string{"parent"})
	if got["parent"] != `[{"url":"shown.png","original_url":"","start_pos":0,"end_pos":0,"caption":"","ocr_text":""}]` {
		t.Fatalf("unexpected parent references: %v", got)
	}
}
