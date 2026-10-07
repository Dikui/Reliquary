package types

import "time"

const (
	KnowledgeEvidenceCurrent    = "current"
	KnowledgeEvidenceSuperseded = "superseded"
	KnowledgeExtractionPending  = "pending"
	KnowledgeExtractionComplete = "complete"
	KnowledgeExtractionFailed   = "failed"

	KnowledgeFactAttribute  = "attribute"
	KnowledgeFactRelation   = "relation"
	KnowledgeFactCurrent    = "current"
	KnowledgeFactSuperseded = "superseded"
	KnowledgeFactCorrected  = "corrected"

	KnowledgeEventRelationSameOccurrence = "same_occurrence"
	KnowledgeEventRelationSameStory      = "same_story"
	KnowledgeEventRelationUnrelated      = "unrelated"
	KnowledgeEvidenceSourceKindRoundup   = "roundup"
)

// KnowledgeEntity is a canonical object shared by the whole team space.
type KnowledgeEntity struct {
	ID                       string                           `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID                 uint64                           `json:"tenant_id" gorm:"not null;uniqueIndex:idx_kce_canonical"`
	KnowledgeBaseID          string                           `json:"knowledge_base_id,omitempty" gorm:"type:varchar(36);default:null"`
	CanonicalName            string                           `json:"canonical_name" gorm:"type:varchar(512);not null"`
	NormalizedName           string                           `json:"-" gorm:"type:varchar(512);not null;uniqueIndex:idx_kce_canonical"`
	EntityType               string                           `json:"entity_type" gorm:"type:varchar(128);not null;default:'';uniqueIndex:idx_kce_canonical"`
	Description              string                           `json:"description,omitempty" gorm:"type:text;not null;default:''"`
	NameEmbedding            ContextNameEmbedding             `json:"-" gorm:"type:vector"`
	NameEmbeddingProfileHash string                           `json:"-" gorm:"-"`
	EmbeddingModelID         string                           `json:"embedding_model_id,omitempty" gorm:"type:varchar(64);not null;default:''"`
	EmbeddingDimension       int                              `json:"embedding_dimension,omitempty" gorm:"not null;default:0"`
	EmbeddingNeedsRebuild    bool                             `json:"embedding_needs_rebuild" gorm:"not null"`
	MergedIntoID             string                           `json:"merged_into_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt                time.Time                        `json:"created_at"`
	UpdatedAt                time.Time                        `json:"updated_at"`
	Aliases                  []string                         `json:"aliases,omitempty" gorm:"-"`
	AliasResolutions         []KnowledgeEntityAlias           `json:"-" gorm:"-"`
	MergeSuggestions         []KnowledgeEntityMergeSuggestion `json:"-" gorm:"-"`
}

func (KnowledgeEntity) TableName() string { return "knowledge_context_entities" }

