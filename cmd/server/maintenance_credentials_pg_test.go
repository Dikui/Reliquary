package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestOfflineMaintenanceCredentialsAreEncryptedAndNeverPrinted(t *testing.T) {
	db := pgtest.Open(t)
	// Naming is current, but an offline restore can lack maintenance state.
	// Refilling credentials must not create it or change migration history.
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migrations, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := migrations.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	require.NoError(t, db.Exec("DROP TABLE maintenance_state").Error)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,status,business,parser_engine_config,updated_at) VALUES
	 (1,'Offline owner','active','personal','{"mineru_endpoint":"https://parser.example.invalid","mineru_cloud_model":"pipeline","future_option":{"description":"retain every public field"}}','2026-10-01T12:00:00Z');
	 INSERT INTO models(id,space_id,name,type,source,status,parameters)
	 VALUES ('offline-model',1,'Offline fixture','KnowledgeQA','remote','needs_configuration',
	 '{"interface_type":"openai","extra_config":{"maintenance_required_fields":"[\"api_key\",\"app_secret\",\"custom_headers.Authorization\"]","maintenance_original_status":"active","maintenance_original_updated_at":"2026-10-10T00:00:00+00:00"}}');`).Error)
	dsn, err := url.Parse(db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	password, _ := dsn.User.Password()
	for key, value := range map[string]string{
		"DB_HOST": dsn.Hostname(), "DB_PORT": dsn.Port(), "DB_USER": dsn.User.Username(), "DB_PASSWORD": password,
		"DB_NAME": dsn.Path[1:], "SYSTEM_AES_KEY": "0123456789abcdef0123456789abcdef", "VAULT_ROOT": t.TempDir(), "AUTO_MIGRATE": "true",
	} {
		t.Setenv(key, value)
	}
	invoke := func(kind, id, value string) (map[string]any, error) {
		var output bytes.Buffer
		handled, err := runServerCommand([]string{"maintenance", "credentials", "--kind", kind, "--id", id}, strings.NewReader(value), &output)
		require.True(t, handled)
		require.NotContains(t, output.String(), "fixture-new-api-key")
		require.NotContains(t, output.String(), "fixture-new-app-secret")
		require.NotContains(t, output.String(), "fixture-new-auth-header")
		require.NotContains(t, output.String(), "fixture-new-parser-key")
		var result map[string]any
		if err == nil {
			require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		}
		return result, err
	}
	result, err := invoke("model", "offline-model", `{"api_key":"fixture-new-api-key"}`)
	require.NoError(t, err)
	require.Equal(t, "needs_configuration", result["status"])
	result, err = invoke("model", "offline-model", `{"app_secret":"fixture-new-app-secret","custom_headers":{"Authorization":"fixture-new-auth-header"}}`)
	require.NoError(t, err)
	require.Equal(t, "active", result["status"])
	var raw string
	require.NoError(t, db.Raw("SELECT parameters::text FROM models WHERE id='offline-model'").Scan(&raw).Error)
	require.NotContains(t, raw, "fixture-new-api-key")
	require.NotContains(t, raw, "fixture-new-app-secret")
	require.Contains(t, raw, "enc:v1:")
	var model types.Model
	require.NoError(t, db.Where("id='offline-model'").Take(&model).Error)
	require.Equal(t, "fixture-new-api-key", model.Parameters.APIKey)
	require.Equal(t, "fixture-new-app-secret", model.Parameters.AppSecret)
	require.Equal(t, "fixture-new-auth-header", model.Parameters.CustomHeaders["Authorization"])
	require.Equal(t, "active", model.Parameters.ExtraConfig["maintenance_original_status"])
	require.NotContains(t, model.Parameters.ExtraConfig, "maintenance_required_fields")
	_, err = invoke("data-source", "deleted-source", `{}`)
	require.ErrorContains(t, err, "model|space")
	var spaceBefore types.Space
	require.NoError(t, db.Where("id=1").Take(&spaceBefore).Error)
	result, err = invoke("space", "1", `{"parser_engine_config":{"mineru_api_key":"fixture-new-parser-key"}}`)
	require.NoError(t, err)
	require.Equal(t, "active", result["status"])
	var parserRaw string
	require.NoError(t, db.Raw("SELECT parser_engine_config::text FROM spaces WHERE id=1").Scan(&parserRaw).Error)
	var parser map[string]any
	require.NoError(t, json.Unmarshal([]byte(parserRaw), &parser))
	require.Equal(t, "https://parser.example.invalid", parser["mineru_endpoint"])
	require.Equal(t, "pipeline", parser["mineru_cloud_model"])
	require.Equal(t, map[string]any{"description": "retain every public field"}, parser["future_option"])
	var spaceAfter types.Space
	require.NoError(t, db.Where("id=1").Take(&spaceAfter).Error)
	require.Equal(t, "fixture-new-parser-key", spaceAfter.ParserEngineConfig.MinerUAPIKey)
	require.Equal(t, spaceBefore.UpdatedAt, spaceAfter.UpdatedAt)
	_, err = invoke("space", "1", `{"parser_engine_config":{"mineru_api_key":"fixture-new-parser-key","mineru_endpoint":"https://altered.example.invalid"}}`)
	require.Error(t, err)
	var unchangedParser string
	require.NoError(t, db.Raw("SELECT parser_engine_config::text FROM spaces WHERE id=1").Scan(&unchangedParser).Error)
	require.Equal(t, parserRaw, unchangedParser)
	_, err = invoke("model", "offline-model", `{"parser_engine_config":{"mineru_api_key":"fixture-new-parser-key"}}`)
	require.Error(t, err)
	before := raw
	_, err = invoke("model", "offline-model", `{"api_key":"fixture-new-api-key","extra_config":{"maintenance_original_status":"forged"}}`)
	require.Error(t, err)
	require.NoError(t, db.Raw("SELECT parameters::text FROM models WHERE id='offline-model'").Scan(&raw).Error)
	require.Equal(t, before, raw)
	_, err = invoke("model", "offline-model", `{"api_key":"fixture-new-api-key","unknown_secret":"fixture-new-app-secret"}`)
	require.Error(t, err)
	require.NotContains(t, err.Error(), "fixture-new-app-secret")
	var table string
	require.NoError(t, db.WithContext(context.Background()).Raw("SELECT COALESCE(to_regclass('maintenance_state')::text,'')").Scan(&table).Error)
	require.Empty(t, table)
	version, dirty, err := migrations.Version()
	require.NoError(t, err)
	require.EqualValues(t, 7, version)
	require.False(t, dirty)
}

func TestOfflineCredentialsRejectMissingEncryptionKeyAndReadableInputFile(t *testing.T) {
	db := pgtest.Open(t)
	t.Setenv("SYSTEM_AES_KEY", "")
	var output bytes.Buffer
	err := runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(`{"api_key":"fixture-secret"}`), &output)
	require.ErrorContains(t, err, "32-byte")
	require.Empty(t, output.String())
	t.Setenv("SYSTEM_AES_KEY", "0123456789abcdef0123456789abcdef")
	input := t.TempDir() + "/credentials.json"
	require.NoError(t, os.WriteFile(input, []byte(`{"api_key":"fixture-secret"}`), 0644))
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture", "--input", input}, bytes.NewReader(nil), &output)
	require.ErrorContains(t, err, "owner-only")
	require.NotContains(t, err.Error(), "fixture-secret")
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(`{"api_key":"fixture-secret"} {}`), &output)
	require.ErrorContains(t, err, "exactly one")
	err = runMaintenanceCredentials(context.Background(), db, []string{"--id", "fixture"}, strings.NewReader(strings.Repeat(" ", 1<<20)+"{}"), &output)
	require.ErrorContains(t, err, "1 MiB")
}
