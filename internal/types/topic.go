package types

import "time"

// Topic describes a user-defined direction within the personal space.
type Topic struct {
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64    `json:"tenant_id" gorm:"not null;uniqueIndex:idx_topics_tenant_name"`
	Name        string    `json:"name" gorm:"type:varchar(255);not null;uniqueIndex:idx_topics_tenant_name"`
	Description string    `json:"description" gorm:"type:text;not null;default:''"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Topic) TableName() string { return "topics" }

type KnowledgeTopic struct {
	TenantID    uint64    `json:"tenant_id" gorm:"not null;index"`
	KnowledgeID string    `json:"knowledge_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_knowledge_topics_pair"`
	TopicID     string    `json:"topic_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_knowledge_topics_pair"`
	CreatedAt   time.Time `json:"created_at"`
}

func (KnowledgeTopic) TableName() string { return "knowledge_topics" }
