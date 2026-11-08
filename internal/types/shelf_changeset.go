package types

import "time"

// OperationSource identifies the submitter independently of the confirming user.
type OperationSource struct {
	Type          string     `json:"type,omitempty"`
	ImportPlanKey string     `json:"import_plan_key,omitempty"`
	PlanRevision  int64      `json:"plan_revision,omitempty"`
	FileName      string     `json:"file_name,omitempty"`
	FileSHA256    string     `json:"file_sha256,omitempty"`
	Worksheet     string     `json:"worksheet,omitempty"`
	Cells         []string   `json:"cells,omitempty"`
	AgentName     string     `json:"agent_name,omitempty"`
	ImportBatchID string     `json:"import_batch_id,omitempty"`
	FileDate      *time.Time `json:"file_date,omitempty"`
}

// ShelfOperation is a closed set of typed proposals. Later shelf capabilities
// add operation types here and execute them through the same confirmation path.
type ShelfOperation struct {
	ParentID           string                            `json:"parent_id,omitempty"`
	Classification     *ShelfDocumentClassification      `json:"classification,omitempty"`
	VersionID          string                            `json:"version_id,omitempty"`
	ImportPlan         *ShelfImportPlanInput             `json:"import_plan,omitempty"`
	ImportLocation     *ShelfImportLocation              `json:"import_location,omitempty"`
	Reparse            *ShelfReparseInput                `json:"reparse,omitempty"`
	Document           *ShelfDocumentInput               `json:"document,omitempty"`
	CertificationOwner *ShelfCertificationOwner          `json:"certification_owner,omitempty"`
	Certification      *ShelfCertificationInput          `json:"certification,omitempty"`
	SaleStatuses       *[]ShelfSaleStatus                `json:"sale_statuses,omitempty"`
	ParameterValues    *map[string][]ShelfParameterValue `json:"parameter_values,omitempty"`
	EffectiveAt        *time.Time                        `json:"effective_at,omitempty"`
	ID                 string                            `json:"id"`
	Type               string                            `json:"type"`
	CandidateEntityID  string                            `json:"candidate_entity_id,omitempty"`
	EntityID           string                            `json:"entity_id,omitempty"`
	Entity             *ManagedEntityInput               `json:"entity,omitempty"`
	Patch              *ManagedEntityPatch               `json:"patch,omitempty"`
	Alias              string                            `json:"alias,omitempty"`
	Revision           int64                             `json:"revision,omitempty"`
	RelationID         string                            `json:"relation_id,omitempty"`
	Relation           *ManagedRelationInput             `json:"relation,omitempty"`
	ValidTo            *time.Time                        `json:"valid_to,omitempty"`
	EntryID            string                            `json:"entry_id,omitempty"`
	Excluded           bool                              `json:"excluded,omitempty"`
	RelationType       *string                           `json:"relation_type,omitempty"`
	Library            *LibraryPatch                     `json:"library,omitempty"`
	LibraryUpdatedAt   *time.Time                        `json:"library_updated_at,omitempty"`
	Issues             []ShelfConflict                   `json:"issues,omitempty"`
}

type ShelfConflict struct {
	Type         string                    `json:"type"`
	Message      string                    `json:"message,omitempty"`
	Details      any                       `json:"details,omitempty"`
	ManualBefore map[string]any            `json:"manual_before,omitempty"`
	Candidates   []ManagedNameConfirmation `json:"candidates,omitempty"`
}

type ShelfDecision struct {
	ItemID            string                    `json:"item_id"`
	Action            string                    `json:"action"`
	TargetEntityID    string                    `json:"target_entity_id,omitempty"`
	TargetRevision    int64                     `json:"target_revision,omitempty"`
	NameConfirmations []ManagedNameConfirmation `json:"name_confirmations,omitempty"`
	Replacements      []ShelfOperation          `json:"replacements,omitempty"`
	Replacement       *ShelfOperation           `json:"replacement,omitempty"`
}

type ShelfChangesetItem struct {
	Warnings  []ShelfWarning  `json:"warnings,omitempty"`
	ID        string          `json:"id"`
	EntityID  string          `json:"entity_id,omitempty"`
	Operation ShelfOperation  `json:"operation"`
	Before    JSON            `json:"before"`
	After     JSON            `json:"after"`
	Conflicts []ShelfConflict `json:"conflicts"`
	Decision  *ShelfDecision  `json:"decision,omitempty"`
}

type ShelfChangeset struct {
	ID                  string               `json:"id" gorm:"primaryKey"`
	SpaceID             uint64               `json:"space_id"`
	LibraryID           string               `json:"library_id"`
	Revision            int64                `json:"revision"`
	Status              string               `json:"status"`
	Source              OperationSource      `json:"source" gorm:"serializer:json;type:jsonb"`
	Items               []ShelfChangesetItem `json:"items" gorm:"serializer:json;type:jsonb"`
	DeferredChangesetID string               `json:"deferred_changeset_id,omitempty"`
	CreatedAt           time.Time            `json:"created_at"`
	UpdatedAt           time.Time            `json:"updated_at"`
	ConfirmedAt         *time.Time           `json:"confirmed_at,omitempty"`
}

type ShelfChangesetInput struct {
	Source     OperationSource  `json:"source"`
	Operations []ShelfOperation `json:"operations"`
	Confirmed  bool             `json:"confirmed"`
	Decisions  []ShelfDecision  `json:"decisions,omitempty"`
}

type ShelfChangesetReview struct {
	Revision  int64           `json:"revision"`
	Decisions []ShelfDecision `json:"decisions"`
}

type ShelfChangesetPage struct {
	Items    []ShelfChangeset `json:"items"`
	Total    int64            `json:"total"`
	Page     int              `json:"page"`
	PageSize int              `json:"page_size"`
}
