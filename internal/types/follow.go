package types

import "time"

const (
	FollowTargetEntity = "entity"
	FollowTargetStory  = "story"
)

// Follow is a subscription to a canonical story or entity.
type Follow struct {
	SpaceID    uint64    `json:"-" gorm:"primaryKey"`
	TargetType string    `json:"target_type" gorm:"primaryKey"`
	TargetID   string    `json:"target_id" gorm:"primaryKey"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Follow) TableName() string { return "follows" }

type FollowTarget struct {
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Name       string    `json:"name"`
	CreatedAt  time.Time `json:"created_at"`
}

type FollowFeedRequest struct {
	Libraries []string `json:"libraries,omitempty"`
	Cursor    string   `json:"cursor,omitempty"`
	Since     string   `json:"since,omitempty"`
	TargetIDs []string `json:"target_ids,omitempty"`
	Limit     int      `json:"limit,omitempty"`
}

// FollowFeedCard highlights the newest qualifying fact and the three latest facts.
type FollowFeedCard struct {
	Story          Story          `json:"story"`
	TriggerFact    StoryFact      `json:"trigger_fact"`
	RecentFacts    []StoryFact    `json:"recent_facts"`
	MatchedFollows []FollowTarget `json:"matched_follows"`
}

type FollowFeedResponse struct {
	Libraries  []FollowLibrary  `json:"libraries"`
	Results    []FollowFeedCard `json:"results"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Total      int64            `json:"total"`
}

// FollowLibrary identifies an enabled library without retaining read state.
type FollowLibrary struct {
	LibraryID   string `json:"library_id"`
	LibraryName string `json:"library_name"`
	DisplayName string `json:"display_name"`
}