// KnowledgeEntityAlias stores normalized names used to resolve future mentions.
type KnowledgeEntityAlias struct {
	EntityType       string    `json:"entity_type" gorm:"type:varchar(128);not null;default:'';uniqueIndex:idx_kcea_alias"`
	ID               string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64    `json:"tenant_id" gorm:"not null;uniqueIndex:idx_kcea_alias"`
	KnowledgeBaseID  string    `json:"knowledge_base_id,omitempty" gorm:"type:varchar(36);default:null"`
	EntityID         string    `json:"entity_id" gorm:"type:varchar(36);not null;index"`
	Alias            string    `json:"alias" gorm:"type:varchar(512);not null"`
	NormalizedAlias  string    `json:"normalized_alias" gorm:"type:varchar(512);not null;uniqueIndex:idx_kcea_alias"`
	EvidenceID       string    `json:"evidence_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	Quote            string    `json:"quote,omitempty" gorm:"type:text;not null;default:''"`
	ResolutionMethod string    `json:"resolution_method" gorm:"type:varchar(16);not null;default:'exact'"`
	CreatedAt        time.Time `json:"created_at"`
}

func (KnowledgeEntityAlias) TableName() string { return "knowledge_context_entity_aliases" }

type KnowledgeEntityMergeSuggestion struct {
	ID                  string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID            uint64     `json:"tenant_id" gorm:"not null;uniqueIndex:idx_kcems_pair"`
	KnowledgeBaseID     string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index"`
	NewEntityID         string     `json:"new_entity_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcems_pair"`
	CandidateEntityID   string     `json:"candidate_entity_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcems_pair"`
	EvidenceID          string     `json:"evidence_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcems_pair"`
	Reason              string     `json:"reason" gorm:"type:text;not null"`
	Quote               string     `json:"quote" gorm:"type:text;not null"`
	Status              string     `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	CreatedAt           time.Time  `json:"created_at"`
	DecidedAt           *time.Time `json:"decided_at,omitempty"`
	NewEntityName       string     `json:"new_entity_name" gorm:"column:new_entity_name;->;-:migration"`
	CandidateEntityName string     `json:"candidate_entity_name" gorm:"column:candidate_entity_name;->;-:migration"`
}

func (KnowledgeEntityMergeSuggestion) TableName() string {
	return "knowledge_context_merge_suggestions"
}

// KnowledgeEvidence is an immutable source snapshot. Body is kept with its hash
// so facts and reports can resolve their citations after a message is updated.
type KnowledgeEvidence struct {
	KnowledgeBaseName  string      `json:"knowledge_base_name,omitempty" gorm:"-"`
	ID                 string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID           uint64      `json:"tenant_id" gorm:"not null;index:idx_kcevid_message;uniqueIndex:idx_kcevid_version;index:idx_kcevid_hash"`
	KnowledgeBaseID    string      `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kcevid_message;uniqueIndex:idx_kcevid_version;index:idx_kcevid_hash"`
	KnowledgeID        string      `json:"knowledge_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcevid_version;index:idx_kcevid_hash"`
	Version            int         `json:"version" gorm:"not null;uniqueIndex:idx_kcevid_version"`
	BodyHash           string      `json:"body_hash" gorm:"type:char(64);not null;index:idx_kcevid_hash"`
	SupersededByID     string      `json:"superseded_by_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	Title              string      `json:"title" gorm:"type:text;not null;default:''"`
	Body               string      `json:"body" gorm:"type:text;not null"`
	Author             string      `json:"author,omitempty" gorm:"type:text;not null;default:''"`
	Publisher          string      `json:"publisher,omitempty" gorm:"type:text;not null;default:''"`
	OriginalURL        string      `json:"original_url,omitempty" gorm:"type:text;not null;default:''"`
	PublishedAt        string      `json:"published_at,omitempty" gorm:"type:text;not null;default:''"`
	ImageRefs          StringArray `json:"image_refs,omitempty" gorm:"type:json"`
	SourceKind         string      `json:"source_kind,omitempty" gorm:"type:varchar(32);not null;default:''"`
	Status             string      `json:"status" gorm:"type:varchar(16);not null;default:'current';index"`
	ExtractionStatus   string      `json:"extraction_status" gorm:"type:varchar(16);not null;default:'pending';index"`
	ExtractionCoverage string      `json:"extraction_coverage" gorm:"type:varchar(16);not null;default:'complete'"`
	ProcessedSegments  int         `json:"processed_segments" gorm:"not null;default:0"`
	TotalSegments      int         `json:"total_segments" gorm:"not null;default:0"`
	ModelCalls         JSON        `json:"model_calls" gorm:"type:jsonb;not null;default:'[]'"`
	ExtractionError    string      `json:"extraction_error,omitempty" gorm:"type:text;not null;default:''"`
	CreatedAt          time.Time   `json:"created_at"`
}

func (KnowledgeEvidence) TableName() string { return "knowledge_context_evidence" }

// KnowledgeFact is a sourced assertion. Relations use ObjectEntityID; scalar
// attributes use Value, ValueType, and Unit. Conflicting assertions coexist.
type KnowledgeFact struct {
	KnowledgeBaseName string     `json:"knowledge_base_name,omitempty" gorm:"-"`
	ID                string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID          uint64     `json:"tenant_id" gorm:"not null;index:idx_kcf_scope"`
	KnowledgeBaseID   string     `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kcf_scope"`
	SubjectEntityID   string     `json:"subject_entity_id" gorm:"type:varchar(36);not null;index"`
	Kind              string     `json:"kind" gorm:"type:varchar(16);not null;default:'attribute';index"`
	Predicate         string     `json:"predicate" gorm:"type:varchar(512);not null;index"`
	OriginalPredicate string     `json:"original_predicate,omitempty" gorm:"type:varchar(512);not null;default:''"`
	Value             JSON       `json:"value,omitempty" gorm:"type:json"`
	ValueType         string     `json:"value_type,omitempty" gorm:"type:varchar(32);not null;default:''"`
	Unit              string     `json:"unit,omitempty" gorm:"type:varchar(128);not null;default:''"`
	ObjectEntityID    string     `json:"object_entity_id,omitempty" gorm:"type:varchar(36);not null;default:'';index"`
	EffectiveFrom     *time.Time `json:"effective_from,omitempty" gorm:"index"`
	EffectiveTo       *time.Time `json:"effective_to,omitempty" gorm:"index"`
	EvidenceID        string     `json:"evidence_id" gorm:"type:varchar(36);not null;index"`
	Quote             string     `json:"quote" gorm:"type:text;not null"`
	Status            string     `json:"status" gorm:"type:varchar(16);not null;default:'current';index"`
	SupersedesID      string     `json:"supersedes_id,omitempty" gorm:"type:varchar(36);not null;default:'';index"`
	CorrectedBy       string     `json:"corrected_by,omitempty" gorm:"type:varchar(36);not null;default:''"`
	CorrectionNote    string     `json:"correction_note,omitempty" gorm:"type:text;not null;default:''"`
	// CorrectionActor identifies the authenticated principal that made this version.
	CorrectionActor string    `json:"correction_actor,omitempty" gorm:"type:varchar(255);not null;default:''"`
	CreatedAt       time.Time `json:"created_at"`
}

