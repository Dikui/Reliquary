package types

import "time"

// OperationLog records a deliberate user action, including API key callers.
type OperationLog struct {
	ID            string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID       uint64    `json:"space_id"`
	Actor         string    `json:"actor"`
	ActorName     string    `json:"actor_name,omitempty" gorm:"-"`
	APIKeyID      uint64    `json:"api_key_id,omitempty" gorm:"not null;default:0"`
	APIKeyName    string    `json:"api_key_name,omitempty" gorm:"type:text;not null;default:''"`
	OperationType string    `json:"operation_type"`
	ObjectID      string    `json:"object_id"`
	Before        JSON      `json:"before" gorm:"column:before_value;type:jsonb"`
	After         JSON      `json:"after" gorm:"column:after_value;type:jsonb"`
	CreatedAt     time.Time `json:"created_at"`
}

func (OperationLog) TableName() string { return "operation_logs" }

var OperationTypes = []string{
	"ingest", "write_report", "delete_document",
	"ingest_discard",
	"review_approve", "review_reject", "review_retry", "review_discard",
	"review_supplement_url", "review_admit_unverified",
	"star", "unstar", "merge_entities",
	"follow", "unfollow",
	"approve_entity_candidate", "alias_entity_candidate", "ignore_entity_candidate", "add_entity_alias",
}

type OperationLogPage struct {
	Items    []OperationLog `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}
