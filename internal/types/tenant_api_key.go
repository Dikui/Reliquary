package types

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/utils"
)

// TenantAPIKey is a revocable credential for the personal workspace.
// KeyHash is used for authentication lookup; APIKey is stored encrypted
// when SYSTEM_AES_KEY is set.
type TenantAPIKey struct {
	ID         uint64     `json:"id" gorm:"primaryKey;autoIncrement"`
	TenantID   *uint64    `json:"tenant_id,omitempty" gorm:"index"`
	Name       string     `json:"name" gorm:"type:varchar(128);not null"`
	KeyHash    string     `json:"-" gorm:"type:varchar(64);not null;uniqueIndex"`
	APIKey     string     `json:"api_key" gorm:"column:api_key;type:text;not null;default:''"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty"`
	RevokedAt  *time.Time `json:"revoked_at,omitempty" gorm:"index"`
	CreatedAt  time.Time  `json:"created_at"`
	UpdatedAt  time.Time  `json:"updated_at"`
}

func (k *TenantAPIKey) TenantIDValue() uint64 {
	if k == nil || k.TenantID == nil {
		return 0
	}
	return *k.TenantID
}

func (TenantAPIKey) TableName() string {
	return "tenant_api_keys"
}

func (k *TenantAPIKey) BeforeSave(tx *gorm.DB) error {
	if key := utils.GetAESKey(); key != nil && k.APIKey != "" {
		encrypted, err := utils.EncryptAESGCM(k.APIKey, key)
		if err != nil {
			// Never fall through to storing the plaintext key: abort the
			// write so the caller sees the failure instead of silently
			// persisting an unencrypted secret.
			return fmt.Errorf("encrypt tenant_api_keys.api_key (id=%d): %w", k.ID, err)
		}
		tx.Statement.SetColumn("api_key", encrypted)
	}
	return nil
}

func (k *TenantAPIKey) AfterFind(tx *gorm.DB) error {
	decrypted, err := utils.DecryptStoredSecret(k.APIKey)
	if err != nil {
		return fmt.Errorf("decrypt tenant_api_keys.api_key (id=%d): %w", k.ID, err)
	}
	k.APIKey = decrypted
	return nil
}