func (KnowledgeFact) TableName() string { return "knowledge_context_facts" }

// KnowledgeEvent represents one real-world change, possibly reported by many
// messages. Evidence links and attention assessments are stored separately.
type KnowledgeEvent struct {
	Sources         []KnowledgeContextSource `json:"sources,omitempty" gorm:"-"`
	ID              string                   `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64                   `json:"tenant_id" gorm:"not null;index:idx_kcevent_scope"`
	KnowledgeBaseID string                   `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kcevent_scope"`
	Title           string                   `json:"title" gorm:"type:varchar(512);not null"`
	EventType       string                   `json:"event_type,omitempty" gorm:"type:varchar(128);not null;default:''"`
	Description     string                   `json:"description,omitempty" gorm:"type:text;not null;default:''"`
	OccurredAt      *time.Time               `json:"occurred_at,omitempty" gorm:"index"`
	EntityIDs       StringArray              `json:"entity_ids,omitempty" gorm:"type:json"`
	EvidenceIDs     StringArray              `json:"evidence_ids,omitempty" gorm:"type:json"`
	SourceCount     int                      `json:"source_count" gorm:"not null;default:0"`
	Heat            float64                  `json:"heat" gorm:"not null;default:0"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
	SourceEntityIDs StringArray              `json:"-" gorm:"-"`
	Relations       []KnowledgeEventRelation `json:"relations,omitempty" gorm:"-"`
}

func (KnowledgeEvent) TableName() string { return "knowledge_context_events" }

// KnowledgeEventRelation links an event to another visible event. EvidenceID
// identifies the source report that supports this directed relation.
type KnowledgeEventRelation struct {
	Relation       string `json:"relation"`
	RelatedEventID string `json:"related_event_id"`
	RelatedTitle   string `json:"related_title,omitempty"`
	EvidenceID     string `json:"evidence_id"`
}

type KnowledgeEventEvidence struct {
	ID                string      `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID          uint64      `json:"tenant_id" gorm:"not null;uniqueIndex:idx_kcee_pair;index:idx_kcee_scope;index:idx_kcee_related_event,priority:1"`
	KnowledgeBaseID   string      `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kcee_scope;index:idx_kcee_related_event,priority:2"`
	EventID           string      `json:"event_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcee_pair"`
	EvidenceID        string      `json:"evidence_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kcee_pair;index"`
	EntityIDs         StringArray `json:"entity_ids" gorm:"type:json;not null"`
	SourceTitle       string      `json:"source_title" gorm:"type:varchar(512);not null;default:''"`
	SourceEventType   string      `json:"source_event_type" gorm:"type:varchar(128);not null;default:''"`
	SourceDescription string      `json:"source_description" gorm:"type:text;not null;default:''"`
	SourceOccurredAt  *time.Time  `json:"source_occurred_at,omitempty" gorm:"index"`
	SourceKey         string      `json:"source_key" gorm:"type:varchar(512);not null;default:''"`
	Relation          string      `json:"relation" gorm:"type:varchar(32);not null;default:''"`
	RelatedEventID    string      `json:"related_event_id,omitempty" gorm:"type:varchar(36);not null;default:'';index:idx_kcee_related_event,priority:3"`
	CreatedAt         time.Time   `json:"created_at"`
}

