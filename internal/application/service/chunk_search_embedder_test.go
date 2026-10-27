package service

import (
	"context"

	"reliquary/internal/models/embedding"
)

// chunkSearchEmbedder is the deterministic three-dimension embedder shared by
// the retrieval fixtures.
type chunkSearchEmbedder struct{}

func (chunkSearchEmbedder) GetDimensions() int   { return 3 }
func (chunkSearchEmbedder) GetModelID() string   { return "chunk-search-fixed" }
func (chunkSearchEmbedder) GetModelName() string { return "chunk-search-fixed" }
func (chunkSearchEmbedder) Embed(context.Context, string) ([]float32, error) {
	return []float32{1, 0, 0}, nil
}
func (e chunkSearchEmbedder) BatchEmbed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, len(texts))
	for i, text := range texts {
		vector, err := e.Embed(ctx, text)
		if err != nil {
			return nil, err
		}
		vectors[i] = vector
	}
	return vectors, nil
}
func (e chunkSearchEmbedder) BatchEmbedWithPool(ctx context.Context, _ embedding.Embedder, texts []string) ([][]float32, error) {
	return e.BatchEmbed(ctx, texts)
}
