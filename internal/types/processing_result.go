package types

import "time"

// EntryProcessingOutput is the latest processed evidence, consumed by ikb-12.
// It is separate from any future canonical relation and source-count tables.
type EntryProcessingOutput struct {
	EntryID     string               `json:"entry_id" gorm:"primaryKey"`
	SpaceID     uint64               `json:"space_id"`
	LibraryID   string               `json:"library_id"`
	RunID       string               `json:"run_id"`
	BodyHash    string               `json:"body_hash"`
	ContentType string               `json:"content_type"`
	Relations   []ProcessingRelation `json:"relations" gorm:"serializer:json;type:jsonb"`
	Images      []ProcessingImage    `json:"images" gorm:"serializer:json;type:jsonb"`
	UpdatedAt   time.Time            `json:"updated_at"`
}

func (EntryProcessingOutput) TableName() string { return "entry_processing_outputs" }

type ProcessingSpeaker struct {
	EntityID  string `json:"entity_id,omitempty"`
	Name      string `json:"name"`
	Selection string `json:"selection"`
}
type RelationEvidence struct {
	Locations []SourceLocation   `json:"locations"`
	Speaker   *ProcessingSpeaker `json:"speaker,omitempty"`
}
type ProcessingRelation struct {
	SubjectID string             `json:"subject_id"`
	Type      string             `json:"type"`
	ObjectID  string             `json:"object_id"`
	Evidence  []RelationEvidence `json:"evidence"`
}
type ProcessingImage struct {
	URL         string           `json:"url"`
	Description string           `json:"description,omitempty"`
	SkipReason  string           `json:"skip_reason,omitempty"`
	Locations   []SourceLocation `json:"locations"`
	// Ordinal addresses repeated references independently in the saved body.
	Ordinal int `json:"ordinal"`
}
