package types

import (
	"time"

	"gorm.io/gorm"
)

// User represents a user in the system
type User struct {
	// Unique identifier of the user
	ID string `json:"id"         gorm:"type:varchar(36);primaryKey"`
	// Username of the user
	Username string `json:"username"   gorm:"type:varchar(100);uniqueIndex;not null"`
	// Email address of the user
	Email string `json:"email"      gorm:"type:varchar(255);uniqueIndex;not null"`
	// Avatar URL of the user
	Avatar string `json:"avatar"     gorm:"type:varchar(500)"`
	// Workspace ID that the user belongs to
	SpaceID uint64 `json:"space_id"  gorm:"index"`
	// Whether the user is active
	IsActive bool `json:"is_active"  gorm:"default:true"`
	// Creation time of the user
	CreatedAt time.Time `json:"created_at"`
	// Last updated time of the user
	UpdatedAt time.Time `json:"updated_at"`
	// Deletion time of the user
	DeletedAt gorm.DeletedAt `json:"deleted_at" gorm:"index"`

	// Association relationship, not stored in the database
	Space *Space `json:"space,omitempty" gorm:"foreignKey:SpaceID"`
}
