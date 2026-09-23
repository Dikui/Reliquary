package types

import "time"

// KnowledgeStrategyVersion records a proposed or active set of text rules.
// Versions never contain original message bodies; previews store brief results.
type KnowledgeStrategyVersion struct {
	ID              string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64     `json:"tenant_id"`
	KnowledgeBaseID string     `json:"knowledge_base_id"`
	Status          string     `json:"status"`
	Rules           JSON       `json:"rules" gorm:"type:json"`
	PreviousRules   JSON       `json:"previous_rules" gorm:"type:json"`
	Reason          string     `json:"reason"`
	SampleResults   JSON       `json:"sample_results" gorm:"type:json"`
	CreatedBy       string     `json:"created_by"`
	ActivatedBy     string     `json:"activated_by"`
	CreatedAt       time.Time  `json:"created_at"`
	ActivatedAt     *time.Time `json:"activated_at"`
}

func (KnowledgeStrategyVersion) TableName() string { return "knowledge_strategy_versions" }
