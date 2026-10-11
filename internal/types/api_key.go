package types

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/utils"
)

// APIKey is a revocable credential for the personal workspace.
// KeyHash is used for authentication lookup; APIKey is stored encrypted
// when SYSTEM_AES_KEY is set.
type APIKey struct {
	ID         uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	SpaceID    *uint64    `json:"space_id,omitempty" gorm:"index"`
	Name       string     `json:"name" gorm:"type:varchar(128);not null"`
	KeyHash    string     `json:"-" gorm:"type:varchar(64);not null;uniqueIndex"`
	APIKey     string     `json:"api_key" gorm:"column:api_key;type:text;not null;default:''"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" gorm:"index"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (k *APIKey) SpaceIDValue() uint64 {
	if k == nil || k.SpaceID == nil {
		return 0
	}
	return *k.SpaceID
}

func (APIKey) TableName() string {
	return "api_keys"
}

func (k *APIKey) BeforeSave(tx *gorm.DB) error {
	if key := utils.GetAESKey(); key != nil && k.APIKey != "" {
		encrypted, err := utils.EncryptAESGCM(k.APIKey, key)
		if err != nil {
			// Never fall through to storing the plaintext key: abort the
			// write so the caller sees the failure instead of silently
			// persisting an unencrypted secret.
			return fmt.Errorf("encrypt api_keys.api_key (id=%d): %w", k.ID, err)
		}
		tx.Statement.SetColumn("api_key", encrypted)
	}
	return nil
}

func (k *APIKey) AfterFind(tx *gorm.DB) error {
	decrypted, err := utils.DecryptStoredSecret(k.APIKey)
	if err != nil {
		return fmt.Errorf("decrypt api_keys.api_key (id=%d): %w", k.ID, err)
	}
	k.APIKey = decrypted
	return nil
}
