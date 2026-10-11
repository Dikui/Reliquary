package types

import "time"

// EmbeddingRebuildRun is a durable snapshot of all vectors to rebuild.
type EmbeddingRebuildRun struct {
	ID             string     `json:"id" gorm:"primaryKey"`
	TenantID       uint64     `json:"tenant_id"`
	Status         string     `json:"status"`
	ModelID        string     `json:"model_id"`
	ModelName      string     `json:"model_name"`
	Dimension      int        `json:"dimension"`
	ProfileHash    string     `json:"-"`
	TotalCount     int64      `json:"total_count"`
	CompletedCount int64      `json:"completed_count"`
	FailedCount    int64      `json:"failed_count"`
	Error          string     `json:"error"`
	AttemptID      string     `json:"-"`
	LeaseUntil     *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	StartedAt      *time.Time `json:"started_at,omitempty"`
	FinishedAt     *time.Time `json:"finished_at,omitempty"`
}

func (EmbeddingRebuildRun) TableName() string { return "embedding_rebuild_runs" }

type EmbeddingRebuildItem struct {
	ID             string    `json:"id" gorm:"primaryKey"`
	RunID          string    `json:"run_id"`
	Kind           string    `json:"kind"`
	SourceID       string    `json:"source_id"`
	AttachmentPath string    `json:"attachment_path,omitempty"`
	Status         string    `json:"status"`
	Attempts       int       `json:"attempts"`
	Error          string    `json:"error"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (EmbeddingRebuildItem) TableName() string { return "embedding_rebuild_items" }

type EmbeddingRebuildStatus struct {
	LatestRun           *EmbeddingRebuildRun   `json:"latest_run"`
	FailedItems         []EmbeddingRebuildItem `json:"failed_items"`
	CurrentModelMatches bool                   `json:"current_model_matches"`
}

type EmbeddingRebuildPayload struct {
	TenantID uint64 `json:"tenant_id"`
	RunID    string `json:"run_id"`
}
