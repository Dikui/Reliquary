package types

import (
	"time"
)

// Topic describes a user-defined direction within one library.
type Topic struct {
	LibraryID   string    `json:"library_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_topics_space_library_name"`
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID     uint64    `json:"space_id" gorm:"not null;uniqueIndex:idx_topics_space_library_name"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null;uniqueIndex:idx_topics_space_library_name"`
	Description string    `json:"description" gorm:"type:text;not null;default:''"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	EntryCount  int64     `json:"entry_count" gorm:"->"`
}

func (Topic) TableName() string { return "topics" }

type EntryTopic struct {
	SpaceID      uint64    `json:"space_id" gorm:"not null;index"`
	EntryID      string    `json:"entry_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entry_topics_pair"`
	TopicID      string    `json:"topic_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entry_topics_pair"`
	IsHistorical bool      `json:"is_historical"`
	Source       string    `json:"source" gorm:"type:varchar(8);not null;default:'model'"`
	CreatedAt    time.Time `json:"created_at"`
}

func (EntryTopic) TableName() string { return "entry_topics" }

const (
	TopicSourceModel = "model"
	TopicSourceUser  = "user"
)

// TopicBackfillJob is a durable snapshot of one topic and its historical articles.
type TopicBackfillJob struct {
	ID               string               `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID          uint64               `json:"-"`
	TopicID          string               `json:"topic_id"`
	TopicName        string               `json:"-"`
	TopicDescription string               `json:"-"`
	Status           string               `json:"status"`
	Total            int                  `json:"total"`
	Processed        int                  `json:"processed"`
	FailedEntries    int                  `json:"failed_entries"`
	FailedBatches    int                  `json:"failed_batches"`
	CreatedAt        time.Time            `json:"created_at"`
	UpdatedAt        time.Time            `json:"updated_at"`
	Batches          []TopicBackfillBatch `json:"batches" gorm:"-"`
}

func (TopicBackfillJob) TableName() string { return "topic_backfill_jobs" }

type TopicBackfillBatch struct {
	RetryAt    *time.Time `json:"retry_at,omitempty"`
	ID         string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	JobID      string     `json:"-"`
	Ordinal    int        `json:"ordinal"`
	EntryIDs   JSON       `json:"-" gorm:"type:jsonb"`
	EntryCount int        `json:"entry_count"`
	Status     string     `json:"status"`
	Attempts   int        `json:"attempts"`
	Error      string     `json:"error"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (TopicBackfillBatch) TableName() string { return "topic_backfill_batches" }
