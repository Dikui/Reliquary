package types

import "time"

// Stars express the team's structured extraction focus, independently of
// resource favorites and attention scores.
type KnowledgeEntityStar struct {
	TenantID  uint64    `json:"tenant_id" gorm:"primaryKey;autoIncrement:false"`
	EntityID  string    `json:"entity_id" gorm:"type:varchar(36);primaryKey"`
	CreatedAt time.Time `json:"created_at"`
}

func (KnowledgeEntityStar) TableName() string { return "knowledge_context_entity_stars" }

type KnowledgeTopicStar struct {
	TenantID  uint64    `json:"tenant_id" gorm:"primaryKey;autoIncrement:false"`
	TagID     string    `json:"tag_id" gorm:"type:varchar(36);primaryKey"`
	CreatedAt time.Time `json:"created_at"`
}

func (KnowledgeTopicStar) TableName() string { return "knowledge_context_topic_stars" }

// KnowledgeFocusEntity identifies a tracked object and the workflow query
// scopes in which a new message mentioning it requests complete extraction.
type KnowledgeFocusEntity struct {
	KnowledgeEntity
	WorkflowIDs      []string `json:"workflow_ids"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
}

type KnowledgeFocusTopic struct {
	KnowledgeTag
	KnowledgeBaseName string `json:"knowledge_base_name" gorm:"-"`
}

type KnowledgeFocusScope struct {
	StarredEntities []KnowledgeEntity      `json:"starred_entities"`
	StarredTopics   []KnowledgeFocusTopic  `json:"starred_topics"`
	TrackedEntities []KnowledgeFocusEntity `json:"tracked_entities"`
	TrackedTopics   []KnowledgeFocusTopic  `json:"tracked_topics"`
	AvailableTopics []KnowledgeFocusTopic  `json:"available_topics"`
}
