package types

import "time"

// ShelfDocumentInput classifies a staged original or an external link.
// Text is optional; file parsing is handled separately from version creation.
type ShelfDocumentInput struct {
	RequirementIDs    []string         `json:"requirement_ids,omitempty"`
	FileCategory      string           `json:"file_category,omitempty"`
	Unassigned        bool             `json:"unassigned,omitempty"`
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
	RequirementIDs    []string         `json:"requirement_ids" gorm:"serializer:json;type:jsonb"`
	FileCategory      string           `json:"file_category,omitempty"`
	ParserKey         string           `json:"parser_key,omitempty"`
	ParseAttempt      int64            `json:"parse_attempt"`
	ParseError        string           `json:"parse_error,omitempty"`
	ID                string           `json:"id" gorm:"primaryKey"`
	SpaceID           uint64           `json:"space_id"`
	LibraryID         string           `json:"library_id"`
	FileID            string           `json:"file_id"`
	OwnerID           *string          `json:"owner_id,omitempty"`
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

// ShelfDocumentClassification changes the explicit classification of a whole file.
// Version originals and labels stay unchanged; the prior classification is logged.
type ShelfDocumentClassification struct {
	RequirementIDs    []string         `json:"requirement_ids,omitempty"`
	OwnerID           string           `json:"owner_id"`
	RequirementID     string           `json:"requirement_id,omitempty"`
	CertificateNumber string           `json:"certificate_number,omitempty"`
	Conditions        []ShelfCondition `json:"conditions"`
}
type ShelfDocumentSelection struct {
	ID         string    `json:"id" gorm:"primaryKey"`
	SpaceID    uint64    `json:"space_id"`
	LibraryID  string    `json:"library_id"`
	FileID     string    `json:"file_id"`
	VersionID  string    `json:"version_id"`
	SelectedAt time.Time `json:"selected_at"`
}

func (ShelfDocumentSelection) TableName() string { return "shelf_document_selections" }

type ShelfDocumentListRequest struct {
	Query          string
	Group          string
	OwnerKind      string
	ParseStatus    string
	IncludeHistory bool
	Limit          int
	Offset         int
}
type ShelfDocumentListItem struct {
	Requirements []Entity             `json:"requirements"`
	Entity       Entity               `json:"entity"`
	Document     ShelfDocumentVersion `json:"document"`
	Owner        *Entity              `json:"owner,omitempty"`
	Requirement  *Entity              `json:"requirement,omitempty"`
}
type ShelfDocumentList struct {
	Items     []ShelfDocumentListItem `json:"items"`
	Total     int64                   `json:"total"`
	Truncated bool                    `json:"truncated"`
}

// RequirementReferences accepts the legacy single selector at API boundaries.
func RequirementReferences(single string, multiple []string) []string {
	if len(multiple) > 0 {
		return multiple
	}
	if single != "" {
		return []string{single}
	}
	return []string{}
}
func (v ShelfDocumentVersion) Requirements() []string {
	single := ""
	if v.RequirementID != nil {
		single = *v.RequirementID
	}
	return RequirementReferences(single, v.RequirementIDs)
}
