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

// Tenant represents the tenant
type Tenant struct {
	ModelTiers ModelTierConfig `json:"model_tiers" gorm:"type:jsonb;not null;default:'{}'"`
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
	// Storage quota (Bytes), default is 10GB, including vector, original file, text, index, etc.
	StorageQuota int64 `yaml:"storage_quota"       json:"storage_quota"       gorm:"default:10737418240"`
	// Storage used (Bytes)
	StorageUsed int64 `yaml:"storage_used"        json:"storage_used"        gorm:"default:0"`
	// Global Context configuration for this workspace (default for all sessions)
	ContextConfig *ContextConfig `yaml:"context_config"      json:"context_config"      gorm:"type:jsonb"`
	// Global WebSearch configuration for this workspace
	WebSearchConfig *WebSearchConfig `yaml:"web_search_config"   json:"web_search_config"   gorm:"type:jsonb"`
	// Parser engine config overrides (MinerU endpoint, API key, etc.). Used when parsing documents; overrides env.
	ParserEngineConfig *ParserEngineConfig `yaml:"parser_engine_config" json:"parser_engine_config" gorm:"type:jsonb"`
	// Credentials retains encrypted metadata from existing deployments.
	Credentials *CredentialsConfig `yaml:"credentials" json:"credentials" gorm:"type:jsonb"`
	// Chat history config: knowledge base configuration for indexing and searching chat messages via vector search
	ChatHistoryConfig *ChatHistoryConfig `yaml:"chat_history_config" json:"chat_history_config" gorm:"type:jsonb"`
	// Retrieval config: global search/retrieval parameters shared by knowledge search and message search
	RetrievalConfig *RetrievalConfig `yaml:"retrieval_config" json:"retrieval_config" gorm:"type:jsonb"`
	// Memory config: workspace switch for cross-session long-term memory
	MemoryConfig *MemoryConfig `yaml:"memory_config" json:"memory_config" gorm:"type:jsonb"`
	// Creation time
	CreatedAt time.Time `yaml:"created_at"          json:"created_at"`
	// Last updated time
	UpdatedAt time.Time `yaml:"updated_at"          json:"updated_at"`
	// Deletion time
	DeletedAt gorm.DeletedAt `yaml:"deleted_at"          json:"deleted_at"          gorm:"index"`
}

// CredentialsConfig holds third-party provider credentials at the tenant level.
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
			log.Printf("[crypto] tenant credentials we_knora_cloud.app_secret: decrypt failed (SYSTEM_AES_KEY missing/rotated?), treating as unconfigured")
			c.WeKnoraCloud.AppSecret = ""
		}
	}
	return nil
}

