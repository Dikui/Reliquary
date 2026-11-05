package types

import "time"

// Relation is the PostgreSQL authority for a directed, library-scoped triple.
type Relation struct {
	Attributes   map[string]any `json:"attributes" gorm:"serializer:json;type:jsonb;not null;default:'{}'"`
	ID           string         `json:"id" gorm:"primaryKey"`
	SpaceID      uint64         `json:"space_id"`
	LibraryID    string         `json:"library_id"`
	SubjectID    string         `json:"subject_id"`
	RelationType string         `json:"relation_type"`
	ObjectID     string         `json:"object_id"`
	Version      int64          `json:"version"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}
type RelationSource struct {
	RelationID string             `json:"relation_id" gorm:"primaryKey"`
	EntryID    string             `json:"entry_id" gorm:"primaryKey"`
	Evidence   []RelationEvidence `json:"evidence" gorm:"serializer:json;type:jsonb"`
	RunID      string             `json:"run_id"`
	BodyHash   string             `json:"body_hash"`
}
type RelationSuppression struct {
	SpaceID      uint64    `json:"space_id"`
	LibraryID    string    `json:"library_id" gorm:"primaryKey"`
	SubjectID    string    `json:"subject_id" gorm:"primaryKey"`
	RelationType string    `json:"relation_type" gorm:"primaryKey"`
	ObjectID     string    `json:"object_id" gorm:"primaryKey"`
	Actor        string    `json:"actor"`
	CreatedAt    time.Time `json:"created_at"`
}
type RelationTypeMerge struct {
	LibraryID string    `json:"library_id" gorm:"primaryKey"`
	FromType  string    `json:"from_type" gorm:"primaryKey"`
	ToType    string    `json:"to_type"`
	CreatedAt time.Time `json:"created_at"`
}
type GraphPendingSync struct {
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	ID              string     `json:"id" gorm:"primaryKey"`
	SpaceID         uint64     `json:"space_id"`
	EntityID        string     `json:"entity_id"`
	RelationID      string     `json:"relation_id"`
	RelationVersion int64      `json:"relation_version"`
	Deleted         bool       `json:"deleted"`
	CreatedAt       time.Time  `json:"created_at"`
}

func (GraphPendingSync) TableName() string { return "graph_pending_sync" }
