package types

import (
	"time"

	"gorm.io/gorm"
)

// KnowledgeDomain defines the editable extraction vocabulary for a team domain.
type KnowledgeDomain struct {
	ID              string             `json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID        uint64             `json:"tenant_id" gorm:"not null"`
	Name            string             `json:"name" gorm:"type:varchar(255);not null"`
	Description     string             `json:"description"`
	EntityTypes     []string           `json:"entity_types" gorm:"serializer:json;type:jsonb;not null"`
	Fields          []DomainField      `json:"fields" gorm:"serializer:json;type:jsonb;not null"`
	Relations       []DomainRelation   `json:"relations" gorm:"serializer:json;type:jsonb;not null"`
	UnitFamilies    []DomainUnitFamily `json:"unit_families" gorm:"serializer:json;type:jsonb;not null"`
	ExtractionHints string             `json:"extraction_hints"`
	IsBuiltin       bool               `json:"is_builtin"`
	CreatedBy       string             `json:"created_by" gorm:"type:varchar(36)"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
	DeletedAt       gorm.DeletedAt     `json:"deleted_at" gorm:"index"`
}

type DomainField struct {
	Key         string   `json:"key"`
	Label       string   `json:"label"`
	Aliases     []string `json:"aliases"`
	ValueType   string   `json:"value_type"`
	UnitFamily  string   `json:"unit_family"`
	EntityTypes []string `json:"entity_types"`
}

type DomainRelation struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Aliases      []string `json:"aliases"`
	SubjectTypes []string `json:"subject_types"`
	ObjectTypes  []string `json:"object_types"`
}

type DomainUnitFamily struct {
	Key           string       `json:"key"`
	CanonicalUnit string       `json:"canonical_unit"`
	Units         []DomainUnit `json:"units"`
}

type DomainUnit struct {
	Name    string   `json:"name"`
	Aliases []string `json:"aliases"`
	Factor  float64  `json:"factor"`
}

// DomainOpenField summarizes source fields that have no template mapping.
type DomainOpenField struct {
	Name         string          `json:"name"`
	Count        int64           `json:"count"`
	ExampleFacts []KnowledgeFact `json:"example_facts"`
}

type DomainFieldMapping struct {
	OpenField string `json:"open_field"`
	FieldKey  string `json:"field_key"`
}
