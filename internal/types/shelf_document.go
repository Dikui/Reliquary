package types

import "time"

// ShelfDocumentInput classifies a staged original or an external link.
// Text is optional; file parsing is handled separately from version creation.
type ShelfDocumentInput struct {
	Name              string           `json:"name,omitempty"`
	OwnerID           string           `json:"owner_id"`
	RequirementID     string           `json:"requirement_id,omitempty"`
	CertificateNumber string           `json:"certificate_number,omitempty"`
	AttachmentID      string           `json:"attachment_id,omitempty"`
	CloudURL          string           `json:"cloud_url,omitempty"`
	EntryID           string           `json:"entry_id,omitempty"`
	VersionLabel      string           `json:"version_label,omitempty"`
	Stage             string           `json:"stage"`
	Origin            string           `json:"origin,omitempty"`
	OccurredAt        *time.Time       `json:"occurred_at,omitempty"`
	Conditions        []ShelfCondition `json:"conditions,omitempty"`
}

type ShelfDocumentVersion struct {
	ID                string           `json:"id" gorm:"primaryKey"`
	SpaceID           uint64           `json:"space_id"`
	LibraryID         string           `json:"library_id"`
	FileID            string           `json:"file_id"`
	OwnerID           string           `json:"owner_id"`
	RequirementID     *string          `json:"requirement_id,omitempty"`
	CertificateNumber string           `json:"certificate_number,omitempty"`
	AttachmentID      *string          `json:"attachment_id,omitempty"`
	CloudURL          string           `json:"cloud_url,omitempty"`
	EntryID           *string          `json:"entry_id,omitempty"`
	Ordinal           int64            `json:"ordinal"`
	VersionLabel      string           `json:"version_label"`
	Stage             string           `json:"stage"`
	IsCurrent         bool             `json:"is_current"`
	Origin            string           `json:"origin,omitempty"`
	OccurredAt        time.Time        `json:"occurred_at"`
	CreatedAt         time.Time        `json:"created_at"`
	Source            OperationSource  `json:"source" gorm:"serializer:json;type:jsonb"`
	ChangesetID       string           `json:"changeset_id"`
	ParseStatus       string           `json:"parse_status"`
	Conditions        []ShelfCondition `json:"conditions" gorm:"serializer:json;type:jsonb"`
}

func (ShelfDocumentVersion) TableName() string { return "shelf_document_versions" }
