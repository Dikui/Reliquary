package types

import "time"

type GraphEntityInput struct {
	ID   string `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}
type GraphSearchRequest struct {
	Libraries     []string          `json:"libraries"`
	Entity        GraphEntityInput  `json:"entity"`
	Target        *GraphEntityInput `json:"target,omitempty"`
	Mode          string            `json:"mode,omitempty"`
	Hops          int               `json:"hops,omitempty"`
	RelationTypes []string          `json:"relation_types,omitempty"`
	Limit         int               `json:"limit,omitempty"`
}
type GraphSource struct {
	EntryID          string             `json:"entry_id"`
	Title            string             `json:"title"`
	LibraryID        string             `json:"library_id"`
	LibraryName      string             `json:"library_name"`
	LibraryDirectory string             `json:"library_directory"`
	PublishedAt      *time.Time         `json:"published_at"`
	SourceUnverified bool               `json:"source_unverified"`
	Acquisition      string             `json:"acquisition"`
	Evidence         []RelationEvidence `json:"evidence"`
}
type GraphSearchEdge struct {
	ID           string        `json:"id"`
	SubjectID    string        `json:"subject_id"`
	ObjectID     string        `json:"object_id"`
	LibraryID    string        `json:"library_id"`
	RelationType string        `json:"relation_type"`
	Version      int64         `json:"version"`
	Sources      []GraphSource `json:"sources"`
	SourceCount  int           `json:"source_count"`
}
type GraphSearchPath struct {
	EntityIDs   []string `json:"entity_ids"`
	RelationIDs []string `json:"relation_ids"`
}
type GraphTruncation struct {
	Hop      int    `json:"hop,omitempty"`
	EntityID string `json:"entity_id,omitempty"`
	Reason   string `json:"reason"`
}
type GraphSearchResponse struct {
	Status         string                         `json:"status"`
	Libraries      []string                       `json:"libraries"`
	Mode           string                         `json:"mode"`
	Hops           int                            `json:"hops"`
	ExpansionLimit int                            `json:"expansion_limit"`
	Candidates     map[string][]EntrySearchEntity `json:"candidates"`
	Entities       []EntrySearchEntity            `json:"entities"`
	Edges          []GraphSearchEdge              `json:"edges"`
	Paths          []GraphSearchPath              `json:"paths"`
	Documents      []*EntrySearchResult           `json:"documents"`
	Truncated      bool                           `json:"truncated"`
	Truncations    []GraphTruncation              `json:"truncations"`
	PendingSync    struct {
		Global   int64 `json:"global"`
		Involved int64 `json:"involved"`
	} `json:"pending_sync"`
}
