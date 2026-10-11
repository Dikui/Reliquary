package types

import "time"

const (
	FollowTargetEntity = "entity"
	FollowTargetTopic  = "topic"
)

// Follow is a subscription to a canonical entity or a user-defined topic.
type Follow struct {
	TenantID   uint64    `json:"-" gorm:"primaryKey"`
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
	Cursor          string   `json:"cursor,omitempty"`
	Since           string   `json:"since,omitempty"`
	TargetIDs       []string `json:"target_ids,omitempty"`
	Limit           int      `json:"limit,omitempty"`
	CollapseSimilar *bool    `json:"collapse_similar,omitempty"`
	IncludeSeen     bool     `json:"include_seen,omitempty"`
}

// FollowFeedArticle preserves the retrieval projection, including folded items.
type FollowFeedArticle struct {
	ArticleSearchResult
	EnteredAt      *time.Time           `json:"entered_at"`
	CollectedAt    time.Time            `json:"collected_at"`
	IsNew          bool                 `json:"is_new"`
	MatchedFollows []FollowTarget       `json:"matched_follows"`
	SimilarReports []*FollowFeedArticle `json:"similar_reports"`
}

type FollowFeedResponse struct {
	Results      []*FollowFeedArticle `json:"results"`
	NextCursor   string               `json:"next_cursor,omitempty"`
	Total        int64                `json:"total"`
	NewCount     int64                `json:"new_count"`
	LastViewedAt *time.Time           `json:"last_viewed_at"`
	ViewedAt     time.Time            `json:"viewed_at"`
}

type FollowFeedState struct {
	NewCount     int64      `json:"new_count"`
	LastViewedAt *time.Time `json:"last_viewed_at"`
}

// FollowFeedCheckpoint is space-wide because each space has one user.
type FollowFeedCheckpoint struct {
	TenantID     uint64 `gorm:"primaryKey"`
	LastViewedAt *time.Time
}

func (FollowFeedCheckpoint) TableName() string { return "follow_feed_states" }
