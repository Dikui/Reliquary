package search

import "reliquary/internal/types"

type Request struct {
	Query            string
	KnowledgeBaseIDs []string
	EntryIDs         []string
	SearchTargets    types.SearchTargets
	VectorThreshold  float64
	KeywordThreshold float64
	EmbeddingTopK    int
	RerankModelID    string
	RerankTopK       int
	RerankThreshold  float64
	SpaceID          uint64
}
type Results struct {
	RewriteQuery string
	SearchResult []*types.SearchResult
	RerankResult []*types.SearchResult
	MergeResult  []*types.SearchResult
}
type State struct {
	Request
	Results
}
type StageError struct {
	Err         error
	Description string
	ErrorType   string
}

func (e *StageError) WithError(err error) *StageError {
	return &StageError{Err: err, Description: e.Description, ErrorType: e.ErrorType}
}

var (
	ErrSearchNothing  = &StageError{Description: "No relevant content found", ErrorType: "search_nothing"}
	ErrSearch         = &StageError{Description: "Failed to search knowledge base", ErrorType: "search_failed"}
	ErrGetRerankModel = &StageError{Description: "Failed to get rerank model", ErrorType: "get_rerank_model_failed"}
)
