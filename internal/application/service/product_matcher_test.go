package service

import (
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

// Regression: partially overlapping names survive; invalid suffixes do not hide a product.
func TestProductMatchingKeepsOverlapsAndValidPrefixes(t *testing.T) {
	matcher, err := compileProductMatcher([]types.Entity{
		{ID: "line", CanonicalName: "HUAWEI Mate"},
		{ID: "model", CanonicalName: "Mate 60"},
		{ID: "pro", CanonicalName: "Mate 60 Pro"},
	})
	require.NoError(t, err)
	body := "产品 HUAWEI Mate 60。Mate 60 Pros。Mate 60 Pro。"
	evidence := matcher.evidence(body)
	require.Len(t, evidence["line"], 1)
	require.Len(t, evidence["model"], 2)
	require.Len(t, evidence["pro"], 1)
	for id, spans := range evidence {
		want := map[string]string{"line": "HUAWEI Mate", "model": "Mate 60", "pro": "Mate 60 Pro"}[id]
		for _, span := range spans {
			require.Equal(t, want, string([]rune(body)[span.Start:span.End]))
		}
	}
}
