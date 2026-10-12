package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

// Catalog assertions include extension index definitions and function bodies.
const obsoleteSchemaObjects = `
SELECT 'column:'||table_schema||'.'||table_name||'.'||column_name AS name
FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') AND table_schema NOT LIKE 'pg_toast%' AND (table_name ILIKE '%tenant%' OR column_name ILIKE '%tenant%')
UNION ALL SELECT 'relation:'||c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.relname ILIKE '%tenant%'
UNION ALL SELECT 'constraint:'||c.conname FROM pg_constraint c JOIN pg_namespace n ON n.oid=c.connamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND c.conname ILIKE '%tenant%'
UNION ALL SELECT 'function:'||p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND (p.proname ILIKE '%tenant%' OR p.prosrc ILIKE '%tenant%')
UNION ALL SELECT 'trigger:'||t.tgname FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND t.tgname ILIKE '%tenant%'
UNION ALL SELECT 'index:'||indexname FROM pg_indexes WHERE schemaname NOT IN ('pg_catalog','information_schema') AND schemaname NOT LIKE 'pg_toast%' AND indexdef ILIKE '%tenant%'
UNION ALL SELECT 'comment:'||d.description FROM pg_description d JOIN pg_class c ON d.classoid='pg_class'::regclass AND d.objoid=c.oid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%' AND d.description ILIKE '%tenant%'
UNION ALL SELECT 'view:'||viewname FROM pg_views WHERE schemaname NOT IN ('pg_catalog','information_schema') AND schemaname NOT LIKE 'pg_toast%' AND (viewname ILIKE '%tenant%' OR definition ILIKE '%tenant%')
UNION ALL SELECT 'materialized view:'||matviewname FROM pg_matviews WHERE schemaname NOT IN ('pg_catalog','information_schema') AND schemaname NOT LIKE 'pg_toast%' AND (matviewname ILIKE '%tenant%' OR definition ILIKE '%tenant%')
UNION ALL SELECT 'namespace:'||nspname FROM pg_namespace WHERE nspname ILIKE '%tenant%'
UNION ALL SELECT 'type:'||t.typname FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND t.typname ILIKE '%tenant%'`

func TestSpaceNamingMigrationPreservesDataAndConstraints(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(1))
	require.NoError(t, db.Exec(`
INSERT INTO tenants(id,name,business) VALUES(1,'Naming fixture','personal');
INSERT INTO tenant_api_keys(tenant_id,name,key_hash,api_key) VALUES(1,'Original key','fixture-hash','fixture-secret');
INSERT INTO users(id,username,email,password_hash) VALUES('owner','Owner','naming@example.invalid','original-hash');
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES('kb','Original library',1,'','');
INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,parse_status,metadata) VALUES('item',1,'kb','manual','Original observations','','completed','{"tenant_id":"opaque metadata"}');
INSERT INTO entities(id,tenant_id,canonical_name,normalized_name,entity_type,created_at,updated_at) VALUES('entity',1,'Original entity','original entity','company',now(),now());
INSERT INTO topics(id,tenant_id,name,description) VALUES('topic',1,'Original topic','Keep description');
INSERT INTO knowledge_entities(tenant_id,knowledge_id,entity_id) VALUES(1,'item','entity');
INSERT INTO knowledge_topics(tenant_id,knowledge_id,topic_id,source) VALUES(1,'item','topic','user');
INSERT INTO follows(tenant_id,target_type,target_id) VALUES(1,'entity','entity');
INSERT INTO task_jobs(id,task_type,queue,payload,state) VALUES('old-task','manual:process','default','{"tenant_id":1,"knowledge_id":"item"}','pending');
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(2))
	after := baselineContents(t, db)
	for table, raw := range before {
		var rows []map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rows))
		for _, row := range rows {
			if value, ok := row["tenant_id"]; ok {
				row["space_id"] = value
				delete(row, "tenant_id")
			}
		}
		name := table
		if name == "tenants" {
			name = "spaces"
		}
		if name == "tenant_api_keys" {
			name = "api_keys"
		}
		var actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(after[name]), &actual))
		require.ElementsMatch(t, rows, actual, "all columns of %s", name)
	}
	var leftovers []string
	require.NoError(t, db.Raw(obsoleteSchemaObjects).Scan(&leftovers).Error)
	require.Empty(t, leftovers)
	var fields []string
	require.NoError(t, db.Raw(`SELECT name FROM paradedb.schema('public.knowledges_summary_search_idx'::regclass)`).Scan(&fields).Error)
	require.Contains(t, fields, "space_id")
	require.NotContains(t, fields, "tenant_id")
	// FK, uniqueness, singleton checks and trigger bodies remain effective.
	for _, statement := range []string{
		`INSERT INTO spaces(id,name,business) VALUES(2,'Forbidden','personal')`,
		`INSERT INTO api_keys(space_id,name,key_hash) VALUES(1,'Duplicate','fixture-hash')`,
		`INSERT INTO users(id,username,email,password_hash,space_id) VALUES('other','Other','other@example.invalid','x',2)`,
		`INSERT INTO knowledge_entities(space_id,knowledge_id,entity_id) VALUES(1,'missing','entity')`,
		`INSERT INTO topics(id,space_id,name) VALUES('duplicate',1,'Original topic')`,
	} {
		require.Error(t, db.Exec(statement).Error, statement)
	}
	require.NoError(t, db.Exec(`UPDATE entities SET canonical_name='Changed entity' WHERE id='entity'; UPDATE topics SET name='Changed topic' WHERE id='topic'`).Error)
	var count int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM knowledges WHERE space_id=1 AND search_seq_id @@@ 'search_text:Changed'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, m.Migrate(1))
	// Undo the explicit post-up test edits before exact old-data comparison.
	require.NoError(t, db.Exec(`UPDATE entities SET canonical_name='Original entity' WHERE id='entity'; UPDATE topics SET name='Original topic' WHERE id='topic'`).Error)
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(2))
	require.NoError(t, db.Raw(obsoleteSchemaObjects).Scan(&leftovers).Error)
	require.Empty(t, leftovers)
}
