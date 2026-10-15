package router

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

func TestVaultRebuildUsesSameAuthenticationAsStatus(t *testing.T) {
	t.Setenv("LOCAL_STORAGE_BASE_DIR", t.TempDir())
	f := newPersonalAuthFixture(t)
	_, err := repository.EnsureSystemLibraries(context.Background(), f.db, types.SingleSpaceID, "")
	require.NoError(t, err)
	root := t.TempDir()
	documents, err := service.NewVaultDocumentService(f.db, nil, nil, root)
	require.NoError(t, err)
	vault, err := service.NewVaultService(f.db, &config.Config{Vault: &config.VaultConfig{Root: root}})
	require.NoError(t, err)
	h := handler.NewVaultHandler(documents)
	h.SetArticles(vault)
	f.params.VaultHandler = h
	// Use the production router so moving an endpoint outside auth is caught.
	router := NewRouter(f.params)
	for _, credential := range []struct {
		name, value string
		key         bool
		allowed     bool
	}{
		{"invalid token", "invalid", false, false}, {"invalid key", "invalid", true, false},
		{"local user", "", false, true}, {"API key", f.key, true, true},
	} {
		t.Run(credential.name, func(t *testing.T) {
			for _, route := range []struct {
				method, path string
				code         int
			}{
				{http.MethodGet, "/api/v1/vault/status", http.StatusOK},
				{http.MethodGet, "/api/v1/vault/rebuild", http.StatusOK},
				{http.MethodPost, "/api/v1/vault/rebuild", http.StatusAccepted},
			} {
				response := personalRequest(t, router, route.method, route.path, "", credential.value, credential.key)
				if !credential.allowed {
					require.Equal(t, http.StatusUnauthorized, response.Code, response.Body.String())
					continue
				}
				require.Equal(t, route.code, response.Code, response.Body.String())
				if route.method == http.MethodPost {
					require.Eventually(t, func() bool { return vault.FileRebuildStatus().Status != "running" }, 10*time.Second, 10*time.Millisecond)
					require.Equal(t, "success", vault.FileRebuildStatus().Status, vault.FileRebuildStatus().Error)
				}
			}
		})
	}
}