func (KnowledgeEventEvidence) TableName() string { return "knowledge_context_event_evidence" }

// KnowledgeAttention records the independent reason an otherwise eligible
// message or event is useful to follow; it never participates in intake quality.
type KnowledgeAttention struct {
	ID              string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64    `json:"tenant_id" gorm:"not null;index:idx_kca_scope;uniqueIndex:idx_kca_evidence"`
	KnowledgeBaseID string    `json:"knowledge_base_id" gorm:"type:varchar(36);not null;index:idx_kca_scope;uniqueIndex:idx_kca_evidence"`
	EvidenceID      string    `json:"evidence_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_kca_evidence"`
	EventID         string    `json:"event_id,omitempty" gorm:"type:varchar(36);not null;default:'';index"`
	Novelty         float64   `json:"novelty" gorm:"not null;default:0"`
	Impact          float64   `json:"impact" gorm:"not null;default:0"`
	Credibility     float64   `json:"credibility" gorm:"not null;default:0"`
	Relevance       float64   `json:"relevance" gorm:"not null;default:0"`
	Actionability   float64   `json:"actionability" gorm:"not null;default:0"`
	Score           float64   `json:"score" gorm:"not null;default:0;index"`
	Reason          string    `json:"reason" gorm:"type:text;not null;default:''"`
	CreatedAt       time.Time `json:"created_at"`
}

func (KnowledgeAttention) TableName() string { return "knowledge_context_attention" }

type KnowledgeContextSource struct {
	KnowledgeBaseID   string `json:"knowledge_base_id"`
	KnowledgeBaseName string `json:"knowledge_base_name"`
}

type KnowledgeContextQuery struct {
	Scope           string     `json:"scope,omitempty"`
	DomainID        string     `json:"domain_id,omitempty"`
	KnowledgeBaseID string     `json:"knowledge_base_id,omitempty"`
	KnowledgeIDs    []string   `json:"knowledge_ids,omitempty"`
	EntityID        string     `json:"entity_id,omitempty"`
	Entity          string     `json:"entity,omitempty"`
	Predicate       string     `json:"predicate,omitempty"`
	Relation        string     `json:"relation,omitempty"`
	ObjectEntityID  string     `json:"object_entity_id,omitempty"`
	MinValue        *float64   `json:"min_value,omitempty"`
	MaxValue        *float64   `json:"max_value,omitempty"`
	Unit            string     `json:"unit,omitempty"`
	EventID         string     `json:"event_id,omitempty"`
	Since           *time.Time `json:"since,omitempty"`
	Until           *time.Time `json:"until,omitempty"`
	IncludeHistory  bool       `json:"include_history,omitempty"`
	Limit           int        `json:"limit,omitempty"`
	Offset          int        `json:"offset,omitempty"`
}

type KnowledgeContextResult struct {
	Entities  []KnowledgeEntity    `json:"entities"`
	Facts     []KnowledgeFact      `json:"facts"`
	Events    []KnowledgeEvent     `json:"events"`
	Evidence  []KnowledgeEvidence  `json:"evidence"`
	Attention []KnowledgeAttention `json:"attention"`
}

type KnowledgeFactCorrection struct {
	Value          JSON       `json:"value,omitempty"`
	ValueType      string     `json:"value_type,omitempty"`
	Unit           string     `json:"unit,omitempty"`
	EffectiveFrom  *time.Time `json:"effective_from,omitempty"`
	EffectiveTo    *time.Time `json:"effective_to,omitempty"`
	Quote          string     `json:"quote"`
	CorrectionNote string     `json:"correction_note"`
}
