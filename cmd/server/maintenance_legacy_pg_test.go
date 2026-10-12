package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestMaintenanceHelperSnapshotsLegacySchemaWithoutUpgradingIt(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migrations, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := migrations.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	require.NoError(t, migrations.Migrate(0))
	require.NoError(t, db.Exec(`
INSERT INTO tenants(id,name,business) VALUES(1,'Legacy maintenance','personal');
INSERT INTO users(id,username,email,password_hash) VALUES('legacy-user','Legacy owner','legacy@example.invalid','fixture-password-hash');
INSERT INTO models(id,tenant_id,name,type,source,status,parameters) VALUES('legacy-model',1,'Legacy model','knowledge-qa','remote','active','{"api_key":"fixture-legacy-key","interface_type":"openai"}');
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES('legacy-kb','Articles',1,'legacy-model','legacy-model');
INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,parse_status,content_sim_hash,metadata)
VALUES('legacy-article',1,'legacy-kb','manual','Original awaiting review','','pending_review',9007199254740993,'{"intake":{"original_content":"Exact legacy original.","reason":"missing source"}}');
INSERT INTO task_jobs(id,task_type,queue,state,payload,claim_token,lease_until) VALUES('legacy-active','manual:process','default','active','{"knowledge_id":"legacy-article"}','legacy-worker',now()+interval '1 hour');
`).Error)
	contents := func() map[string]string {
		var tables []string
		require.NoError(t, db.Raw("SELECT tablename FROM pg_tables WHERE schemaname='public' ORDER BY tablename").Scan(&tables).Error)
		result := map[string]string{}
		for _, table := range tables {
			var rows string
			require.NoError(t, db.Raw(fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM public.%q t`, table)).Scan(&rows).Error)
			result[table] = rows
		}
		return result
	}
	before := contents()
	dsn, err := url.Parse(db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	password, _ := dsn.User.Password()
	for key, value := range map[string]string{
		"DB_DRIVER": "postgres", "DB_HOST": dsn.Hostname(), "DB_PORT": dsn.Port(), "DB_USER": dsn.User.Username(), "DB_PASSWORD": password,
		"DB_NAME": dsn.Path[1:], "VAULT_ROOT": t.TempDir(), "LOCAL_STORAGE_BASE_DIR": t.TempDir(), "AUTO_MIGRATE": "true",
	} {
		t.Setenv(key, value)
	}
	invoke := func(name string, arguments ...string) map[string]any {
		var output bytes.Buffer
		handled, err := runServerCommand(append([]string{"maintenance", name}, arguments...), bytes.NewReader(nil), &output)
		require.True(t, handled)
		require.NoError(t, err, output.String())
		var result map[string]any
		require.NoError(t, json.Unmarshal(output.Bytes(), &result))
		return result
	}
	require.Equal(t, true, invoke("flush")["flushed"])
	manifest := invoke("manifest")
	require.NotContains(t, manifest["tables"], "maintenance_state")
	require.Equal(t, true, invoke("verify")["ok"])
	manifestFile := filepath.Join(t.TempDir(), "manifest.json")
	// Write the raw manifest from the command to preserve its exact 64-bit values.
	var raw bytes.Buffer
	_, err = runServerCommand([]string{"maintenance", "manifest"}, bytes.NewReader(nil), &raw)
	require.NoError(t, err)
	encoded := raw.Bytes()
	require.NoError(t, os.WriteFile(manifestFile, encoded, 0600))
	require.Equal(t, true, invoke("verify", "--manifest", manifestFile)["ok"])
	require.Equal(t, before, contents(), "legacy pre-upgrade maintenance must not change schema or accepted data")
	version, dirty, err := migrations.Version()
	require.NoError(t, err)
	require.EqualValues(t, 0, version)
	require.False(t, dirty)
	// Credential scrubbing runs against a disposable clone in production. This
	// isolated legacy database proves that the command itself needs no migration.
	scrub := invoke("scrub")
	require.NotEmpty(t, scrub["required"])
	version, dirty, err = migrations.Version()
	require.NoError(t, err)
	require.EqualValues(t, 0, version)
	require.False(t, dirty)
	var stateTable string
	require.NoError(t, db.Raw("SELECT COALESCE(to_regclass('public.maintenance_state')::text,'')").Scan(&stateTable).Error)
	require.Empty(t, stateTable)
	files, err := os.ReadDir(filepath.Join(os.Getenv("VAULT_ROOT"), ".reliquary"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
}
