package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestIsEnvStoreID(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		expected bool
	}{
		{"env postgres ID", "__env_postgres__", true},

		{"env prefix only", "__env_", true},
		{"UUID ID", "550e8400-e29b-41d4-a716-446655440000", false},
		{"empty string", "", false},
		{"similar but not prefix", "_env_postgres__", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsEnvStoreID(tt.id))
		})
	}
}
func TestVectorStore_BeforeCreate(t *testing.T) {
	t.Run("generates UUID when ID is empty", func(t *testing.T) {
		v := &VectorStore{}
		err := v.BeforeCreate(&gorm.DB{})
		require.NoError(t, err)
		assert.NotEmpty(t, v.ID)
		assert.Len(t, v.ID, 36) // UUID format: 8-4-4-4-12
	})

	t.Run("preserves existing ID", func(t *testing.T) {
		v := &VectorStore{ID: "existing-id"}
		err := v.BeforeCreate(&gorm.DB{})
		require.NoError(t, err)
		assert.Equal(t, "existing-id", v.ID)
	})
}
func TestVectorStore_TableName(t *testing.T) {
	assert.Equal(t, "vector_stores", VectorStore{}.TableName())
}

func TestConnectionConfig_ValueScan(t *testing.T) {
	t.Run("round-trip PostgreSQL metadata", func(t *testing.T) {
		original := ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}
		raw, err := original.Value()
		require.NoError(t, err)

		var intermediate map[string]interface{}
		require.NoError(t, json.Unmarshal(raw.([]byte), &intermediate))
		assert.Equal(t, true, intermediate["use_default_connection"])
		assert.Equal(t, "16.2", intermediate["version"])

		var scanned ConnectionConfig
		require.NoError(t, scanned.Scan(raw.([]byte)))
		assert.Equal(t, original, scanned)
	})

	t.Run("empty metadata is omitted", func(t *testing.T) {
		raw, err := ConnectionConfig{}.Value()
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(raw.([]byte)))
	})

	t.Run("Scan nil value returns no error", func(t *testing.T) {
		var c ConnectionConfig
		assert.NoError(t, c.Scan(nil))
	})

	t.Run("Scan non-byte value returns no error", func(t *testing.T) {
		var c ConnectionConfig
		assert.NoError(t, c.Scan(42))
	})

	t.Run("original struct is not mutated by Value", func(t *testing.T) {
		original := ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}
		_, err := original.Value()
		require.NoError(t, err)
		assert.Equal(t, ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}, original)
	})
}

func TestConnectionConfig_MaskSensitiveFields(t *testing.T) {
	t.Run("preserves PostgreSQL metadata", func(t *testing.T) {
		c := ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}
		masked := c.MaskSensitiveFields()
		assert.True(t, masked.UseDefaultConnection)
		assert.Equal(t, "16.2", masked.Version)
	})

	t.Run("preserves empty metadata", func(t *testing.T) {
		assert.Equal(t, ConnectionConfig{}, (ConnectionConfig{}).MaskSensitiveFields())
	})

	t.Run("does not mutate original", func(t *testing.T) {
		c := ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}
		_ = c.MaskSensitiveFields()
		assert.Equal(t, ConnectionConfig{UseDefaultConnection: true, Version: "16.2"}, c)
	})
}

func TestIndexConfig_ValueScan(t *testing.T) {
	t.Run("round-trip serialization", func(t *testing.T) {
		original := IndexConfig{}
		raw, err := original.Value()
		require.NoError(t, err)

		var scanned IndexConfig
		require.NoError(t, scanned.Scan(raw.([]byte)))
		assert.Equal(t, original, scanned)
	})

	t.Run("empty config serializes to {}", func(t *testing.T) {
		raw, err := IndexConfig{}.Value()
		require.NoError(t, err)
		assert.JSONEq(t, `{}`, string(raw.([]byte)))
	})

	t.Run("Scan nil value returns no error", func(t *testing.T) {
		var c IndexConfig
		assert.NoError(t, c.Scan(nil))
	})

	t.Run("Scan non-byte value returns no error", func(t *testing.T) {
		var c IndexConfig
		assert.NoError(t, c.Scan(42))
	})
}

func TestNewVectorStoreResponse(t *testing.T) {
	store := &VectorStore{
		ID: "test-id", Name: "test-store", EngineType: PostgresRetrieverEngineType,
		ConnectionConfig: ConnectionConfig{UseDefaultConnection: true, Version: "16.2"},
	}

	t.Run("preserves PostgreSQL metadata", func(t *testing.T) {
		resp := NewVectorStoreResponse(store, StoreSourceEnv, true)
		assert.Equal(t, store.ID, resp.ID)
		assert.Equal(t, store.Name, resp.Name)
		assert.Equal(t, PostgresRetrieverEngineType, resp.EngineType)
		assert.True(t, resp.ConnectionConfig.UseDefaultConnection)
		assert.Equal(t, "16.2", resp.ConnectionConfig.Version)
	})

	t.Run("preserves source and readonly", func(t *testing.T) {
		resp := NewVectorStoreResponse(store, StoreSourceEnv, true)
		assert.Equal(t, StoreSourceEnv, resp.Source)
		assert.True(t, resp.ReadOnly)
	})

	t.Run("does not mutate original store", func(t *testing.T) {
		original := *store
		resp := NewVectorStoreResponse(store, StoreSourceEnv, true)
		resp.ConnectionConfig.Version = "changed"
		assert.Equal(t, original, *store)
	})

	t.Run("preserves empty metadata", func(t *testing.T) {
		resp := NewVectorStoreResponse(&VectorStore{ID: "test-id"}, StoreSourceEnv, true)
		assert.Equal(t, ConnectionConfig{}, resp.ConnectionConfig)
	})
}

func TestBuildEnvVectorStores(t *testing.T) {
	t.Run("single application PostgreSQL store", func(t *testing.T) {
		stores := BuildEnvVectorStores("postgres", nil)
		require.Len(t, stores, 1)
		assert.Equal(t, "__env_postgres__", stores[0].ID)
		assert.Equal(t, "PostgreSQL", stores[0].Name)
		assert.Equal(t, PostgresRetrieverEngineType, stores[0].EngineType)
		assert.True(t, stores[0].ConnectionConfig.UseDefaultConnection)
	})

	t.Run("default PostgreSQL requires no driver selection", func(t *testing.T) {
		stores := BuildEnvVectorStores("", nil)
		require.Len(t, stores, 1)
		assert.Equal(t, "__env_postgres__", stores[0].ID)
	})
}

func TestFindEnvVectorStore(t *testing.T) {
	t.Run("found", func(t *testing.T) {
		store := FindEnvVectorStore("postgres", nil, "__env_postgres__")
		require.NotNil(t, store)
		assert.Equal(t, "__env_postgres__", store.ID)
	})

	t.Run("not found", func(t *testing.T) {
		assert.Nil(t, FindEnvVectorStore("postgres", nil, "__env_unknown__"))
	})

	t.Run("default PostgreSQL requires no driver selection", func(t *testing.T) {
		store := FindEnvVectorStore("", nil, "__env_postgres__")
		require.NotNil(t, store)
		assert.Equal(t, "__env_postgres__", store.ID)
	})
}
