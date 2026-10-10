package types

import "time"

// ArticleSearchRequest is shared by HTTP and future external retrieval callers.
// Values within one filter are alternatives; different filters must all match.
type ArticleSearchRequest struct {
	Query           string   `json:"query"`
	ContentTypes    []string `json:"content_types,omitempty"`
	EntityIDs       []string `json:"entity_ids,omitempty"`
	TopicIDs        []string `json:"topic_ids,omitempty"`
	Sources         []string `json:"sources,omitempty"`
	PublishedFrom   string   `json:"published_from,omitempty"`
	PublishedTo     string   `json:"published_to,omitempty"`
	CollapseSimilar *bool    `json:"collapse_similar,omitempty"`
	Limit           int      `json:"limit,omitempty"`
}

type ArticleSearchEntity struct {
	ID            string   `json:"id"`
	CanonicalName string   `json:"canonical_name"`
	EntityType    string   `json:"entity_type"`
	Aliases       []string `json:"aliases" gorm:"-"`
}

type ArticleSearchTopic struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ArticleSearchSnippet struct {
	ChunkID    string  `json:"chunk_id"`
	Content    string  `json:"content"`
	ChunkIndex int     `json:"chunk_index"`
	Score      float64 `json:"score"`
	MatchType  string  `json:"match_type"`
}

type ArticleSearchResult struct {
	ArticleID      string                 `json:"article_id"`
	Title          string                 `json:"title"`
	Source         string                 `json:"source"`
	PublishedAt    *time.Time             `json:"published_at"`
	ContentType    string                 `json:"content_type"`
	ProcessingMode string                 `json:"processing_mode"`
	Summary        *KnowledgeSummary      `json:"summary"`
	Entities       []ArticleSearchEntity  `json:"entities"`
	Topics         []ArticleSearchTopic   `json:"topics"`
	Snippets       []ArticleSearchSnippet `json:"snippets"`
	Score          float64                `json:"score"`
	SimilarReports []*ArticleSearchResult `json:"similar_reports"`
	VaultPath      string                 `json:"vault_path"`
	OriginalURL    string                 `json:"original_url"`
}

type ArticleSearchResponse struct {
	Results                    []*ArticleSearchResult `json:"results"`
	ExcludedMissingPublishedAt int64                  `json:"excluded_missing_published_at"`
}

type ArticleSearchOptions struct {
	Entities []ArticleSearchEntity `json:"entities"`
	Topics   []ArticleSearchTopic  `json:"topics"`
}
