package types

import "time"

// EntryProcessingOutput stores the latest processing result and image evidence.
type EntryProcessingOutput struct {
	EntryID     string            `json:"entry_id" gorm:"primaryKey"`
	SpaceID     uint64            `json:"space_id"`
	LibraryID   string            `json:"library_id"`
	RunID       string            `json:"run_id"`
	BodyHash    string            `json:"body_hash"`
	ContentType string            `json:"content_type"`
	Images      []ProcessingImage `json:"images" gorm:"serializer:json;type:jsonb"`
	UpdatedAt   time.Time         `json:"updated_at"`
}

func (EntryProcessingOutput) TableName() string { return "entry_processing_outputs" }

type ProcessingImage struct {
	URL         string           `json:"url"`
	Description string           `json:"description,omitempty"`
	SkipReason  string           `json:"skip_reason,omitempty"`
	Locations   []SourceLocation `json:"locations"`
	// Ordinal addresses repeated references independently in the saved body.
	Ordinal int `json:"ordinal"`
}
