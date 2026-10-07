package retriever

import (
	"context"
	"math"

	"reliquary/internal/types"
)

// ScoreNormalizer maps raw retriever scores to a common [0, 1] scale so that
// vector scores produced by PostgreSQL stores can be compared in a single
// ranked list. Implementations MUST be safe for concurrent use and MUST be
// IO-free (Normalize is called inside a hot loop and may not log or block).
//
// Only vector scores are normalized. Keyword (BM25) scores have an unbounded
// positive range; rescaling them would collapse the long tail. Downstream
// RRF fusion is rank-based and immune to scale, so keyword scores pass
// through unchanged.
type ScoreNormalizer interface {
	Normalize(
		ctx context.Context,
		score float64,
		retrieverType types.RetrieverType,
		engineType types.RetrieverEngineType,
	) float64
}

// EngineAwareNormalizer clamps PostgreSQL vector scores to [0, 1].
// Keyword scores pass through unchanged for rank-based fusion.
type EngineAwareNormalizer struct{}

// Compile-time interface satisfaction assertion.
var _ ScoreNormalizer = EngineAwareNormalizer{}

// Normalize implements ScoreNormalizer.
func (EngineAwareNormalizer) Normalize(_ context.Context, score float64, retrieverType types.RetrieverType, _ types.RetrieverEngineType) float64 {
	if retrieverType != types.VectorRetrieverType {
		return score
	}
	return clamp01(score)
}

// clamp01 maps scores, including NaN and infinities, into a sortable [0, 1] range.
func clamp01(s float64) float64 {
	if math.IsNaN(s) {
		return 0
	}
	if s <= 0 || math.IsInf(s, -1) {
		return 0
	}
	if s >= 1 || math.IsInf(s, 1) {
		return 1
	}
	return s
}