// ParserEngineConfig holds tenant-level overrides for document parser engines (e.g. MinerU endpoint, API key).
// These values take precedence over environment variables when parsing documents.
type ParserEngineConfig struct {
	// ChatParserEngineRules selects parser engines for session-scoped chat
	// documents. Knowledge bases keep their own rules in ChunkingConfig.
	ChatParserEngineRules []ParserEngineRule `json:"chat_parser_engine_rules,omitempty"`
	MinerUEndpoint        string             `json:"mineru_endpoint"` // MinerU 自建服务端点
	MinerUAPIKey          string             `json:"mineru_api_key"`  // MinerU 云 API Key

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

func (c *ParserEngineConfig) ResolveChatParserEngine(fileType string) string {
	if c != nil {
		normalized := normalizeParserFileType(fileType)
		for _, rule := range c.ChatParserEngineRules {
			for _, candidate := range rule.FileTypes {
				if normalizeParserFileType(candidate) == normalized {
					return strings.TrimSpace(rule.Engine)
				}
			}
		}
	}
	return DefaultParserEngine(fileType)
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

// TenantSandboxConfig is one named sandbox backend a workspace maintains.
//
// It is self-contained: provider fields are not inherited from process
// environment. Leaving a required provider field empty is rejected on save.
type TenantSandboxConfig struct {
	// SandboxType is docker; disabled is the hidden policy row.
	SandboxType string `json:"sandbox_type,omitempty"`

	// ── 通用配置（跨后端生效）──────────────────────────────────

	// DefaultTimeoutSec is the per-execution timeout in seconds. 0 uses the
	// program's built-in default.
	DefaultTimeoutSec int `json:"default_timeout_sec,omitempty"`

	// AllowPrivateEndpoints permits this workspace config to reach RFC1918 or
	// loopback cluster endpoints. Link-local/cloud-metadata addresses remain
	// blocked. It is explicit in the UI instead of hidden in process env.
	AllowPrivateEndpoints bool `json:"allow_private_endpoints,omitempty"`

	// EnvVars are additional environment variables injected into every
	// sandbox created for this tenant. 🔒 Values are encrypted at rest.
	// These become visible to all scripts running in the tenant's
	// sandboxes — do not place secrets here that scripts must not access.
	EnvVars map[string]string `json:"env_vars,omitempty"`

	// SkillImage points at the snapshot that carries this config's installed
	// skills. Empty means "use the base template". Written only by the skill
	// install/remove path: MergeSandboxConfigForUpdate ignores client values
	// so a settings-form save cannot wipe or plant the pointer.
	SkillImage *SkillImageConfig `json:"skill_image,omitempty"`

	// SkillRollout decides whether sessions that already hold a sandbox of
	// this config rebuild after a skill install or removal. Empty and
	// SkillRolloutNextTurn rebuild on the next chat turn. SkillRolloutNewSession
	// leaves those sandboxes on the previous image; only sessions that start
	// afterwards boot the new snapshot.
	SkillRollout string `json:"skill_rollout,omitempty"`

	// Network is the outbound/inbound network policy applied to every sandbox
	// created from this config — chat sessions, skill installs and deep
	// connectivity probes alike. nil and the zero value mean the same thing:
	// outbound egress allowed, inbound public access closed.
	Network *SandboxNetworkPolicy `json:"network,omitempty"`

	// ── 后端专属配置（同一时刻只有一个生效，由 SandboxType 决定）───
	Docker *DockerSandboxConfig `json:"docker,omitempty"`
}

// DockerSandboxConfig addresses one Docker daemon. Image is required and plays
// the role a template ID plays for the MicroVM backends: every session
// container is created from it.
//
// The daemon endpoint is deliberately the only connection field, and TLS
// material is referenced by path rather than stored here. Client certificates
// are deployment infrastructure mounted onto the Reliquary host; keeping them
// out of the database keeps them out of backups, exports and API responses.
type DockerSandboxConfig struct {
	Image string `json:"image,omitempty"`

	// Host is the daemon endpoint in DOCKER_HOST form. Empty means the local
	// unix socket.
	Host string `json:"host,omitempty"`

	// TLSCertPath is a directory on the Reliquary host containing ca.pem,
	// cert.pem and key.pem. Required when Host is a TCP endpoint.
	TLSCertPath string `json:"tls_cert_path,omitempty"`

	// CPULimit is the number of CPU cores one sandbox may use. 0 uses the
	// built-in default.
	CPULimit float64 `json:"cpu_limit,omitempty"`

	// MemoryLimitMB caps one sandbox's memory. 0 uses the built-in default.
	MemoryLimitMB int `json:"memory_limit_mb,omitempty"`

	// PidsLimit caps how many processes one sandbox may run. 0 uses the
	// built-in default.
	PidsLimit int `json:"pids_limit,omitempty"`

	// NetworkMode is the Docker network sandboxes join: "bridge" (default) or
	// "none" for no egress. Nothing else is accepted — host and container:
	// modes share another namespace outright, and a named network is usually
	// the deployment's own compose network, which would put the sandbox next
	// to Postgres and Redis.
	NetworkMode string `json:"network_mode,omitempty"`

	// Runtime selects an alternative OCI runtime such as "runsc" (gVisor).
	// Empty uses the daemon default.
	Runtime string `json:"runtime,omitempty"`

	// IdleTTLSeconds is how long a session container may go unused before it
	// is reclaimed. The daemon has no idle timeout of its own, so this is what
	// stops an abandoned session from pinning host memory indefinitely.
	IdleTTLSeconds int `json:"idle_ttl_seconds,omitempty"`

	// HTTPTimeoutSec bounds each Engine API call. 0 uses the built-in default.
	HTTPTimeoutSec int `json:"http_timeout_sec,omitempty"`
}

const (
	// SkillRolloutNextTurn rebuilds a live session's sandbox on its next chat
	// turn after the skill image changes. This is the default.
	SkillRolloutNextTurn = "next_turn"
	// SkillRolloutNewSession leaves live sandboxes on the previous image.
	// Only a session that starts after the install or removal boots the new one.
	SkillRolloutNewSession = "new_session"
)

// RebuildsExistingOnSkillChange reports whether an install or removal should
// mark already-bound sandboxes stale. Unknown values fail toward rebuilding
// so a corrupted row cannot pin every session on a retired image.
func (c *TenantSandboxConfig) RebuildsExistingOnSkillChange() bool {
	if c == nil {
		return true
	}
	return strings.TrimSpace(c.SkillRollout) != SkillRolloutNewSession
}

// SkillImageConfig is the pointer to the snapshot that carries the skills
// installed on this sandbox config. Snapshot IDs double as template IDs, so
// nothing else is needed to boot sessions from it.
//
// It holds no secrets, so it is not encrypted by TenantSandboxConfig.Value.
type SkillImageConfig struct {
	// SnapshotID is the currently effective snapshot; empty = base template.
	SnapshotID string `json:"snapshot_id,omitempty"`
	// Generation increments on every successful install/remove, for naming
	// and troubleshooting.
	Generation int `json:"generation,omitempty"`
	// BuiltAt records when this generation was produced.
	BuiltAt time.Time `json:"built_at,omitempty"`
	// BaseTemplateID is the template this chain was originally built from;
	// the rebuild path starts over from it.
	BaseTemplateID string `json:"base_template_id,omitempty"`
	// OwnerFingerprint identifies the provider account that owns the snapshot.
	// Snapshots are invisible across accounts, so a mismatch means "fall back
	// to the base template" rather than "fail".
	OwnerFingerprint string `json:"owner_fingerprint,omitempty"`
}

// Value implements the driver.Valuer interface. Every secret-bearing field
// (all EnvVars values) is
// encrypted before persisting. EnvVars are included because environment
// variables routinely carry credentials, and their values are handed to tenant
// scripts verbatim. The receiver is never mutated: nested structs and the map
// are copied first.
func (c *TenantSandboxConfig) Value() (driver.Value, error) {
	if c == nil {
		return nil, nil
	}
	cp := *c
	key := utils.GetAESKey()

	encrypt := func(plain string) string {
		if plain == "" || key == nil {
			return plain
		}
		encrypted, err := utils.EncryptAESGCM(plain, key)
		if err != nil {
			return plain
		}
		return encrypted
	}

	// Keys stay readable so operators can still see which variables are set.
	if len(c.EnvVars) > 0 {
		envVars := make(map[string]string, len(c.EnvVars))
		for name, value := range c.EnvVars {
			envVars[name] = encrypt(value)
		}
		cp.EnvVars = envVars
	}

	return json.Marshal(&cp)
}

// Scan implements the sql.Scanner interface. Secrets that cannot be decrypted
// (missing or rotated SYSTEM_AES_KEY) are blanked and logged rather than
// failing the whole load, matching ModelParameters.Scan — a tenant must stay
// listable even if its sandbox credentials became unreadable.
func (c *TenantSandboxConfig) Scan(value interface{}) error {
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

	decrypt := func(stored, label string) string {
		if stored == "" {
			return ""
		}
		if plain, ok := utils.DecryptStoredSecretLenient(stored); ok {
			return plain
		}
		log.Printf("[crypto] tenant_sandbox_config.%s: decrypt failed "+
			"(SYSTEM_AES_KEY missing/rotated?), treating as unconfigured", label)
		return ""
	}

	for name, stored := range c.EnvVars {
		c.EnvVars[name] = decrypt(stored, "env_vars."+name)
	}

	return nil
}
