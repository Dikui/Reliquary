package pgtest

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// These are task 14's committed baseline files. Changing either file after real
// data is admitted must fail; every schema change gets an incremental migration.
func TestBaselineIsFrozenAtTask14(t *testing.T) {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "versioned")
	for file, expected := range map[string]string{
		"000000_init.up.sql":   "07d734e09334c72360e8f1254fa913f6ff66975d32af23a71a9e27c2eff91e57",
		"000000_init.down.sql": "a0ad0ce42b3acca381503f156df454a20b08fd56572e8af64477518d83bca552",
	} {
		data, err := os.ReadFile(filepath.Join(root, file))
		require.NoError(t, err)
		require.Equal(t, expected, fmt.Sprintf("%x", sha256.Sum256(data)), "frozen baseline changed; add an incremental migration")
	}
}

func baselineContents(t *testing.T, db *gorm.DB) map[string]string {
	t.Helper()
	var tables []string
	require.NoError(t, db.Raw("SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename NOT IN ('schema_migrations','maintenance_state') ORDER BY tablename").Scan(&tables).Error)
	result := make(map[string]string, len(tables))
	for _, table := range tables {
		var content string
		require.NoError(t, db.Raw(fmt.Sprintf(`SELECT COALESCE(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text),'[]'::jsonb)::text FROM public.%q t`, table)).Scan(&content).Error)
		result[table] = content
	}
	return result
}

func TestMaintenanceMigrationPreservesFrozenBaselineData(t *testing.T) {
	db := Open(t)
	_, source, _, _ := runtime.Caller(0)
	directory := filepath.Join(filepath.Dir(source), "..", "..", "..", "migrations", "versioned")
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() {
		sourceErr, databaseErr := m.Close()
		require.NoError(t, sourceErr)
		require.NoError(t, databaseErr)
	})
	// The database now has the exact previous commit's schema, version 0.
	require.NoError(t, m.Migrate(0))
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 0, version)
	require.False(t, dirty)
	require.NoError(t, db.Exec(`
INSERT INTO tenants(id,name,business) VALUES(1,'upgrade fixture','personal');
INSERT INTO users(id,username,email,password_hash) VALUES('upgrade-user','User','user@example.invalid','fixture-password-hash');
INSERT INTO models(id,tenant_id,name,type,source,parameters) VALUES('upgrade-model',1,'fixture','embedding','ollama','{"base_url":"http://localhost:11434","dimension":3}');
INSERT INTO knowledge_bases(id,name,tenant_id,embedding_model_id,summary_model_id) VALUES('upgrade-library','Articles',1,'upgrade-model','upgrade-model');
INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,parse_status,content_type,source_unverified,content_type_corrected,vault_body,processing_review_status,processing_review_reason,custom_metadata)
VALUES('upgrade-article',1,'upgrade-library','manual','An article','', 'completed','research',true,true,'Exact original body','pending','Preserve the review reason','{"collector":"fixture","user_note":"preserve"}');
INSERT INTO knowledges(id,tenant_id,knowledge_base_id,type,title,source,parse_status,metadata)
VALUES('upgrade-intake',1,'upgrade-library','manual','Pending original','', 'pending_review','{"intake":{"decision":"pending","original_content":"Pending original body","reason":"missing source"}}');
INSERT INTO entities(id,tenant_id,canonical_name,normalized_name,entity_type,description,created_at,updated_at) VALUES('upgrade-entity',1,'Entity','entity','company','Original description',now(),now());
INSERT INTO entity_aliases(id,tenant_id,entity_id,alias,normalized_alias,created_at) VALUES('upgrade-alias',1,'upgrade-entity','Alias','alias',now());
INSERT INTO entity_merge_records(id,tenant_id,keep_entity_id,merge_entity_id,actor,created_at) VALUES('upgrade-merge',1,'upgrade-entity','old-entity','web_user:upgrade-user',now());
INSERT INTO topics(id,tenant_id,name,description) VALUES('upgrade-topic',1,'Topic','Original topic description');
INSERT INTO knowledge_entities(tenant_id,knowledge_id,entity_id) VALUES(1,'upgrade-article','upgrade-entity');
INSERT INTO knowledge_topics(tenant_id,knowledge_id,topic_id,source) VALUES(1,'upgrade-article','upgrade-topic','user');
INSERT INTO follows(tenant_id,target_type,target_id) VALUES(1,'entity','upgrade-entity'),(1,'topic','upgrade-topic');
INSERT INTO follow_feed_states(tenant_id,last_viewed_at) VALUES(1,'2026-10-10T00:00:00Z');
INSERT INTO operation_logs(id,tenant_id,actor,operation_type,object_id,before_value,after_value) VALUES('upgrade-log',1,'web_user:upgrade-user','follow','upgrade-entity','{}','{"followed":true}');
INSERT INTO data_sources(id,tenant_id,knowledge_base_id,name,type,config) VALUES('upgrade-source',1,'upgrade-library','RSS','rss','{"url":"https://example.invalid/feed"}');
INSERT INTO task_jobs(id,task_type,queue,state,payload,retried,fail_count) VALUES('upgrade-processing','document:process','default','active','{"knowledge_id":"upgrade-article"}',1,1);
INSERT INTO embeddings(source_id,source_type,dimension,embedding) VALUES('upgrade-article',1,3,'[1,2,3]'::halfvec);
INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,email,status) VALUES('00000000-0000-0000-0000-000000000015','fixture-client','https://example.invalid','subject','chat@example.invalid','needs_login');
`).Error)
	before := baselineContents(t, db)
	require.Greater(t, len(before), 35, "all previous-commit tables must be compared")
	t.Logf("exact full-column JSON comparison covers %d frozen-baseline tables", len(before))
	require.NoError(t, m.Migrate(1))
	version, dirty, err = m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 1, version)
	require.False(t, dirty)
	after := baselineContents(t, db)
	for table, content := range before {
		require.Equal(t, content, after[table], "migration lost or changed data in %s", table)
	}
	var count int64
	require.NoError(t, db.Table("maintenance_state").Count(&count).Error)
	require.EqualValues(t, 1, count)
	// Down is a structural test only. User rollback restores the complete old
	// backup and old application, rather than downgrading a live database.
	require.NoError(t, m.Steps(-1))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(1))
	require.Equal(t, before, baselineContents(t, db))
}
