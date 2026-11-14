package types

import "time"

type ShelfFindRequest struct {
	Query  string `json:"q"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type ShelfFindResult struct {
	Items     []Entity `json:"items"`
	Total     int64    `json:"total"`
	Truncated bool     `json:"truncated"`
}

// ShelfEvidence identifies the saved snapshot or relation and its submitting log.
type ShelfEvidence struct {
	SnapshotID     string          `json:"snapshot_id,omitempty"`
	OperationLogID string          `json:"operation_log_id,omitempty"`
	Source         OperationSource `json:"source"`
	ChangesetID    string          `json:"changeset_id,omitempty"`
	EffectiveAt    time.Time       `json:"effective_at"`
	RecordedAt     time.Time       `json:"recorded_at"`
}

type ShelfFact struct {
	EntityID       string           `json:"entity_id"`
	RelationID     string           `json:"relation_id,omitempty"`
	Field          string           `json:"field"`
	Value          any              `json:"value"`
	Conditions     []ShelfCondition `json:"conditions"`
	PartialOptions bool             `json:"partial_options"`
	Origin         string           `json:"origin"`
	OriginLabel    string           `json:"origin_label"`
	RegionName     string           `json:"region_name,omitempty"`
	Evidence       ShelfEvidence    `json:"evidence"`
}

type ShelfNodeDetail struct {
	Entity   Entity            `json:"entity"`
	Shelf    *ShelfModelDetail `json:"shelf,omitempty"`
	Facts    []ShelfFact       `json:"facts"`
	AsOf     time.Time         `json:"as_of"`
	Recorded bool              `json:"recorded"`
}

type ShelfStatusFilter struct {
	Region     string `json:"region,omitempty"`
	Status     string `json:"status,omitempty"`
	PlannedEOM *bool  `json:"planned_eom,omitempty"`
	NoRecord   bool   `json:"no_record,omitempty"`
}

// Missing selects models in the field's product lines without a structured
// value, the models counted as missing by facets; it excludes min and max.
type ShelfParameterFilter struct {
	Field   string   `json:"field"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Missing bool     `json:"missing,omitempty"`
}

type ShelfCertificationFilter struct {
	Standard string  `json:"standard,omitempty"`
	Progress *string `json:"progress,omitempty"`
	Region   *string `json:"region,omitempty"`
}

// Query detail levels. Summary keeps identity, description and the facts hit by
// the filters, so batch answers do not carry every model's full checklist.
const (
	ShelfQueryDetailSummary = "summary"
	ShelfQueryDetailFull    = "full"
)

type ShelfQueryRequest struct {
	Detail        string                    `json:"detail,omitempty"`
	Facets        bool                      `json:"facets,omitempty"`
	DocumentGaps  bool                      `json:"document_gaps,omitempty"`
	AncestorID    string                    `json:"ancestor_id,omitempty"`
	SaleStatuses  []ShelfStatusFilter       `json:"sale_statuses,omitempty"`
	Parameters    []ShelfParameterFilter    `json:"parameters,omitempty"`
	Composition   []ShelfCondition          `json:"composition,omitempty"`
	PartID        string                    `json:"part_id,omitempty"`
	Certification *ShelfCertificationFilter `json:"certification,omitempty"`
	AsOf          *time.Time                `json:"as_of,omitempty"`
	Limit         int                       `json:"limit,omitempty"`
	Offset        int                       `json:"offset,omitempty"`
}

type ShelfQueryItem struct {
	ShelfNodeDetail
	PartialOptions     bool               `json:"partial_options"`
	MatchingConditions [][]ShelfCondition `json:"matching_conditions"`
	CurrentStage       string             `json:"current_stage,omitempty"`
}

type ShelfQueryResult struct {
	Items     []ShelfQueryItem  `json:"items"`
	Total     int64             `json:"total"`
	Truncated bool              `json:"truncated"`
	AsOf      time.Time         `json:"as_of"`
	Detail    string            `json:"detail"`
	Facets    *ShelfQueryFacets `json:"facets,omitempty"`
}

// ShelfQueryFacets counts, over every matched model rather than the page, the
// models holding each record. A count does not prove the record holds in the
// same option combination as the other filters; the follow-up query's total does.
type ShelfQueryFacets struct {
	SaleStatuses   []ShelfStatusFacet        `json:"sale_statuses"`
	Composition    []ShelfCompositionFacet   `json:"composition"`
	Parameters     []ShelfParameterFacet     `json:"parameters"`
	Certifications []ShelfCertificationFacet `json:"certifications"`
}

// ConditionalModels counts models whose every row for this region and status
// applies only to some option combinations.
type ShelfStatusFacet struct {
	Region            string `json:"region"`
	RegionName        string `json:"region_name,omitempty"`
	Status            string `json:"status"`
	Models            int64  `json:"models"`
	ConditionalModels int64  `json:"conditional_models"`
}

type ShelfCompositionFacet struct {
	ModuleType string `json:"module_type"`
	PartID     string `json:"part_id"`
	PartName   string `json:"part_name"`
	Models     int64  `json:"models"`
}

// Missing counts models in the field's product lines without a structured
// value; their value may still sit in the description or attached text.
type ShelfParameterFacet struct {
	Field   string   `json:"field"`
	Name    string   `json:"name,omitempty"`
	Models  int64    `json:"models"`
	Missing int64    `json:"missing"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
}

type ShelfCertificationFacet struct {
	Standard     string `json:"standard"`
	StandardName string `json:"standard_name,omitempty"`
	Progress     string `json:"progress"`
	Region       string `json:"region"`
	Models       int64  `json:"models"`
}
