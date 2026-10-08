package types

// RetrieverEngineType represents the type of retriever engine
type RetrieverEngineType string

// RetrieverEngineType constants
const (
	PostgresRetrieverEngineType RetrieverEngineType = "postgres"
)

// RetrieverType represents the type of retriever
type RetrieverType string

// RetrieverType constants
const (
	KeywordsRetrieverType RetrieverType = "keywords" // Keywords retriever
	VectorRetrieverType   RetrieverType = "vector"   // Vector retriever
)

// RetrieveParams represents the parameters for retrieval
type RetrieveParams struct {
	// Query text
	Query string
	// Query embedding (used for vector retrieval)
	Embedding []float32
	// Knowledge base IDs
	KnowledgeBaseIDs []string
	// Knowledge IDs
	KnowledgeIDs []string
	TagIDs       []string
	// Excluded knowledge IDs
	ExcludeKnowledgeIDs []string
	// Excluded chunk IDs
	ExcludeChunkIDs []string
	// Number of results to return
	TopK int
	// Similarity threshold
	Threshold     float64
	KnowledgeType string
	// Additional parameters, different retrievers may require different parameters
	AdditionalParams map[string]interface{}
	// Retriever type
	RetrieverType RetrieverType // Retriever type
}

// IndexWithScore represents the index with score
type IndexWithScore struct {
	// ID
	ID string
	// Content
	Content string
	// Source ID
	SourceID string
	// Source type
	SourceType SourceType
	// Chunk ID
	ChunkID string
	// Knowledge ID
	KnowledgeID string
	// Knowledge base ID
	KnowledgeBaseID string
	// Tag ID
	TagID string
	// Score
	Score float64
	// Match type
	MatchType MatchType
	// IsEnabled
	IsEnabled bool
}

// GetScore returns the score for ScoreComparable interface
func (i *IndexWithScore) GetScore() float64 {
	return i.Score
}

// RetrieveResult represents the result of retrieval
type RetrieveResult struct {
	Results             []*IndexWithScore   // Retrieval results
	RetrieverEngineType RetrieverEngineType // Retrieval source type
	RetrieverType       RetrieverType       // Retrieval type
	Error               error               // Retrieval error
}
