package container

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestOfflineReembedPersistsAuditWithoutExportOnCompletionCancellationAndFailedItems(t *testing.T) {
	for _, outcome := range []string{"completed", "cancelled", "failed_items"} {
		t.Run(outcome, func(t *testing.T) {
			cancelAfterFirst := outcome == "cancelled"
			db := pgtest.Open(t)
			root, uploads := t.TempDir(), t.TempDir()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			require.NoError(t, service.EnsurePersonalSpace(ctx, db))
			library, err := repository.EnsureSystemLibraries(ctx, db, 1, "")
			require.NoError(t, err)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v1/embeddings", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				if outcome == "failed_items" {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"message":"offline fixture rejected embedding","type":"invalid_request_error"}}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"total_tokens":1}}`))
			}))
			defer server.Close()
			model := &types.Model{
				ID:      "offline-reembed",
				SpaceID: 1,
				Name:    "fixture",
				Type:    types.ModelTypeEmbedding,
				Source:  types.ModelSourceRemote,
				Status:  types.ModelStatusActive,

				Parameters: types.ModelParameters{
					InterfaceType:       "openai",
					Provider:            "generic",
					BaseURL:             server.URL + "/v1",
					EmbeddingParameters: types.EmbeddingParameters{Dimension: 3},
				},
			}
			require.NoError(t, db.Create(model).Error)
			library.EmbeddingModelID, library.ModelTiers.Embedding = model.ID, model.ID
			require.NoError(t, db.Save(library).Error)
			require.NoError(t,
				db.Model(&types.Space{}).Where("id=?",
					library.SpaceID).Update("embedding_model_id",
					model.ID).Error)
			for _, id := range []string{"first", "second"} {
				require.NoError(t, db.Create(&types.Entry{
					ID: id, SpaceID: 1, LibraryID: library.ID, Type: types.EntryTypeManual,
					Title: id, SourceKind: "mine", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled",
					VaultBody: "Original material " + id,
					StructuredSummary: &types.EntrySummary{
						Overview: "Original summary " + id,
					},
				}).Error)
			}
			configureOfflineReembed(t, db, root, uploads)
			initial,
				err := service.NewVaultService(db,
				&config.Config{Vault: &config.VaultConfig{
					Root:           root,
					HeaderDebounce: time.Hour,
				}})
			require.NoError(t, err)
			require.NoError(t, initial.Close())
			require.NoDirExists(t, filepath.Join(root, ".reliquary"))
			status, runErr := Reembed(ctx, "maintenance_start", "", func(run *types.EmbeddingRebuildRun) {
				if cancelAfterFirst && run.CompletedCount == 1 {
					cancel()
				}
			})
			require.NotNil(t, status)
			if cancelAfterFirst {
				require.ErrorIs(t, runErr, context.Canceled)
				require.Equal(t, "paused", status.LatestRun.Status)
				require.EqualValues(t, 1, status.LatestRun.CompletedCount)
			} else if outcome == "failed_items" {
				require.NoError(t, runErr)
				require.Equal(t, "completed_with_errors", status.LatestRun.Status)
				require.EqualValues(t, 2, status.LatestRun.FailedCount)
			} else {
				require.NoError(t, runErr)
				require.Equal(t, "completed", status.LatestRun.Status)
				require.EqualValues(t, 2, status.LatestRun.CompletedCount)
			}
			// The offline command persists checkpoints in PostgreSQL. File exports
			// remain explicit and must not be created by closing the vault.
			actual, err := maintenance.Capture(context.Background(), db)
			require.NoError(t, err)
			require.Len(t, actual.Tables["embedding_rebuild_runs"], 1)
			require.Len(t, actual.Tables["embedding_rebuild_items"], 2)
			require.NoDirExists(t, filepath.Join(root, ".reliquary"))
			verified, err := initial.VerifyRuntime(context.Background())
			require.NoError(t, err)
			require.True(t, verified.OK, verified.Differences)
		})
	}
}

func TestOfflineReembedFailureDoesNotExportAndReturnsTheOriginalError(t *testing.T) {
	db := pgtest.Open(t)
	root, uploads := t.TempDir(), t.TempDir()
	_, err := repository.EnsureSystemLibraries(context.Background(), db, 1, "")
	require.NoError(t, err)
	configureOfflineReembed(t, db, root, uploads)
	status, err := Reembed(context.Background(), "invalid-offline-action", "", nil)
	require.Nil(t, status)
	require.ErrorContains(t, err, "unsupported reembed action")
	require.False(t, errors.Is(err, context.Canceled))
	saved, err := maintenance.Capture(context.Background(), db)
	require.NoError(t, err)
	require.NotEmpty(t, saved.Tables["libraries"])
	require.NoDirExists(t, filepath.Join(root, ".reliquary"))
}

func configureOfflineReembed(t *testing.T, db *gorm.DB, root, uploads string) {
	t.Helper()
	dsn, err := url.Parse(db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	password, _ := dsn.User.Password()
	// Register this before t.Setenv so its environment cleanup runs first.
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for name, value := range map[string]string{
		"DB_DRIVER":   "postgres",
		"DB_HOST":     dsn.Hostname(),
		"DB_PORT":     dsn.Port(),
		"DB_USER":     dsn.User.Username(),
		"DB_PASSWORD": password,

		"DB_NAME":                dsn.Path[1:],
		"AUTO_MIGRATE":           "false",
		"OLLAMA_OPTIONAL":        "true",
		"VAULT_ROOT":             root,
		"LOCAL_STORAGE_BASE_DIR": uploads,
		"VAULT_HEADER_DEBOUNCE":  "1h",
		"SSRF_WHITELIST":         "127.0.0.1",
		"SSRF_WHITELIST_EXTRA":   "",
	} {
		t.Setenv(name, value)
	}
	utils.ResetSSRFWhitelistForTest()
	data, err := os.ReadFile(filepath.Join("..", "..", "config", "config.yaml"))
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, data, 0o600))
	require.NoError(t, os.CopyFS(filepath.Join(filepath.Dir(path), "prompt_templates"), os.DirFS("../../config/prompt_templates")))
	viper.Reset()
	viper.SetConfigFile(path)
	viper.AddConfigPath(filepath.Dir(path))
	t.Cleanup(viper.Reset)
}
