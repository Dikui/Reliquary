package types

import (
	"encoding/json"
	"time"
)

// EntrySearchRequest is shared by HTTP and future external retrieval callers.
// Values within one filter are alternatives; different filters must all match.
type EntrySearchRequest struct {
	VectorSet       string   `json:"vector_set,omitempty"`
	Libraries       []string `json:"libraries"`
	Query           string   `json:"query"`
	ContentTypes    []string `json:"content_types,omitempty"`
	EntityIDs       []string `json:"entity_ids,omitempty"`
	TopicIDs        []string `json:"topic_ids,omitempty"`
	Sources         []string `json:"sources,omitempty"`
	Starred         *bool    `json:"starred,omitempty"`
	PublishedFrom   string   `json:"published_from,omitempty"`
	PublishedTo     string   `json:"published_to,omitempty"`
	CollapseSimilar *bool    `json:"collapse_similar,omitempty"`
	Limit           int      `json:"limit,omitempty"`
}

type EntrySearchEntity struct {
	ID            string   `json:"id"`
	CanonicalName string   `json:"canonical_name"`
	EntityType    string   `json:"entity_type"`
	Aliases       []string `json:"aliases" gorm:"-"`
}

type EntrySearchTopic struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type EntrySearchSnippet struct {
	ChunkType  string           `json:"chunk_type,omitempty"`
	Locations  []SourceLocation `json:"locations"`
	ImagePath  string           `json:"image_path,omitempty"`
	ChunkID    string           `json:"chunk_id"`
	Content    string           `json:"content"`
	ChunkIndex int              `json:"chunk_index"`
	Score      float64          `json:"score"`
	MatchType  string           `json:"match_type"`
}

type EntrySearchResult struct {
	LibraryID        string               `json:"library_id"`
	LibraryName      string               `json:"library_name"`
	LibraryDirectory string               `json:"library_directory"`
	SourceRef        string               `json:"source_ref,omitempty"`
	Identifier       string               `json:"identifier,omitempty"`
	HasUpdate        bool                 `json:"has_update"`
	LatestEntryID    string               `json:"latest_entry_id,omitempty"`
	EntryID          string               `json:"entry_id"`
	Title            string               `json:"title"`
	Source           string               `json:"source"`
	PublishedAt      *time.Time           `json:"published_at"`
	ContentType      string               `json:"content_type"`
	Acquisition      string               `json:"acquisition"`
	CannotBeFact     bool                 `json:"cannot_be_fact,omitempty"`
	SourceUnverified bool                 `json:"source_unverified"`
	Starred          bool                 `json:"starred"`
	Summary          *EntrySummary        `json:"summary"`
	Entities         []EntrySearchEntity  `json:"entities"`
	Topics           []EntrySearchTopic   `json:"topics"`
	Snippets         []EntrySearchSnippet `json:"snippets"`
	Score            float64              `json:"score"`
	SimilarReports   []*EntrySearchResult `json:"similar_reports"`
	VaultPath        string               `json:"vault_path"`
	OriginalURL      string               `json:"original_url"`
}

type EntrySearchResponse struct {
	Snapshot                   json.RawMessage      `json:"snapshot,omitempty"`
	VectorUnavailable          bool                 `json:"vector_unavailable"`
	RerankStatus               string               `json:"rerank_status"`
	Results                    []*EntrySearchResult `json:"results"`
	ExcludedMissingPublishedAt int64                `json:"excluded_missing_published_at"`
}

type EntrySearchOptions struct {
	ContentTypes []string            `json:"content_types"`
	Entities     []EntrySearchEntity `json:"entities"`
	Topics       []EntrySearchTopic  `json:"topics"`
}
