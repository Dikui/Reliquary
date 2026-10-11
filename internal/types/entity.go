package types

import (
	"strings"
	"time"
)

// Entity is a canonical entity shared by the personal space.
type Entity struct {
	ID                       string                  `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID                  uint64                  `json:"space_id" gorm:"not null;uniqueIndex:idx_entity_canonical"`
	CanonicalName            string                  `json:"canonical_name" gorm:"type:varchar(512);not null"`
	NormalizedName           string                  `json:"-" gorm:"type:varchar(512);not null;uniqueIndex:idx_entity_canonical"`
	EntityType               string                  `json:"entity_type" gorm:"type:varchar(128);not null;default:'other';uniqueIndex:idx_entity_canonical"`
	Description              string                  `json:"description,omitempty" gorm:"type:text;not null;default:''"`
	NameEmbedding            EntityNameEmbedding     `json:"-" gorm:"type:vector"`
	NameEmbeddingProfileHash string                  `json:"-" gorm:"-"`
	EmbeddingModelID         string                  `json:"embedding_model_id,omitempty" gorm:"type:varchar(64);not null;default:''"`
	EmbeddingDimension       int                     `json:"embedding_dimension,omitempty" gorm:"not null;default:0"`
	EmbeddingNeedsRebuild    bool                    `json:"embedding_needs_rebuild" gorm:"not null"`
	MergedIntoID             string                  `json:"merged_into_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt                time.Time               `json:"created_at"`
	UpdatedAt                time.Time               `json:"updated_at"`
	Aliases                  []string                `json:"aliases" gorm:"-"`
	AliasResolutions         []EntityAlias           `json:"-" gorm:"-"`
	MergeSuggestions         []EntityMergeSuggestion `json:"-" gorm:"-"`
}

func (Entity) TableName() string { return "entities" }

// EntityAlias stores normalized names used to resolve future mentions.
type EntityAlias struct {
	EntityType       string    `json:"entity_type" gorm:"type:varchar(128);not null;default:'';uniqueIndex:idx_entity_alias"`
	ID               string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID          uint64    `json:"space_id" gorm:"not null;uniqueIndex:idx_entity_alias"`
	EntityID         string    `json:"entity_id" gorm:"type:varchar(36);not null;index"`
	Alias            string    `json:"alias" gorm:"type:varchar(512);not null"`
	NormalizedAlias  string    `json:"normalized_alias" gorm:"type:varchar(512);not null;uniqueIndex:idx_entity_alias"`
	ResolutionMethod string    `json:"resolution_method" gorm:"type:varchar(16);not null;default:'exact'"`
	CreatedAt        time.Time `json:"created_at"`
}

func (EntityAlias) TableName() string { return "entity_aliases" }

type EntityMergeSuggestion struct {
	EntryID             string     `json:"entry_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entity_merge_suggestion_pair"`
	ID                  string     `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID             uint64     `json:"space_id" gorm:"not null;uniqueIndex:idx_entity_merge_suggestion_pair"`
	NewEntityID         string     `json:"new_entity_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entity_merge_suggestion_pair"`
	CandidateEntityID   string     `json:"candidate_entity_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entity_merge_suggestion_pair"`
	Reason              string     `json:"reason" gorm:"type:text;not null"`
	Status              string     `json:"status" gorm:"type:varchar(16);not null;default:'pending';index"`
	CreatedAt           time.Time  `json:"created_at"`
	DecidedAt           *time.Time `json:"decided_at,omitempty"`
	NewEntityName       string     `json:"new_entity_name" gorm:"column:new_entity_name;->;-:migration"`
	CandidateEntityName string     `json:"candidate_entity_name" gorm:"column:candidate_entity_name;->;-:migration"`
}

func (EntityMergeSuggestion) TableName() string {
	return "entity_merge_suggestions"
}

// EntryEntity associates an article with a canonical entity.
type EntryEntity struct {
	SpaceID   uint64    `json:"space_id" gorm:"not null;index"`
	EntryID   string    `json:"entry_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entry_entity"`
	EntityID  string    `json:"entity_id" gorm:"type:varchar(36);not null;uniqueIndex:idx_entry_entity;index"`
	CreatedAt time.Time `json:"created_at"`
}

func (EntryEntity) TableName() string { return "entry_entities" }

type EntityMergeRecord struct {
	ID            string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID       uint64    `json:"space_id" gorm:"not null;index:idx_entity_merge_keep;index:idx_entity_merge_source"`
	KeepEntityID  string    `json:"keep_entity_id" gorm:"type:varchar(36);not null;index:idx_entity_merge_keep"`
	MergeEntityID string    `json:"merge_entity_id" gorm:"type:varchar(36);not null;index:idx_entity_merge_source"`
	Actor         string    `json:"actor" gorm:"type:varchar(255);not null"`
	CreatedAt     time.Time `json:"created_at"`
}

func (EntityMergeRecord) TableName() string { return "entity_merge_records" }

// EntityCandidate is supplied by a caller that has already recognized names.
type EntityCandidate struct {
	Name        string   `json:"name"`
	EntityType  string   `json:"entity_type"`
	Aliases     []string `json:"aliases,omitempty"`
	Description string   `json:"description,omitempty"`
}

type EntityQuery struct {
	Entity     string `json:"entity,omitempty"`
	EntityType string `json:"entity_type,omitempty"`
	Limit      int    `json:"limit,omitempty"`
	Offset     int    `json:"offset,omitempty"`
}

type EntityDetail struct {
	Entity  Entity  `json:"entity"`
	Entries []Entry `json:"entries"`
}

var EntityTypes = []string{"company", "person", "product", "project", "organization", "other"}

// NormalizeEntityType maps unknown types to other.
func NormalizeEntityType(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	for _, allowed := range EntityTypes {
		if value == allowed {
			return value
		}
	}
	return "other"
}
