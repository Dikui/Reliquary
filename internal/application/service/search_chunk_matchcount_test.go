package service

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"reliquary/internal/types"
)

// TestNormalizedMatchCount pins the contract that a non-positive MatchCount
// resolves to the service-wide retrieval depth. The regression this guards is
// severe and silent: a caller that omitted match_count reached the truncation
// step with 0, which sliced the deduplicated chunk list to [:0] and turned a
// successful retrieval into an empty response. A negative value panicked on
// the same slice bound.
//
// The fallback shares DefaultRetrievalTopK with the over-retrieval floor on
// purpose: with no explicit MatchCount the `*5` amplification in HybridSearch
// collapses, so that floor alone decides the per-retriever depth and a larger
// truncation bound could never yield more results.
func TestNormalizedMatchCount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		requested int
		want      int
	}{
		{name: "omitted arrives as zero", requested: 0, want: types.DefaultRetrievalTopK},
		{name: "negative cannot index a slice", requested: -1, want: types.DefaultRetrievalTopK},
		{name: "explicit value is honored", requested: 3, want: 3},
		{name: "large explicit value is not clamped here", requested: 10000, want: 10000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, normalizedMatchCount(tt.requested))
		})
	}
}
