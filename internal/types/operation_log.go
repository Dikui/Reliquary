package types

import "time"

// OperationLog records a deliberate user action, including API key callers.
type OperationLog struct {
	ID            string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID      uint64    `json:"tenant_id"`
	Actor         string    `json:"actor"`
	ActorName     string    `json:"actor_name,omitempty" gorm:"-"`
	OperationType string    `json:"operation_type"`
	ObjectID      string    `json:"object_id"`
	Before        JSON      `json:"before" gorm:"column:before_value;type:jsonb"`
	After         JSON      `json:"after" gorm:"column:after_value;type:jsonb"`
	CreatedAt     time.Time `json:"created_at"`
}

func (OperationLog) TableName() string { return "operation_logs" }

var OperationTypes = []string{
	"ingest", "write_report",
	"review_approve", "review_reject", "review_retry", "review_discard",
	"review_supplement_url", "review_admit_unverified", "review_convert_to_mine",
	"processing_review_confirm", "processing_review_correct", "processing_review_ignore",
	"star", "unstar", "correct_content_type", "correct_topics", "merge_entities",
	"create_topic", "update_topic", "delete_topic",
}

type OperationLogPage struct {
	Items    []OperationLog `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}
