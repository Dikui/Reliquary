package types

import (
	"database/sql/driver"
	"encoding/json"
)

// RetrievalConfig holds the global retrieval/search configuration for a space.
// Entry retrieval uses these parameters before optional reranking.
//
// Stored as a JSONB column on the spaces table, managed via the settings UI
// at /space/kv/retrieval-config.
type RetrievalConfig struct {
	ImageVectorEnabled bool `json:"image_vector_enabled"`
	// EmbeddingTopK is the maximum number of chunks returned by vector search (default: 50)
	EmbeddingTopK int `json:"embedding_top_k"`
	// VectorThreshold is the minimum vector similarity score (0-1, default: 0.15)
	VectorThreshold float64 `json:"vector_threshold"`
	// KeywordThreshold is the minimum keyword match score (0-1, default: 0.3)
	KeywordThreshold float64 `json:"keyword_threshold"`
	// RerankTopK is the maximum number of results after reranking (default: 10)
	RerankTopK int `json:"rerank_top_k"`
	// RerankThreshold is the minimum rerank score (-10 to 10, default: 0.2)
	RerankThreshold float64 `json:"rerank_threshold"`

	// RRFK is the smoothing constant of Reciprocal Rank Fusion. Larger values
	// flatten the curve, reducing the bias towards top-1 results.
	// Default: 60. Sensible range: 30..100 depending on corpus size.
	RRFK int `json:"rrf_k,omitempty"`
	// RRFVectorWeight is the weight applied to the vector retriever inside RRF.
	// RRFVectorWeight + RRFKeywordWeight should usually sum to 1.0 but the math
	// works for any positive weights. Default: 0.7.
	RRFVectorWeight float64 `json:"rrf_vector_weight,omitempty"`
	// RRFKeywordWeight is the keyword counterpart. Default: 0.3.
	RRFKeywordWeight float64 `json:"rrf_keyword_weight,omitempty"`
	RRFImageWeight   float64 `json:"rrf_image_weight,omitempty"`
	// Similarity and publication distance are evaluated during each search.
	SimilarReportsThreshold  float64 `json:"similar_reports_threshold,omitempty"`
	SimilarReportsWindowDays int     `json:"similar_reports_window_days,omitempty"`
}

// GetEffectiveRRFImageWeight gives the image channel a modest recall boost.
// Its default is lower than either text vector channel because image similarity
// is often broader than a matching passage or article summary.
func (c *RetrievalConfig) GetEffectiveRRFImageWeight() float64 {
	if c == nil || c.RRFImageWeight <= 0 {
		return 0.2
	}
	return c.RRFImageWeight
}

func (c *RetrievalConfig) GetEffectiveSimilarReportsThreshold() float64 {
	if c == nil || c.SimilarReportsThreshold <= 0 || c.SimilarReportsThreshold > 1 {
		return 0.85
	}
	return c.SimilarReportsThreshold
}

func (c *RetrievalConfig) GetEffectiveSimilarReportsWindowDays() int {
	if c == nil || c.SimilarReportsWindowDays <= 0 {
		return 3
	}
	return c.SimilarReportsWindowDays
}

// DefaultRetrievalTopK is the retrieval depth used when a caller supplies no
// usable TopK / MatchCount. HybridSearch also floors its over-retrieval pool
// at this value, so the fallback and the pool it draws from stay in step by
// construction rather than by two independently maintained literals.
const DefaultRetrievalTopK = 50

// GetEffectiveEmbeddingTopK returns EmbeddingTopK with a fallback default.
func (c *RetrievalConfig) GetEffectiveEmbeddingTopK() int {
	if c == nil || c.EmbeddingTopK <= 0 {
		return DefaultRetrievalTopK
	}
	return c.EmbeddingTopK
}

// GetEffectiveVectorThreshold returns VectorThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveVectorThreshold() float64 {
	if c == nil || c.VectorThreshold <= 0 {
		return 0.15
	}
	return c.VectorThreshold
}

// GetEffectiveKeywordThreshold returns KeywordThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveKeywordThreshold() float64 {
	if c == nil || c.KeywordThreshold <= 0 {
		return 0.3
	}
	return c.KeywordThreshold
}

// GetEffectiveRerankTopK returns RerankTopK with a fallback default.
func (c *RetrievalConfig) GetEffectiveRerankTopK() int {
	if c == nil || c.RerankTopK <= 0 {
		return 10
	}
	return c.RerankTopK
}

// GetEffectiveRerankThreshold returns RerankThreshold with a fallback default.
func (c *RetrievalConfig) GetEffectiveRerankThreshold() float64 {
	if c == nil {
		return 0.2
	}
	return c.RerankThreshold
}

// GetEffectiveRRFK returns the RRF smoothing constant with a fallback default.
func (c *RetrievalConfig) GetEffectiveRRFK() int {
	if c == nil || c.RRFK <= 0 {
		return 60
	}
	return c.RRFK
}

// GetEffectiveRRFWeights returns vector / keyword weights with sensible defaults.
// When neither weight is set explicitly, returns 0.7 / 0.3.
func (c *RetrievalConfig) GetEffectiveRRFWeights() (vector, keyword float64) {
	if c == nil || (c.RRFVectorWeight == 0 && c.RRFKeywordWeight == 0) {
		return 0.7, 0.3
	}
	v := c.RRFVectorWeight
	k := c.RRFKeywordWeight
	if v <= 0 {
		v = 0.7
	}
	if k <= 0 {
		k = 0.3
	}
	return v, k
}

// Value implements the driver.Valuer interface for database serialization
func (c RetrievalConfig) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for database deserialization
func (c *RetrievalConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}
