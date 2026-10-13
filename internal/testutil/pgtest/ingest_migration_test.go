package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestIngestOnlyMigrationPreservesPreviousVersionEveryTable(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(3))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Upgrade fixture','personal');
INSERT INTO users(id,username,email,password_hash,space_id) VALUES('owner','Owner','ingest-upgrade@example.test','retained-hash',1);
INSERT INTO knowledge_bases(id,name,space_id,embedding_model_id,summary_model_id) VALUES('library','Library',1,'','');
INSERT INTO entries(id,space_id,knowledge_base_id,type,title,source,parse_status,vault_body,collect_reason,metadata,custom_metadata) VALUES
 ('article',1,'library','manual','Existing article','https://example.test/source','completed','Original immutable body','User reason','{"intake":{"decision":"approved","source":"ingest"}}','{"owner":"retained"}'),
 ('review',1,'library','manual','Waiting review','https://example.test/review','rejected','Waiting review source','','{"intake":{"decision":"rejected","reason":"retain reason"}}','{}');
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,description,created_at,updated_at) VALUES('entity',1,'Original entity','original entity','company','Original description',now(),now());
INSERT INTO topics(id,space_id,name,description) VALUES('topic',1,'Original topic','Original description');
INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'article','entity');
INSERT INTO entry_topics(space_id,entry_id,topic_id,source) VALUES(1,'article','topic','user');
INSERT INTO entry_repost_sources(id,space_id,knowledge_base_id,entry_id,original_url,normalized_url,source_fingerprint,match_method,created_at) VALUES('repost',1,'library','article','https://example.test/repost','https://example.test/repost','fingerprint','url',now());
INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('log',1,'api_key:9','ingest','article','null'::jsonb,'{"reason":"keep"}');
INSERT INTO api_keys(id,space_id,name,key_hash,api_key,revoked_at) VALUES(9,1,'Original revoked collector','old-key-hash','old-encrypted-key','2026-10-01T12:00:00Z');
INSERT INTO task_jobs(id,task_type,queue,payload,state,retried,fail_count) VALUES('job','manual:process','default','{"knowledge_id":"review"}','pending',2,1);
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(4))
	after := baselineContents(t, db)
	added := map[string][]string{
		"entries":        {"acquisition", "incomplete", "external_completion_attempted", "ingest_api_key_id", "ingest_api_key_name", "last_ingest_api_key_id", "last_ingest_api_key_name", "missing_materials", "ingest_attachments", "content_type_hint", "related_article_id"},
		"operation_logs": {"api_key_id", "api_key_name"},
	}
	for table, want := range before {
		var expected, actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(want), &expected))
		require.NoError(t, json.Unmarshal([]byte(after[table]), &actual))
		require.Len(t, actual, len(expected), table)
		for _, row := range actual {
			for _, column := range added[table] {
				delete(row, column)
			}
		}
		require.ElementsMatch(t, expected, actual, table)
	}
	require.Equal(t, "[]", after["ingest_assets"])
	require.Equal(t, "[]", after["ingest_discards"])
	t.Logf("version 3 to 4 preserved all old columns and row counts across %d tables", len(before))
	require.NoError(t, m.Migrate(3))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(4))
}
