package types

import "time"

// KnowledgeAttention records the independent reason an otherwise eligible
// article is useful to follow; it never participates in intake quality.
type KnowledgeAttention struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"not null;index:idx_kca_scope;uniqueIndex:idx_kca_evidence"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kca_scope;uniqueIndex:idx_kca_evidence"`
	EvidenceID      string    `json:"evidence_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kca_evidence"`
	EventID         string    `json:"event_id,omitempty" gorm:"type:varchar(36);not null;default:'';index"`
	Novelty         float64   `json:"novelty" gorm:"not null;default:0"`
	Impact          float64   `json:"impact" gorm:"not null;default:0"`
	Credibility     float64   `json:"credibility" gorm:"not null;default:0"`
	Relevance       float64   `json:"relevance" gorm:"not null;default:0"`
	Actionability   float64   `json:"actionability" gorm:"not null;default:0"`
	Score           float64   `json:"score" gorm:"not null;default:0;index"`
	Reason          string    `json:"reason" gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time `json:"created_at"`
}

func (KnowledgeAttention) TableName() string { return "knowledge_context_attention" }
