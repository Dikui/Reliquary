package types

import "time"

type ContextNameEmbeddingRebuildPayload struct {
	TenantID uint64 `json:"tenant_id"`
	RunID    string `json:"run_id"`
}

type ContextNameEmbeddingRebuildRun struct {
	ID             string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID       uint64     `json:"tenant_id" gorm:"not null;index"`
	Status         string     `json:"status" gorm:"type:varchar(16);not null"`
	Reason         string     `json:"reason" gorm:"type:text;not null"`
	Error          string     `json:"error" gorm:"type:text;not null;default:''"`
	ModelID        string     `json:"model_id" gorm:"type:varchar(64);not null;default:''"`
	Dimension      int        `json:"dimension" gorm:"not null;default:0"`
	ProfileHash    string     `json:"-" gorm:"type:varchar(64);not null;default:''"`
	ProcessedCount int64      `json:"processed_count" gorm:"not null;default:0"`
	TotalCount     int64      `json:"total_count" gorm:"not null;default:0"`
	ModelCalls     int        `json:"model_calls" gorm:"not null;default:0"`
	Attempts       int        `json:"attempts" gorm:"not null;default:0"`
	AttemptID      string     `json:"-" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

func (ContextNameEmbeddingRebuildRun) TableName() string {
	return "knowledge_context_embedding_rebuild_runs"
}

type ContextNameEmbeddingRebuildStatus struct {
	PendingCount int64                           `json:"pending_count"`
	LatestRun    *ContextNameEmbeddingRebuildRun `json:"latest_run,omitempty"`
}
