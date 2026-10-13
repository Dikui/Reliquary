package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
)

func collectionMigration(t *testing.T, db *gorm.DB) *migrate.Migrate {
	t.Helper()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	return migration
}
func collectionVersionFour(t *testing.T) *gorm.DB {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, collectionMigration(t, db).Migrate(4))
	return db
}
func seedCollection(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.Exec(`INSERT INTO data_sources(id,space_id,knowledge_base_id,name,type,default_content_type,config,sync_schedule,sync_mode,status,sync_deletions,last_sync_at,last_sync_cursor,last_sync_result,error_message,sync_log_retention_days,created_at,updated_at) VALUES
 ('rss',1,'library','Original subscription','rss','research','{"settings":{"feed_urls":["https://example.test/feed"],"title":"Original settings"},"credentials":{"token":"legacy-source-private-token"},"headers":{"Authorization":"Bearer legacy-source-header","Accept":"application/rss+xml"}}','*/30 * * * *','incremental','active',false,'2026-10-01T12:00:00Z','{"cursor":"original-cursor"}','{"created":2}','original-source-error',14,'2026-09-01T00:00:00Z','2026-10-01T12:00:00Z');
 INSERT INTO sync_logs(id,data_source_id,space_id,status,started_at,finished_at,items_total,items_created,items_updated,items_deleted,items_skipped,items_failed,error_message,result,created_at,updated_at) VALUES
 ('sync-1','rss',1,'success','2026-10-01T12:00:00Z','2026-10-01T12:01:00Z',8,2,1,1,3,1,'original-sync-error','{"url":"https://example.test/article","details":["retain details"]}','2026-10-01T12:00:00Z','2026-10-01T12:01:00Z'),
 ('sync-2','rss',1,'failed','2026-10-02T12:00:00Z',NULL,0,0,0,0,0,0,'retained failure','null'::jsonb,'2026-10-02T12:00:00Z','2026-10-02T12:00:00Z');
 UPDATE entries SET channel='rss',source='https://example.test/article',metadata=jsonb_set(metadata,'{datasource_id}','"rss"'::jsonb),file_name='retained.pdf',file_path='local://retained.pdf' WHERE id='article';`).Error)
}
func collectionTableContents(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	var tables []string
	require.NoError(t, db.Raw("SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename!='schema_migrations' ORDER BY tablename").Scan(&tables).Error)
	result := map[string]string{}
	for _, table := range tables {
		var rows string
		require.NoError(t, db.Raw(fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM public.%q t`, table)).Scan(&rows).Error)
		result[table] = rows
	}
	return result
}
func TestCollectionRemovalExportsAndPreservesEveryRemainingTable(t *testing.T) {
	ctx := context.Background()
	db := collectionVersionFour(t)
	seed(t, db)
	seedCollection(t, db)
	before := collectionTableContents(t, db)
	snapshot, err := captureHistoricalSchema(ctx, db)
	require.NoError(t, err)
	require.Len(t, snapshot.Tables["data_sources"], 1)
	require.Len(t, snapshot.Tables["sync_logs"], 2)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	expected, err := manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	result, err := exportHistoricalSources(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, 1, result.DataSources)
	require.Equal(t, 2, result.SyncLogs)
	require.Contains(t, result.Path, ".reliquary/exports/sources-000005-")
	payload, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "legacy-source-private-token")
	require.NotContains(t, string(payload), "legacy-source-header")
	var export SourcesExport
	require.NoError(t, json.Unmarshal(payload, &export))
	require.Equal(t, 5, export.MigrationVersion)
	require.EqualValues(t, 4, *export.SourceMigrationVersion)
	require.NotEmpty(t, export.ExportedAt)
	require.Len(t, export.Tables["data_sources"][0], 19)
	source := export.Tables["data_sources"][0]
	require.Equal(t, "Original subscription", source["name"])
	require.Equal(t, "*/30 * * * *", source["sync_schedule"])
	require.Equal(t, "research", source["default_content_type"])
	require.Equal(t, "active", source["status"])
	require.Equal(t, snapshot.Tables["sync_logs"], namedRowsJSONNumbers(t, export.Tables["sync_logs"]))
	require.Equal(t, snapshot.JSONNulls["sync_logs"], export.JSONNulls["sync_logs"])
	again, err := exportHistoricalSources(ctx, db, root)
	require.NoError(t, err)
	require.Equal(t, result.Path, again.Path)
	migration := collectionMigration(t, db)
	require.NoError(t, migration.Migrate(5))
	delete(before, "data_sources")
	delete(before, "sync_logs")
	require.Equal(t, before, collectionTableContents(t, db), "migration must retain every other column and row, including RSS source metadata")
	current, err := captureHistoricalSchema(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, current.Tables, "data_sources")
	require.NotContains(t, current.Tables, "sync_logs")
	require.NoError(t, WriteSnapshot(root, "", current))
	actual, err := manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))
	require.NoFileExists(t, filepath.Join(root, ".reliquary/config/data_sources.json"))
	require.NoFileExists(t, filepath.Join(root, ".reliquary/data/sync_logs.json"))
	require.NoError(t, migration.Migrate(4))
	require.NoError(t, classifyHistoricalSchema(ctx, db))
	var count int64
	require.NoError(t, db.Table("data_sources").Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, migration.Migrate(5))
	require.NoError(t, classifyHistoricalSchema(ctx, db))
	t.Logf("version 4 to 5 retained every field and row across %d remaining public tables; two removed tables archived", len(before))
}
func namedRowsJSONNumbers(t *testing.T, rows []map[string]any) []map[string]any {
	t.Helper()
	payload, err := json.Marshal(rows)
	require.NoError(t, err)
	var converted []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&converted))
	return converted
}
func TestOldReplicaRebuildExportsRetiredTablesAndVerifiesRetainedData(t *testing.T) {
	ctx := context.Background()
	source := collectionVersionFour(t)
	seed(t, source)
	seedCollection(t, source)
	snapshot, err := captureHistoricalSchema(ctx, source)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	target := pgtest.Open(t)
	restored, err := Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	require.NotContains(t, restored.Tables, "data_sources")
	require.NotContains(t, restored.Tables, "sync_logs")
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	exports, err := collectionExports(root)
	require.NoError(t, err)
	require.Len(t, exports, 1)
	require.Len(t, exports[0].Export.Tables["sync_logs"], 2)
	require.NoError(t, target.Exec(`UPDATE entries SET metadata='{}'::jsonb WHERE id='article'`).Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, target, root), "table entries")
}
func TestCollectionRemovalVerificationRejectsMissingOrAlteredExports(t *testing.T) {
	ctx := context.Background()
	db := collectionVersionFour(t)
	seed(t, db)
	seedCollection(t, db)
	root := t.TempDir()
	snapshot, err := captureHistoricalSchema(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	expected, err := manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	result, err := exportHistoricalSources(ctx, db, root)
	require.NoError(t, err)
	require.NoError(t, collectionMigration(t, db).Migrate(5))
	snapshot, err = captureHistoricalSchema(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	payload, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	var export SourcesExport
	require.NoError(t, json.Unmarshal(payload, &export))
	export.Tables["data_sources"][0]["name"] = "tampered"
	changed, err := json.Marshal(export)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(result.Path, changed, 0600))
	actual, err := manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "missing table data_sources")
	require.NoError(t, os.Remove(result.Path))
	actual, err = manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "missing table sync_logs")
}

func TestFrozenBaselineRestoreExportsAndUpgradesWithStrictDefaults(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	migration := collectionMigration(t, db)
	require.NoError(t, migration.Migrate(0))
	require.NoError(t, db.Exec(`INSERT INTO tenants(id,name,business) VALUES(1,'Baseline owner','personal');
 INSERT INTO users(id,username,email,password_hash) VALUES('owner','Owner','baseline@example.test','preserved-hash');
 INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES('library','Library',1,'','');
 INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config,status) VALUES('rss',1,'library','Baseline RSS','rss','{"url":"https://example.test/feed","credentials":{"token":"baseline-private-token"}}','active');
 INSERT INTO sync_logs(id,data_source_id,tenant_id,status,result) VALUES('sync','rss',1,'success','{"created":1}');
 INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,channel,parse_status,vault_body,metadata) VALUES('article',1,'library','manual','Original baseline article','https://example.test/article','rss','completed','Original baseline body','{"datasource_id":"rss","intake":{"decision":"approved"}}');
 INSERT INTO operation_logs(id,tenant_id,actor,operation_type,object_id,before_value,after_value) VALUES('original-log',1,'owner','ingest','article','null'::jsonb,'{"retained":true}');`).Error)
	root := t.TempDir()
	var expected *Manifest
	require.NoError(t, WithCurrentNames(db, func(tx *gorm.DB) error {
		snapshot, err := captureHistoricalSchema(ctx, tx)
		if err != nil {
			return err
		}
		if err := WriteSnapshot(root, "", snapshot); err != nil {
			return err
		}
		expected, err = manifestHistoricalSchema(ctx, tx, root, "")
		if err != nil {
			return err
		}
		result, err := exportHistoricalSources(ctx, tx, root)
		require.NoError(t, err)
		payload, err := os.ReadFile(result.Path)
		require.NoError(t, err)
		require.Contains(t, string(payload), `"source_migration_version": 0`)
		return err
	}))
	require.NoError(t, migration.Migrate(5))
	current, err := captureHistoricalSchema(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	require.NoError(t, classifyHistoricalSchema(ctx, db))
	actual, err := manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))
	require.NoError(t, db.Exec(`UPDATE entries SET acquisition='model_written' WHERE id='article'`).Error)
	actual, err = manifestHistoricalSchema(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table entries", "only exact migration defaults may be added to a baseline backup")
}
