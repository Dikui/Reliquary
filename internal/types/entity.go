package types

import (
	"time"
)

// Entity is a canonical entity shared by the personal space.
type Entity struct {
	ID             string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID        uint64    `json:"space_id" gorm:"not null"`
	CanonicalName  string    `json:"canonical_name" gorm:"type:varchar(512);not null"`
	NormalizedName string    `json:"-" gorm:"type:varchar(512);not null"`
	MergedIntoID   string    `json:"merged_into_id,omitempty" gorm:"type:varchar(36);not null;default:''"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	Aliases        []string  `json:"aliases" gorm:"-"`
}

// EntityInput replaces an entity's editable directory fields.
type EntityInput struct {
	CanonicalName string   `json:"canonical_name" binding:"required"`
	Aliases       []string `json:"aliases"`
}

func (Entity) TableName() string { return "entities" }

// EntityAlias stores normalized names used to resolve future mentions.
type EntityAlias struct {
	ID               string    `json:"id" gorm:"type:varchar(36);primaryKey"`
	SpaceID          uint64    `json:"space_id" gorm:"not null;uniqueIndex:idx_entity_alias"`
	EntityID         string    `json:"entity_id" gorm:"type:varchar(36);not null;index"`
	Alias            string    `json:"alias" gorm:"type:varchar(512);not null"`
	NormalizedAlias  string    `json:"normalized_alias" gorm:"type:varchar(512);not null;uniqueIndex:idx_entity_alias"`
	ResolutionMethod string    `json:"resolution_method" gorm:"type:varchar(16);not null;default:'exact'"`
	CreatedAt        time.Time `json:"created_at"`
}

func (EntityAlias) TableName() string { return "entity_aliases" }

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

type EntityQuery struct {
	Entity string `json:"entity,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

type EntityLibraryGroup struct {
	LibraryID   string  `json:"library_id"`
	LibraryName string  `json:"library_name"`
	Count       int     `json:"count"`
	Entries     []Entry `json:"entries"`
}
type EntityDetail struct {
	LibraryGroups []EntityLibraryGroup `json:"library_groups"`
	Entity        Entity               `json:"entity"`
	Entries       []Entry              `json:"entries"`
}
