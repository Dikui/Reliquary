package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reliquary/internal/application/service"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestParseReembedArguments(t *testing.T) {
	for _, test := range []struct {
		args       []string
		action, id string
	}{
		{nil, "start", ""},
		{[]string{"--preserve-entity-timestamps"}, "maintenance_start", ""},
		{[]string{"--resume", "run-id"}, "resume", "run-id"},
		{[]string{"--retry-failed", "run-id"}, "retry_failed", "run-id"},
	} {
		action, id, err := parseReembedArgs(test.args)
		require.NoError(t, err)
		require.Equal(t, test.action, action)
		require.Equal(t, test.id, id)
	}
	for _, args := range [][]string{{"--resume"}, {"--retry-failed", ""}, {"--unknown", "id"}, {"--resume", "id", "extra"}, {"--preserve-entity-timestamps", "--resume", "id"}} {
		_, _, err := parseReembedArgs(args)
		require.Error(t, err)
	}
}

func TestReembedHistoricalFailedRunReportsItsOwnStatusAndExitError(t *testing.T) {
	db := pgtest.Open(t)
	providerFails := true
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1/embeddings", r.URL.Path)
		if providerFails {
			http.Error(w, "temporary provider failure", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[1,0,0],"index":0}]}`)
	}))
	defer provider.Close()
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "CLI rebuild", EmbeddingModelID: "cli-model"}).Error)
	require.NoError(
		t,
		db.Create(
			&types.Library{
				ID:         "cli-library",
				SpaceID:    1,
				Name:       "CLI",
				ModelTiers: types.ModelTierConfig{Embedding: "cli-model"},
			},
		).Error,
	)
	require.NoError(
		t,
		db.Create(
			&types.Model{
				ID:      "cli-model",
				SpaceID: 1,
				Name:    "Fixture model",
				Type:    types.ModelTypeEmbedding,
				Source:  types.ModelSourceRemote,
				Status:  types.ModelStatusActive,
				Parameters: types.ModelParameters{
					BaseURL:             provider.URL + "/v1",
					InterfaceType:       "openai",
					EmbeddingParameters: types.EmbeddingParameters{Dimension: 3},
				},
			},
		).Error,
	)
	require.NoError(t, db.Create(&types.Entry{
		ID: "cli-entry", SpaceID: 1, LibraryID: "cli-library", Type: types.EntryTypeManual,
		Title: "CLI material", SourceKind: "mine", ParseStatus: types.ParseStatusCompleted,
		StructuredSummary: &types.EntrySummary{Overview: "CLI summary", Points: []string{}, KeyTerms: []string{}},
		VaultBody:         "Original CLI material",
	}).Error)

	var databaseName string
	require.NoError(t, db.Raw("SELECT current_database()").Scan(&databaseName).Error)
	dsn, err := url.Parse(os.Getenv(pgtest.DSNEnv))
	require.NoError(t, err)
	password, _ := dsn.User.Password()
	for name, value := range map[string]string{
		"DB_DRIVER":   "postgres",
		"DB_HOST":     dsn.Hostname(),
		"DB_PORT":     dsn.Port(),
		"DB_USER":     dsn.User.Username(),
		"DB_PASSWORD": password,

		"DB_NAME": databaseName, "AUTO_MIGRATE": "false", "VAULT_ROOT": t.TempDir(), "SSRF_WHITELIST": "127.0.0.1",
	} {
		t.Setenv(name, value)
	}
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	configPath, err := filepath.Abs("../../config")
	require.NoError(t, err)
	viper.AddConfigPath(configPath)
	invoke := func(args []string) (*types.EmbeddingRebuildStatus, error) {
		var output bytes.Buffer
		handled, commandErr := runServerCommand(args, bytes.NewReader(nil), &output)
		require.True(t, handled)
		var status *types.EmbeddingRebuildStatus
		decoder := json.NewDecoder(&output)
		for {
			var record struct {
				Event string          `json:"event"`
				Data  json.RawMessage `json:"data"`
			}
			decodeErr := decoder.Decode(&record)
			if decodeErr == io.EOF {
				break
			}
			require.NoError(t, decodeErr)
			if record.Event == "status" {
				require.NoError(t, json.Unmarshal(record.Data, &status))
			}
		}
		require.NotNil(t, status)
		return status, commandErr
	}
	original, err := invoke([]string{"reembed"})
	require.Error(t, err)
	require.Equal(t, "completed_with_errors", original.LatestRun.Status)
	providerFails = false
	newer, err := invoke([]string{"reembed"})
	require.NoError(t, err)
	require.Equal(t, "completed", newer.LatestRun.Status)
	require.NotEqual(t, original.LatestRun.ID, newer.LatestRun.ID)
	providerFails = true
	retried, err := invoke([]string{"reembed", "--retry-failed", original.LatestRun.ID})
	require.ErrorContains(t, err, "--retry-failed "+original.LatestRun.ID)
	require.Equal(t, original.LatestRun.ID, retried.LatestRun.ID)
	require.EqualValues(t, 1, retried.LatestRun.FailedCount)
	require.Equal(t, "completed_with_errors", retried.LatestRun.Status)
	require.Len(t, retried.FailedItems, 1)
	// Settings still shows the latest created run, independently of CLI scope.
	latest, err := service.NewEmbeddingRebuilder(db, nil, nil, nil).Status(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, newer.LatestRun.ID, latest.LatestRun.ID)
	providerFails = false
	completed, err := invoke([]string{"reembed", "--retry-failed", original.LatestRun.ID})
	require.NoError(t, err)
	require.Equal(t, original.LatestRun.ID, completed.LatestRun.ID)
	require.Equal(t, "completed", completed.LatestRun.Status)
	require.EqualValues(t, 1, completed.LatestRun.CompletedCount)
}
