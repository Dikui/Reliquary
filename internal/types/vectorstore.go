package types

import (
	"database/sql/driver"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EnvStoreIDPrefix is the prefix for virtual application store IDs.
const EnvStoreIDPrefix = "__env_"

// EnvLookupFunc is retained for callers that provide environment lookups.
type EnvLookupFunc func(string) string

// IsEnvStoreID checks whether an ID names a virtual application store.
func IsEnvStoreID(id string) bool { return strings.HasPrefix(id, EnvStoreIDPrefix) }

// VectorStore retains the database binding used by existing knowledge bases.
// Retrieval always uses PostgreSQL and the application's database connection.
type VectorStore struct {
	ID               string              `yaml:"id" json:"id" gorm:"type:varchar(36);primaryKey"`
	TenantID         uint64              `yaml:"tenant_id" json:"tenant_id"`
	Name             string              `yaml:"name" json:"name" gorm:"type:varchar(255);not null"`
	EngineType       RetrieverEngineType `yaml:"engine_type" json:"engine_type" gorm:"type:varchar(50);not null"`
	ConnectionConfig ConnectionConfig    `yaml:"connection_config" json:"connection_config" gorm:"type:json"`
	IndexConfig      IndexConfig         `yaml:"index_config" json:"index_config" gorm:"type:json"`
	CreatedAt        time.Time           `yaml:"created_at" json:"created_at"`
	UpdatedAt        time.Time           `yaml:"updated_at" json:"updated_at"`
	DeletedAt        gorm.DeletedAt      `yaml:"deleted_at" json:"deleted_at" gorm:"index"`
}

// TableName returns the table used by existing knowledge base bindings.
func (VectorStore) TableName() string { return "vector_stores" }

// BeforeCreate generates a UUID for a new binding record.
func (v *VectorStore) BeforeCreate(_ *gorm.DB) error {
	if v.ID == "" {
		v.ID = uuid.New().String()
	}
	return nil
}

// ConnectionConfig contains only metadata for the shared PostgreSQL connection.
type ConnectionConfig struct {
	UseDefaultConnection bool   `yaml:"use_default_connection" json:"use_default_connection,omitempty"`
	Version              string `yaml:"version" json:"version,omitempty"`
}

// Value implements driver.Valuer for PostgreSQL connection metadata.
func (c ConnectionConfig) Value() (driver.Value, error) { return json.Marshal(c) }

// Scan implements sql.Scanner for PostgreSQL connection metadata.
func (c *ConnectionConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// MaskSensitiveFields preserves metadata, which contains no credentials.
func (c ConnectionConfig) MaskSensitiveFields() ConnectionConfig { return c }

// IndexConfig is kept for existing database rows; PostgreSQL owns its indexes.
type IndexConfig struct{}

// Value implements driver.Valuer for the retained index metadata column.
func (c IndexConfig) Value() (driver.Value, error) { return json.Marshal(c) }

// Scan implements sql.Scanner for the retained index metadata column.
func (c *IndexConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	b, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(b, c)
}

// Store sources preserve the API-safe ownership projection used by shared KBs.
const (
	StoreSourceEnv         = "env"
	StoreSourceUser        = "user"
	StoreSourceUnavailable = "unavailable"
)

// StoreDisplay is the API-safe projection of a VectorStore for embedding in
// other resource responses (notably KnowledgeBase). It carries only the
// display-safe identifiers — never connection credentials.
//
// Source is one of the StoreSource* constants. EngineType is the underlying
// engine name (e.g. "postgres"). Status mirrors Source by default but
// is split out to give the UI a stable boolean-like signal independent of
// future Source value additions.
type StoreDisplay struct {
	Name       string `json:"vector_store_name,omitempty"`
	Source     string `json:"vector_store_source,omitempty"`
	EngineType string `json:"vector_store_engine_type,omitempty"`
	Status     string `json:"vector_store_status,omitempty"`
}

// DefaultStoreDisplay is the display payload for KBs that fall back to the
// tenant's env stores (VectorStoreID == nil).
func DefaultStoreDisplay() StoreDisplay {
	return StoreDisplay{
		Name:   "System default",
		Source: StoreSourceEnv,
		Status: "available",
	}
}

// UnavailableStoreDisplay is used when the bound store cannot be resolved
// (deleted row, registry miss, transient infra error). The UI can branch on
// Status to guide recovery (admin tool, rebind, etc.).
func UnavailableStoreDisplay() StoreDisplay {
	return StoreDisplay{
		Source: StoreSourceUnavailable,
		Status: "unavailable",
	}
}

// ---------------------------------------------------------------------------
// VectorStoreResponse — API response DTO
// ---------------------------------------------------------------------------

// VectorStoreResponse is the API response DTO for vector store.
// Wraps VectorStore with additional metadata (source, readonly).
type VectorStoreResponse struct {
	VectorStore
	Source   string `json:"source"`   // "env" or "user"
	ReadOnly bool   `json:"readonly"` // the application store is read-only
	Status   string `json:"status"`
}

// NewVectorStoreResponse creates a response DTO from a VectorStore
// with sensitive fields masked.
func NewVectorStoreResponse(store *VectorStore, source string, readonly bool) VectorStoreResponse {
	masked := *store
	masked.ConnectionConfig = store.ConnectionConfig.MaskSensitiveFields()
	return VectorStoreResponse{
		VectorStore: masked,
		Source:      source,
		ReadOnly:    readonly,
	}
}

// BuildEnvVectorStores returns the application's single read-only retriever.
// The arguments are retained for internal callers while configuration is fixed.
func BuildEnvVectorStores(_ string, _ EnvLookupFunc) []VectorStore {
	return []VectorStore{{
		ID: "__env_postgres__", Name: "PostgreSQL", EngineType: PostgresRetrieverEngineType,
		ConnectionConfig: ConnectionConfig{UseDefaultConnection: true},
	}}
}

// FindEnvVectorStore finds the application PostgreSQL store by its virtual ID.
func FindEnvVectorStore(driver string, lookup EnvLookupFunc, id string) *VectorStore {
	for _, store := range BuildEnvVectorStores(driver, lookup) {
		if store.ID == id {
			return &store
		}
	}
	return nil
}
