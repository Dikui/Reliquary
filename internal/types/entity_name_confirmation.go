package types

import "time"

// ManagedNameConfirmation acknowledges one spelling and the current owner in
// another kind. An acknowledgement cannot approve future owners or kind changes.
type ManagedNameConfirmation struct {
	Name          string `json:"name"`
	OtherEntityID string `json:"other_entity_id"`
	OtherKind     string `json:"other_kind"`
}

type ManagedNameConflict struct {
	ManagedNameConfirmation
	EntityID       string `json:"entity_id,omitempty"`
	Kind           string `json:"kind"`
	KindLabel      string `json:"kind_label"`
	OtherName      string `json:"other_name"`
	OtherKindLabel string `json:"other_kind_label"`
}

type ManagedNameConflictDetails struct {
	Type      string                `json:"type"`
	Conflicts []ManagedNameConflict `json:"conflicts"`
}

// Conversion already has stable IDs for both endpoints of a confirmed pair.
type ManagedConversionNameConfirmation struct {
	ManagedNameConfirmation
	EntityID string `json:"entity_id"`
}

type EntityNameConfirmation struct {
	SpaceID        uint64    `json:"space_id" gorm:"primaryKey"`
	LibraryID      string    `json:"library_id" gorm:"primaryKey"`
	EntityAID      string    `json:"entity_a_id" gorm:"primaryKey"`
	KindA          string    `json:"kind_a" gorm:"primaryKey"`
	EntityBID      string    `json:"entity_b_id" gorm:"primaryKey"`
	KindB          string    `json:"kind_b" gorm:"primaryKey"`
	NormalizedName string    `json:"normalized_name" gorm:"primaryKey"`
	ConfirmedAt    time.Time `json:"confirmed_at"`
}
