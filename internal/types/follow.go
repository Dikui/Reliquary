package types

import "time"

const (
	FollowTargetEntity = "entity"
	FollowTargetTopic  = "topic"
)

// Follow is a subscription to a canonical entity or a user-defined topic.
type Follow struct {
	SpaceID    uint64    `json:"-" gorm:"primaryKey"`
	TargetType string    `json:"target_type" gorm:"primaryKey"`
	TargetID   string    `json:"target_id" gorm:"primaryKey"`
	CreatedAt  time.Time `json:"created_at"`
}

func (Follow) TableName() string { return "follows" }

type FollowTarget struct {
	TargetType         string    `json:"target_type"`
	TargetID           string    `json:"target_id"`
	Name               string    `json:"name"`
	CreatedAt          time.Time `json:"created_at"`
	RecentArticleCount int64     `json:"recent_article_count"`
}

type FollowFeedRequest struct {
	Libraries       []string `json:"libraries,omitempty"`
	Cursor          string   `json:"cursor,omitempty"`
	Since           string   `json:"since,omitempty"`
	TargetIDs       []string `json:"target_ids,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	CollapseSimilar *bool    `json:"collapse_similar,omitempty"`
	IncludeSeen     bool     `json:"include_seen,omitempty"`
}

// FollowFeedArticle preserves the retrieval projection, including folded items.
type FollowFeedArticle struct {
	EntrySearchResult
	EnteredAt      *time.Time           `json:"entered_at"`
	CollectedAt    time.Time            `json:"collected_at"`
	IsNew          bool                 `json:"is_new"`
	MatchedFollows []FollowTarget       `json:"matched_follows"`
	SimilarReports []*FollowFeedArticle `json:"similar_reports"`
}

type FollowFeedResponse struct {
	Libraries    []FollowLibraryState `json:"libraries"`
	Results      []*FollowFeedArticle `json:"results"`
	NextCursor   string               `json:"next_cursor,omitempty"`
	Total        int64                `json:"total"`
	NewCount     int64                `json:"new_count"`
	LastViewedAt *time.Time           `json:"last_viewed_at"`
	ViewedAt     time.Time            `json:"viewed_at"`
}

type FollowLibraryState struct {
	LibraryID    string     `json:"library_id"`
	LibraryName  string     `json:"library_name"`
	DisplayName  string     `json:"display_name"`
	NewCount     int64      `json:"new_count"`
	LastViewedAt *time.Time `json:"last_viewed_at"`
}

type FollowFeedState struct {
	Libraries    []FollowLibraryState `json:"libraries"`
	NewCount     int64                `json:"new_count"`
	LastViewedAt *time.Time           `json:"last_viewed_at"`
}

// FollowFeedCheckpoint retains the legacy space watermark for older backups.
type FollowFeedCheckpoint struct {
	SpaceID      uint64 `gorm:"primaryKey"`
	LastViewedAt *time.Time
}

func (FollowFeedCheckpoint) TableName() string { return "follow_feed_states" }

// Library checkpoints are independent; the old space checkpoint is retained for backup compatibility.
type FollowLibraryCheckpoint struct {
	SpaceID      uint64 `gorm:"primaryKey"`
	LibraryID    string `gorm:"primaryKey"`
	LastViewedAt *time.Time
}

func (FollowLibraryCheckpoint) TableName() string { return "follow_feed_library_states" }
