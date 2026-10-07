package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"reliquary/internal/storageallowlist"
	"reliquary/internal/types"
)

// fakeStorageBackendRepo is a minimal StorageBackendRepository used to exercise
// GetStorageEngineStatus' multi-instance awareness without a database.
type fakeStorageBackendRepo struct{ backends []*types.StorageBackend }

func (f *fakeStorageBackendRepo) Create(context.Context, *types.StorageBackend) error {
	return nil
}
func (f *fakeStorageBackendRepo) GetByID(context.Context, uint64, string) (*types.StorageBackend, error) {
	return nil, nil
}
func (f *fakeStorageBackendRepo) List(context.Context, uint64) ([]*types.StorageBackend, error) {
	return f.backends, nil
}
func (f *fakeStorageBackendRepo) Update(context.Context, *types.StorageBackend) error { return nil }
func (f *fakeStorageBackendRepo) Delete(context.Context, uint64, string) error        { return nil }
func (f *fakeStorageBackendRepo) FindLegacyAlias(context.Context, uint64, string) (*types.StorageBackend, error) {
	return nil, nil
}

// A workspace that configured S3 only through Storage settings
// (storage_backends), with an empty legacy StorageEngineConfig, must still
// be reported as available.
func TestGetStorageEngineStatus_AvailableFromActiveBackend(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Setenv(storageallowlist.AllowListEnv, "")

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/system/storage-engine-status", nil)
	c.Set(types.TenantInfoContextKey.String(), &types.Tenant{ID: 42})

	h := &SystemHandler{storageBackendRepo: &fakeStorageBackendRepo{backends: []*types.StorageBackend{
		{Provider: "minio", Status: types.StorageBackendStatusActive},
		{Provider: "s3", Status: types.StorageBackendStatusDisabled},
	}}}
	h.GetStorageEngineStatus(c)

	require.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Data struct {
			Engines []StorageEngineStatusItem `json:"engines"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))

	status := map[string]bool{}
	for _, engine := range resp.Data.Engines {
		status[engine.Name] = engine.Available
	}
	assert.True(t, status["minio"], "active MinIO backend should be available")
	assert.False(t, status["s3"], "disabled S3 backend should not be available")
}
