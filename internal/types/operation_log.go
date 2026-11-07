package types

import "time"

// OperationLog records a deliberate user action, including API key callers.
type OperationLog struct {
	ID            string          `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID       uint64          `json:"space_id"`
	Actor         string          `json:"actor"`
	ActorName     string          `json:"actor_name,omitempty" gorm:"-"`
	APIKeyID      uint64          `json:"api_key_id,omitempty" gorm:"not null;default:0"`
	APIKeyName    string          `json:"api_key_name,omitempty" gorm:"type:text;not null;default:''"`
	Source        OperationSource `json:"source" gorm:"serializer:json;type:jsonb"`
	ChangesetID   string          `json:"changeset_id,omitempty"`
	OperationType string          `json:"operation_type"`
	ObjectID      string          `json:"object_id"`
	Before        JSON            `json:"before" gorm:"column:before_value;type:jsonb"`
	After         JSON            `json:"after" gorm:"column:after_value;type:jsonb"`
	CreatedAt     time.Time       `json:"created_at"`
}

func (OperationLog) TableName() string { return "operation_logs" }

var OperationTypes = []string{
	"save_import_plan", "configure_shelf", "reparse_document", "create_entity", "update_entity", "delete_entity", "update_pending_entity_attributes",
	"create_relation", "update_relation", "set_entity_link",
	"switch_entity_library", "merge_managed_entities", "split_managed_entity",
	"ingest", "write_report", "delete_document",
	"ingest_discard",
	"review_approve", "review_reject", "review_retry", "review_discard",
	"review_supplement_url", "review_admit_unverified",
	"star", "unstar", "correct_content_type", "correct_topics", "merge_entities",
	"create_topic", "update_topic", "delete_topic",
	"graph_check", "graph_rebuild",
	"follow", "unfollow", "add_relation_type", "delete_relation", "merge_relation_types",
}

type OperationLogPage struct {
	Items    []OperationLog `json:"items"`
	Total    int64          `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}
