package types

import "time"

// Story groups facts about one developing occurrence.
type Story struct {
	ID          string    `json:"id"            gorm:"primaryKey"`
	SpaceID     uint64    `json:"space_id"`
	Title       string    `json:"title"`
	FirstFactAt time.Time `json:"first_fact_at"`
	LastFactAt  time.Time `json:"last_fact_at"`
}

// Fact describes one occurrence and belongs to exactly one story.
type Fact struct {
	ID              string  `json:"id"          gorm:"primaryKey"`
	SpaceID         uint64  `json:"space_id"`
	StoryID         string  `json:"story_id"`
	Title           string  `json:"title"`
	Subject         string  `json:"subject"`
	Action          string  `json:"action"`
	Object          string  `json:"object"`
	Conditions      string  `json:"conditions"`
	SubjectEntityID *string `json:"subject_entity_id"`
	ObjectEntityID  *string `json:"object_entity_id"`
	// OccurredAt preserves YYYY-MM-DD or RFC3339 precision; nil means unknown.
	OccurredAt *string   `json:"occurred_at"`
	CreatedAt  time.Time `json:"created_at"`
}

// EntryFact records source evidence or a mention. Only sources require a quote.
type EntryFact struct {
	SpaceID   uint64    `json:"space_id"`
	EntryID   string    `json:"entry_id"`
	FactID    string    `json:"fact_id"`
	Role      string    `json:"role"`
	Quote     string    `json:"quote"`
	Manual    bool      `json:"manual"`
	CreatedAt time.Time `json:"created_at"`
}

// FactSource includes enough material metadata to verify the quoted evidence.
type FactSource struct {
	EntryID     string `json:"entry_id"`
	LibraryID   string `json:"library_id"`
	Title       string `json:"title"`
	OriginalURL string `json:"original_url"`
	Role        string `json:"role"`
	Quote       string `json:"quote"`
	Manual      bool   `json:"manual"`
}

// StoryFact pairs a timeline fact with its source evidence.
type StoryFact struct {
	Fact
	Sources []FactSource `json:"sources" gorm:"-"`
}

// StoryDetail is the story header and its chronological timeline.
type StoryDetail struct {
	Story    Story        `json:"story"`
	Facts    []StoryFact  `json:"facts"`
	Mentions []FactSource `json:"mentions"`
}

// StoryQuery selects a bounded page from the story list.
type StoryQuery struct {
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`
}

// StoryPage contains a bounded list and the total visible story count.
type StoryPage struct {
	Items []Story `json:"items"`
	Total int64   `json:"total"`
}

// GroupingDecision retains every recalled candidate and its verified disposition.
type GroupingDecision struct {
	ID               string    `json:"id" gorm:"primaryKey"`
	SpaceID          uint64    `json:"space_id"`
	EntryID          string    `json:"entry_id"`
	CandidateFactID  string    `json:"candidate_fact_id"`
	Similarity       float64   `json:"similarity"`
	ModelID          string    `json:"model_id"`
	Result           string    `json:"result"`
	Confidence       float64   `json:"confidence"`
	ReviewResult     string    `json:"review_result"`
	ReviewConfidence *float64  `json:"review_confidence"`
	AppliedResult    string    `json:"applied_result"`
	Reason           string    `json:"reason"`
	CreatedAt        time.Time `json:"created_at"`
}
