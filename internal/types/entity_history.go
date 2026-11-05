package types

import "time"

// EntityAttributeVersion records a complete effective snapshot. RecordedAt is
// when the team saved it; EffectiveAt is when the values apply.
type EntityAttributeVersion struct {
	ID          string         `json:"id" gorm:"primaryKey"`
	SpaceID     uint64         `json:"space_id"`
	EntityID    string         `json:"entity_id"`
	Revision    int64          `json:"revision"`
	EffectiveAt time.Time      `json:"effective_at"`
	RecordedAt  time.Time      `json:"recorded_at"`
	Attributes  map[string]any `json:"attributes" gorm:"serializer:json;type:jsonb"`
}

type EntityReorganization struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	SpaceID   uint64    `json:"space_id"`
	LibraryID string    `json:"library_id"`
	Kind      string    `json:"kind"`
	SourceID  string    `json:"source_id"`
	TargetID  string    `json:"target_id"`
	CreatedAt time.Time `json:"created_at"`
}

type ManagedEntityMergeInput struct {
	DropRelationIDs []string       `json:"drop_relation_ids,omitempty"`
	Revision        int64          `json:"revision"`
	SourceID        string         `json:"source_id"`
	SourceRevision  int64          `json:"source_revision"`
	Attributes      map[string]any `json:"attributes"`
	EffectiveAt     *time.Time     `json:"effective_at,omitempty"`
}

type ManagedEntitySplitInput struct {
	Revision    int64              `json:"revision"`
	NewEntity   ManagedEntityInput `json:"new_entity"`
	EntryIDs    []string           `json:"entry_ids"`
	RelationIDs []string           `json:"relation_ids"`
	EffectiveAt *time.Time         `json:"effective_at,omitempty"`
}

// Attachments are owned by their library. Keeping them independently of a
// current field value preserves downloads from historical snapshots.
type EntityAttachment struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	SpaceID   uint64    `json:"space_id"`
	LibraryID string    `json:"library_id"`
	Name      string    `json:"name"`
	MimeType  string    `json:"mime_type"`
	Size      int64     `json:"size"`
	Path      string    `json:"-"`
	CreatedAt time.Time `json:"created_at"`
}
