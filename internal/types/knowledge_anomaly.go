package types

import "time"

// KnowledgeAnomaly is a message-level issue or correction suggestion. It stores
// identifiers and a short explanation, never a copy of the original message.
type KnowledgeAnomaly struct {
	ID                   string                               `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID             uint64                               `json:"tenant_id" gorm:"index"`
	KnowledgeBaseID      string                               `json:"knowledge_base_id" gorm:"type:varchar(36);index"`
	KnowledgeIDs         []string                             `json:"knowledge_ids" gorm:"serializer:json;type:text"`
	Kind                 string                               `json:"kind" gorm:"type:varchar(32);not null"`
	Description          string                               `json:"description" gorm:"type:text;not null"`
	Site                 string                               `json:"site,omitempty" gorm:"type:varchar(255)"`
	SourceURL            string                               `json:"source_url,omitempty" gorm:"type:text"`
	Reason               string                               `json:"reason,omitempty" gorm:"type:text"`
	Adapter              string                               `json:"adapter,omitempty" gorm:"type:varchar(64)"`
	ProposalType         string                               `json:"proposal_type" gorm:"type:varchar(16);not null;default:'none'"`
	ProposedTagIDs       []string                             `json:"proposed_tag_ids" gorm:"serializer:json;type:text"`
	ProposedRemoveTagIDs []string                             `json:"proposed_remove_tag_ids" gorm:"serializer:json;type:text"`
	ProposedDeleteTagID  string                               `json:"proposed_delete_tag_id" gorm:"type:varchar(36)"`
	AppliedChanges       map[string]KnowledgeAnomalyTagChange `json:"applied_changes" gorm:"serializer:json;type:text"`
	Status               string                               `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	ReportedBy           string                               `json:"reported_by" gorm:"type:varchar(36);not null"`
	DecidedBy            string                               `json:"decided_by" gorm:"type:varchar(36)"`
	DecisionNote         string                               `json:"decision_note" gorm:"type:text"`
	Result               string                               `json:"result" gorm:"type:varchar(500)"`
	CreatedAt            time.Time                            `json:"created_at"`
	UpdatedAt            time.Time                            `json:"updated_at"`
	DecidedAt            *time.Time                           `json:"decided_at"`
}

type KnowledgeAnomalyTagChange struct {
	Added   []string `json:"added"`
	Removed []string `json:"removed"`
}

func (KnowledgeAnomaly) TableName() string { return "knowledge_anomalies" }
