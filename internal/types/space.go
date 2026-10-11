package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"gorm.io/gorm"

	"reliquary/internal/utils"
)

// Space represents the space
type Space struct {
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
	// Parser engine config overrides (MinerU endpoint, API key, etc.). Used when parsing documents; overrides env.
	ParserEngineConfig *ParserEngineConfig `yaml:"parser_engine_config" json:"parser_engine_config" gorm:"type:jsonb"`
	// Credentials retains encrypted metadata from existing deployments.
	Credentials *CredentialsConfig `yaml:"credentials" json:"credentials" gorm:"type:jsonb"`
	// Retrieval config: global search/retrieval parameters used by knowledge search
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

// ParserEngineConfig holds space-level overrides for document parser engines (e.g. MinerU endpoint, API key).
// These values take precedence over environment variables when parsing documents.
type ParserEngineConfig struct {
	MinerUEndpoint string `json:"mineru_endpoint"` // MinerU 自建服务端点
	MinerUAPIKey   string `json:"mineru_api_key"`  // MinerU 云 API Key

	// MinerU 自建解析参数
	MinerUModel         string `json:"mineru_model,omitempty"`          // backend: pipeline, vlm-*, hybrid-*
	MinerUVLMServerURL  string `json:"mineru_vlm_server_url,omitempty"` // vLLM 服务器地址 (vlm-http-client / hybrid-http-client)
	MinerUEnableFormula *bool  `json:"mineru_enable_formula,omitempty"`
	MinerUEnableTable   *bool  `json:"mineru_enable_table,omitempty"`
	MinerUParseMethod   string `json:"mineru_parse_method,omitempty"`
	// MinerUEnableOCR is retained for compatibility with configurations saved
	// before parse_method supported auto/ocr/txt.
	MinerUEnableOCR *bool  `json:"mineru_enable_ocr,omitempty"`
	MinerULanguage  string `json:"mineru_language,omitempty"`

	// MinerU 云 API 解析参数
	MinerUCloudModel         string `json:"mineru_cloud_model,omitempty"` // model_version: pipeline, vlm, MinerU-HTML
	MinerUCloudEnableFormula *bool  `json:"mineru_cloud_enable_formula,omitempty"`
	MinerUCloudEnableTable   *bool  `json:"mineru_cloud_enable_table,omitempty"`
	MinerUCloudEnableOCR     *bool  `json:"mineru_cloud_enable_ocr,omitempty"`
	MinerUCloudLanguage      string `json:"mineru_cloud_language,omitempty"`
}

const (
	MinerUParseMethodAuto = "auto"
	MinerUParseMethodOCR  = "ocr"
	MinerUParseMethodText = "txt"
)

// ResolveMinerUParseMethod normalizes the explicit MinerU parse method and
// maps the legacy OCR toggle to the closest safe behavior. The old enabled
// value maps to auto instead of ocr so digital PDFs keep their native text
// layer while scanned PDFs are still detected and OCRed by MinerU.
func ResolveMinerUParseMethod(method string, legacyOCREnabled *bool) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case MinerUParseMethodAuto:
		return MinerUParseMethodAuto
	case MinerUParseMethodOCR:
		return MinerUParseMethodOCR
	case MinerUParseMethodText:
		return MinerUParseMethodText
	}

	if legacyOCREnabled != nil && !*legacyOCREnabled {
		return MinerUParseMethodText
	}
	return MinerUParseMethodAuto
}

// ToOverridesMap returns a map suitable for ParserEngineOverrides in parse requests.
// Keys are snake_case (mineru_endpoint, mineru_api_key, etc.).
func (c *ParserEngineConfig) ToOverridesMap() map[string]string {
	if c == nil {
		return nil
	}
	m := make(map[string]string)
	if c.MinerUEndpoint != "" {
		m["mineru_endpoint"] = c.MinerUEndpoint
	}
	if c.MinerUAPIKey != "" {
		m["mineru_api_key"] = c.MinerUAPIKey
	}
	if c.MinerUModel != "" {
		m["mineru_model"] = c.MinerUModel
	}
	if c.MinerUVLMServerURL != "" {
		m["mineru_vlm_server_url"] = c.MinerUVLMServerURL
	}
	if c.MinerUEnableFormula != nil {
		m["mineru_enable_formula"] = fmt.Sprintf("%v", *c.MinerUEnableFormula)
	}
	if c.MinerUEnableTable != nil {
		m["mineru_enable_table"] = fmt.Sprintf("%v", *c.MinerUEnableTable)
	}
	if c.MinerUParseMethod != "" || c.MinerUEnableOCR != nil {
		m["mineru_parse_method"] = ResolveMinerUParseMethod(c.MinerUParseMethod, c.MinerUEnableOCR)
	}
	if c.MinerUEnableOCR != nil {
		m["mineru_enable_ocr"] = fmt.Sprintf("%v", *c.MinerUEnableOCR)
	}
	if c.MinerULanguage != "" {
		m["mineru_language"] = c.MinerULanguage
	}
	if c.MinerUCloudModel != "" {
		m["mineru_cloud_model"] = c.MinerUCloudModel
	}
	if c.MinerUCloudEnableFormula != nil {
		m["mineru_cloud_enable_formula"] = fmt.Sprintf("%v", *c.MinerUCloudEnableFormula)
	}
	if c.MinerUCloudEnableTable != nil {
		m["mineru_cloud_enable_table"] = fmt.Sprintf("%v", *c.MinerUCloudEnableTable)
	}
	if c.MinerUCloudEnableOCR != nil {
		m["mineru_cloud_enable_ocr"] = fmt.Sprintf("%v", *c.MinerUCloudEnableOCR)
	}
	if c.MinerUCloudLanguage != "" {
		m["mineru_cloud_language"] = c.MinerUCloudLanguage
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// Value implements the driver.Valuer interface for ParserEngineConfig
func (c *ParserEngineConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	return json.Marshal(c)
}

// Scan implements the sql.Scanner interface for ParserEngineConfig
func (c *ParserEngineConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// SingleSpaceID identifies the only personal space.
const SingleSpaceID uint64 = 1
