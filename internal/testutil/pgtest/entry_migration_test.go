package pgtest

import (
	"encoding/json"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"testing"
)

func TestEntryNamingMigrationPreservesDataConstraintsAndRetrieval(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(2))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business) VALUES(1,'Naming fixture','personal');
 INSERT INTO knowledge_bases(id,name,space_id,embedding_model_id,summary_model_id) VALUES('library','Library',1,'','');
 INSERT INTO knowledges(id,space_id,knowledge_base_id,type,title,source,source_kind,parse_status,enable_status,metadata) VALUES
 ('article',1,'library','manual','Headerneedle article','','article','completed','enabled','{"knowledge_id":"opaque","entry_id":"opaque new"}'),
 ('mine',1,'library','manual','Headerneedle mine','','mine','completed','enabled','{}'),
 ('report',1,'library','manual','Headerneedle report','','report','completed','enabled','{}');
 INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at) VALUES('entity',1,'Entityneedle','entityneedle','company',now(),now());
 INSERT INTO topics(id,space_id,name) VALUES('topic',1,'Topicneedle');
 INSERT INTO knowledge_entities(space_id,knowledge_id,entity_id) VALUES(1,'article','entity');
 INSERT INTO knowledge_topics(space_id,knowledge_id,topic_id,source) VALUES(1,'article','topic','user');
 INSERT INTO knowledge_processing_spans(knowledge_id,attempt,span_id,name,kind,status,input) VALUES('article',1,'root','knowledge_processing','root','done','{"knowledge_id":"article"}');
 INSERT INTO knowledge_repost_sources(id,space_id,knowledge_base_id,knowledge_id,source_fingerprint,match_method,created_at) VALUES('repost',1,'library','article',repeat('a',64),'url',now());
 INSERT INTO chunks(id,space_id,knowledge_base_id,knowledge_id,content,chunk_type,is_enabled,chunk_index,start_at,end_at) VALUES('chunk',1,'library','mine','Original mine evidence','text',true,0,0,22);
 INSERT INTO embeddings(source_id,source_type,chunk_id,knowledge_id,knowledge_base_id,dimension,embedding,is_enabled) VALUES('chunk',0,'chunk','mine','library',3,'[1,0,0]',true);
 INSERT INTO article_embeddings(space_id,knowledge_id,knowledge_base_id,model_id,dimension,embedding,content_hash) VALUES(1,'article','library','fixture',3,'[1,0,0]','hash');
 INSERT INTO task_jobs(id,task_type,queue,state,payload) VALUES('old-task','manual:process','default','pending','{"tenant_id":1,"knowledge_id":"mine"}');
 `).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(3))
	after := baselineContents(t, db)
	names := map[string]string{"knowledges": "entries", "knowledge_entities": "entry_entities", "knowledge_topics": "entry_topics", "knowledge_processing_spans": "entry_processing_spans", "knowledge_repost_sources": "entry_repost_sources"}
	for table, raw := range before {
		var rows, actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rows))
		for _, row := range rows {
			for _, pair := range [][2]string{{"knowledge_id", "entry_id"}, {"knowledge_ids", "entry_ids"}} {
				if v, ok := row[pair[0]]; ok {
					row[pair[1]] = v
					delete(row, pair[0])
				}
			}
		}
		name := table
		if mapped, ok := names[name]; ok {
			name = mapped
		}
		require.NoError(t, json.Unmarshal([]byte(after[name]), &actual))
		require.ElementsMatch(t, rows, actual, table)
	}
	var leftovers []string
	require.NoError(t, db.Raw(obsoleteEntrySchemaObjects).Scan(&leftovers).Error)
	require.Empty(t, leftovers)
	for _, sql := range []string{
		`INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'missing','entity')`,
		`INSERT INTO entry_topics(space_id,entry_id,topic_id) VALUES(1,'article','missing')`,
		`INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'article','entity')`,
		`INSERT INTO article_embeddings(space_id,entry_id,knowledge_base_id,model_id,dimension,embedding,content_hash) VALUES(1,'missing','library','fixture',3,'[1,0,0]','hash')`,
	} {
		require.Error(t, db.Exec(sql).Error, sql)
	}
	var count int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM entries WHERE search_seq_id @@@ 'search_text:Entityneedle'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	var id string
	require.NoError(t, db.Raw(`SELECT entry_id FROM embeddings ORDER BY embedding <=> '[1,0,0]'::halfvec LIMIT 1`).Scan(&id).Error)
	require.Equal(t, "mine", id)
	require.NoError(t, db.Raw(`SELECT name FROM paradedb.schema('entries_summary_search_idx'::regclass) WHERE name='space_id'`).Scan(&id).Error)
	require.Equal(t, "space_id", id)
	require.NoError(t, m.Migrate(2))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(3))
	require.NoError(t, db.Raw(obsoleteEntrySchemaObjects).Scan(&leftovers).Error)
	require.Empty(t, leftovers)
}

const obsoleteEntrySchemaObjects = `WITH names AS (
 SELECT 'column:'||table_schema||'.'||table_name||'.'||column_name AS name, column_name AS token FROM information_schema.columns WHERE table_schema NOT IN ('pg_catalog','information_schema') AND table_schema NOT LIKE 'pg_toast%'
 UNION ALL SELECT 'relation:'||n.nspname||'.'||c.relname,c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'
 UNION ALL SELECT 'constraint:'||conname,conname FROM pg_constraint WHERE connamespace IN (SELECT oid FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%')
 UNION ALL SELECT 'function:'||proname,proname FROM pg_proc WHERE pronamespace IN (SELECT oid FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%')
 UNION ALL SELECT 'trigger:'||tgname,tgname FROM pg_trigger WHERE NOT tgisinternal
 UNION ALL SELECT 'namespace:'||nspname,nspname FROM pg_namespace
 UNION ALL SELECT 'type:'||typname,typname FROM pg_type WHERE typnamespace IN (SELECT oid FROM pg_namespace WHERE nspname NOT IN ('pg_catalog','information_schema') AND nspname NOT LIKE 'pg_toast%')
) SELECT name FROM names WHERE token ~* '(^|_)knowledge(s|_|$)' AND token NOT ILIKE '%knowledge_base%' ORDER BY name;
`
