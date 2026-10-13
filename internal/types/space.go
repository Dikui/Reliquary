package types

import (
	"database/sql/driver"
	"encoding/json"
	"log"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/utils"
)

// Space represents the space
type Space struct {
	EmbeddingModelID string `json:"embedding_model_id"`
	// ID
	ID uint64 `yaml:"id"                  json:"id"                  gorm:"primaryKey"`
	// Name
	Name string `yaml:"name"                json:"name"`
	// Description
	Description string `yaml:"description"         json:"description"`
	// Status
	Status string `yaml:"status"              json:"status"              gorm:"default:'active'"`
	// Business
	Business string `yaml:"business"            json:"business"`
	// Storage used (Bytes)
	StorageUsed int64 `yaml:"storage_used"        json:"storage_used"        gorm:"default:0"`
	// Credentials retains encrypted metadata from existing deployments.
	Credentials *CredentialsConfig `yaml:"credentials" json:"credentials" gorm:"type:jsonb"`
	// Retrieval config: global search/retrieval parameters used by entry search
	RetrievalConfig *RetrievalConfig `yaml:"retrieval_config" json:"retrieval_config" gorm:"type:jsonb"`
	// Creation time
	CreatedAt time.Time `yaml:"created_at"          json:"created_at"`
	// Last updated time
	UpdatedAt time.Time `yaml:"updated_at"          json:"updated_at"`
	// Deletion time
	DeletedAt gorm.DeletedAt `yaml:"deleted_at"          json:"deleted_at"          gorm:"index"`
}

// CredentialsConfig holds third-party provider credentials at the space level.
// Stored as a single JSONB column; each provider is a nested object so new
// providers can be added without schema changes.
type CredentialsConfig struct {
	WeKnoraCloud *WeKnoraCloudCredentials `json:"weknoracloud,omitempty"`
}

// WeKnoraCloudCredentials stores WeKnoraCloud AppID and AppSecret.
// AppSecret is AES-256 encrypted before persisting to database.
type WeKnoraCloudCredentials struct {
	AppID     string `json:"app_id"`
	AppSecret string `json:"app_secret"`
}

// Value implements the driver.Valuer interface for CredentialsConfig
func (c *CredentialsConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	cp := *c
	if cp.WeKnoraCloud != nil && cp.WeKnoraCloud.AppSecret != "" {
		if key := utils.GetAESKey(); key != nil {
			if encrypted, err := utils.EncryptAESGCM(cp.WeKnoraCloud.AppSecret, key); err == nil {
				cp.WeKnoraCloud = &WeKnoraCloudCredentials{AppID: cp.WeKnoraCloud.AppID, AppSecret: encrypted}
			}
		}
	}
	return json.Marshal(cp)
}

// Scan implements the sql.Scanner interface for CredentialsConfig
func (c *CredentialsConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	if c.WeKnoraCloud != nil {
		if plain, ok := utils.DecryptStoredSecretLenient(c.WeKnoraCloud.AppSecret); ok {
			c.WeKnoraCloud.AppSecret = plain
		} else {
			log.Printf("[crypto] space credentials we_knora_cloud.app_secret: decrypt failed (SYSTEM_AES_KEY missing/rotated?), treating as unconfigured")
			c.WeKnoraCloud.AppSecret = ""
		}
	}
	return nil
}

// SingleSpaceID identifies the only personal space.
const SingleSpaceID uint64 = 1
