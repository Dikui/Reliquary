package types

import "time"

// EntityCandidate retains one normalized name and the user's review decision.
type EntityCandidate struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	SpaceID         uint64    `json:"space_id"`
	Name            string    `json:"name"`
	NormalizedName  string    `json:"-"`
	EntityType      string    `json:"entity_type"`
	Status          string    `json:"status"`
	EntityID        *string   `json:"entity_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
	OccurrenceCount int64     `json:"occurrence_count" gorm:"->;-:migration"`
}

// EntityCandidateOccurrence identifies the exact fact endpoint for review and attachment.
type EntityCandidateOccurrence struct {
	SpaceID     uint64    `json:"space_id"`
	CandidateID string    `json:"candidate_id"`
	FactID      string    `json:"fact_id"`
	Endpoint    string    `json:"endpoint"`
	CreatedAt   time.Time `json:"created_at"`
}

// EntityCandidateEntry retains evidence from any material, including those without facts.
type EntityCandidateEntry struct {
	SpaceID     uint64    `json:"space_id"`
	CandidateID string    `json:"candidate_id"`
	EntryID     string    `json:"entry_id"`
	Quote       string    `json:"quote"`
	CreatedAt   time.Time `json:"created_at"`
}

// EntityCandidatePage contains a bounded, occurrence-ranked list.
type EntityCandidatePage struct {
	Items []EntityCandidate `json:"items"`
	Total int64             `json:"total"`
}

// EntityCandidateFact includes the recorded endpoint and its fact.
type EntityCandidateFact struct {
	Fact      *Fact                     `json:"fact,omitempty"`
	Endpoint  string                    `json:"endpoint,omitempty"`
	Entry     *EntityCandidateEntryInfo `json:"entry,omitempty"`
	Quote     string                    `json:"quote,omitempty"`
	CreatedAt time.Time                 `json:"created_at"`
}

type EntityCandidateEntryInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// EntityCandidateDetail retains review state alongside occurrence evidence.
type EntityCandidateDetail struct {
	Candidate   EntityCandidate       `json:"candidate"`
	Occurrences []EntityCandidateFact `json:"occurrences"`
	Total       int64                 `json:"total"`
}
