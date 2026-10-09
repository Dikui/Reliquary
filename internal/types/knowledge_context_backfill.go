package types

import "time"

type KnowledgeContextBackfillTarget struct {
	EntityID string `json:"entity_id,omitempty"`
	TopicID  string `json:"topic_id,omitempty"`
}

type KnowledgeContextBackfillEstimate struct {
	TemplateOnlyMessageCount int    `json:"template_only_message_count"`
	PendingMessageCount      int    `json:"pending_message_count"`
	BatchMessageCount        int    `json:"batch_message_count"`
	RemainingMessageCount    int    `json:"remaining_message_count"`
	EstimatedModelCalls      int    `json:"estimated_model_calls"`
	EstimatedExtractionCalls int    `json:"estimated_extraction_calls"`
	EstimatedEventCalls      int    `json:"estimated_event_calls"`
	EstimateNote             string `json:"estimate_note"`
}

type KnowledgeContextBackfillBatch struct {
	ID           string                         `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID     uint64                         `json:"tenant_id" gorm:"not null;index:idx_kcbb_scope"`
	EntityID     string                         `json:"entity_id,omitempty" gorm:"type:varchar(36);not null;default:'';index:idx_kcbb_scope"`
	TopicID      string                         `json:"topic_id,omitempty" gorm:"type:varchar(36);not null;default:'';index:idx_kcbb_scope"`
	MessageCount int                            `json:"message_count" gorm:"not null;default:0"`
	SuccessCount int                            `json:"success_count" gorm:"not null;default:0"`
	FailureCount int                            `json:"failure_count" gorm:"not null;default:0"`
	Status       string                         `json:"status" gorm:"type:varchar(32);not null;default:'pending'"`
	CreatedAt    time.Time                      `json:"created_at"`
	UpdatedAt    time.Time                      `json:"updated_at"`
	CompletedAt  *time.Time                     `json:"completed_at,omitempty"`
	Items        []KnowledgeContextBackfillItem `json:"items" gorm:"-"`
}

func (KnowledgeContextBackfillBatch) TableName() string { return "knowledge_context_backfill_batches" }

type KnowledgeContextBackfillItem struct {
	ID          string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID    uint64    `json:"tenant_id" gorm:"not null;index:idx_kcbi_batch"`
	BatchID     string    `json:"batch_id" gorm:"type:varchar(36);not null;index:idx_kcbi_batch;uniqueIndex:idx_kcbi_evidence"`
	KnowledgeID string    `json:"knowledge_id" gorm:"type:varchar(36);not null"`
	EvidenceID  string    `json:"evidence_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcbi_evidence;index"`
	Status      string    `json:"status" gorm:"type:varchar(16);not null;default:'pending'"`
	Error       string    `json:"error,omitempty" gorm:"type:text;not null;default:''"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (KnowledgeContextBackfillItem) TableName() string { return "knowledge_context_backfill_items" }

type KnowledgeContextBackfillPayload struct {
	TenantID        uint64 `json:"tenant_id"`
	KnowledgeBaseID string `json:"knowledge_base_id"`
	KnowledgeID     string `json:"knowledge_id"`
	BatchID         string `json:"batch_id"`
	ItemID          string `json:"item_id"`
	EvidenceID      string `json:"evidence_id"`
}
