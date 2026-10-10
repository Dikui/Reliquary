package embedding

import "testing"

func TestEmbeddingDimensionsParamRequiresExplicitOverride(t *testing.T) {
	e := &OpenAIEmbedder{dimensions: 1024}
	e.SetSupportsDimensionOverride(false)
	if e.supportsDimensionsParam() {
		t.Fatal("supportsDimensionsParam() = true with override disabled")
	}
	e.SetSupportsDimensionOverride(true)
	if !e.supportsDimensionsParam() {
		t.Fatal("supportsDimensionsParam() = false with override enabled")
	}
}

func TestEmbeddingDimensionsParamRequiresPositiveDimension(t *testing.T) {
	e := &OpenAIEmbedder{dimensions: 0}
	e.SetSupportsDimensionOverride(true)
	if e.supportsDimensionsParam() {
		t.Fatal("expected dimensions param to be omitted when configured dimension is zero")
	}
}
