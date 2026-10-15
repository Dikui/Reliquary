package config

import "testing"

func TestProcessingConfigRejectsNegativeLimits(t *testing.T) {
	for _, limits := range []ArticleProcessingConfig{{RelationTypeSimilarityThreshold: -0.1}, {RelationTypeSimilarityThreshold: 1.1}, {MaxBodyChars: -1}, {MaxSegments: -1}, {SegmentOverlapChars: -1}, {ContextReservePercent: -1}, {ContextReservePercent: 91}, {MaxImages: -1}, {MaxCompletionTokens: -1}} {
		if err := ValidateConfig(&Config{ArticleProcessing: &limits}); err == nil {
			t.Fatalf("negative processing limit accepted: %+v", limits)
		}
	}
	if err := ValidateConfig(&Config{ArticleProcessing: &ArticleProcessingConfig{}}); err != nil {
		t.Fatalf("automatic defaults rejected: %v", err)
	}
}
