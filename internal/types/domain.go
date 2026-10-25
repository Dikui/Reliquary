package types

import "time"

// Domain is a user-maintained direction available to all libraries.
type Domain struct {
	ID             string    `json:"id"         gorm:"primaryKey"`
	SpaceID        uint64    `json:"space_id"`
	Name           string    `json:"name"`
	NormalizedName string    `json:"-"`
	Definition     string    `json:"definition"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// DomainInput replaces a domain name and its one-sentence definition.
type DomainInput struct {
	Name       string `json:"name"       binding:"required"`
	Definition string `json:"definition" binding:"required"`
}

// EntryDomain associates a material with a maintained domain.
type EntryDomain struct {
	SpaceID   uint64
	EntryID   string
	DomainID  string
	CreatedAt time.Time
}
