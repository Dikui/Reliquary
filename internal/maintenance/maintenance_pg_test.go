package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMain(m *testing.M) { os.Exit(pgtest.Run(m)) }

func seed(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, WithCurrentNames(db, func(tx *gorm.DB) error { seedCurrentNames(t, tx); return nil }))
}

func seedCurrentNames(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`INSERT INTO spaces(id,name,business,credentials) VALUES(1,'Owner','personal','{"app_secret":"SPACE_SECRET"}')`,
		`INSERT INTO users(id,username,email,password_hash,space_id) VALUES('owner','owner','owner@example.test','$2a$10$preserved-owner-password-hash',1)`,
		`INSERT INTO libraries(id,name,space_id,embedding_model_id,summary_model_id,chunking_config,indexing_strategy) VALUES('library','Personal',1,'embed','chat','{"chunk_size":512,"chunk_overlap":0}','{"keyword_enabled":true,"vector_enabled":true}')`,
		`INSERT INTO models(id,space_id,name,type,source,parameters) VALUES('chat',1,'chat','KnowledgeQA','remote','{"api_key":"MODEL_SECRET","base_url":"http://user:PASSWORD@example.test/v1?api_key=QUERY_SECRET&normal=kept","nested":{"headers":{"Authorization":"Bearer HEADER_SECRET"},"ordinary":"keep"}}'),('embed',1,'embedding','Embedding','remote','{"dimension":3,"base_url":"http://localhost:19100"}')`,
		`INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,description,created_at,updated_at) VALUES('entity-a',1,'Company A','company a','company','Owner description',now(),now()),('entity-b',1,'Company B','company b','company','Merged description',now(),now())`,
		`INSERT INTO entity_aliases(id,space_id,entity_type,entity_id,alias,normalized_alias,resolution_method,created_at) VALUES('alias',1,'company','entity-a','A Inc','a inc','exact',now())`,
		`INSERT INTO entity_merge_records(id,space_id,keep_entity_id,merge_entity_id,actor,created_at) VALUES('merge',1,'entity-a','entity-b','owner',now())`,
		`UPDATE entities SET merged_into_id='entity-a' WHERE id='entity-b'`,
		`INSERT INTO topics(id,space_id,name,description) VALUES('topic',1,'Maintenance','User topic description')`,
		`INSERT INTO entries(id,space_id,library_id,type,title,content_type,content_type_corrected,source_unverified,source_kind,source,parse_status,metadata,custom_metadata,collect_reason,agent_summary,agent_tags,vault_body) VALUES('pending',1,'library','manual','Pending source','research',false,false,'article','','rejected','{"manual":{"content":"Original review text"},"intake":{"decision":"rejected","reason":"missing link"}}','{"caller":"Grok"}','collect because useful','Caller summary','["tag"]','Pending original'),('article',1,'library','manual','Research article','research',true,true,'article','','completed','{"intake":{"decision":"approved","source_info":{"publisher":"Publisher"}}}','{}','owner reason','','[]','Immutable body')`,
		`INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'article','entity-a')`,
		`INSERT INTO entry_topics(space_id,entry_id,topic_id,source,is_historical) VALUES(1,'article','topic','user',true)`,
		`INSERT INTO entry_repost_sources(id,space_id,library_id,entry_id,original_url,source_fingerprint,match_method,created_at) VALUES('repost',1,'library','article','https://example.test/repost','abcdef','url',now())`,
		`INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity','entity-a'),(1,'topic','topic')`,
		`INSERT INTO follow_feed_states(space_id,last_viewed_at) VALUES(1,'2026-10-10T12:00:00Z')`,
		`INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('op-1',1,'owner','follow','entity-a','{}','{"followed":true}')`,
		`INSERT INTO resources(id,handle,space_id,provider,physical_path,location_hash) VALUES('resource','1234567890123456789012',1,'local','local://1/pending/raw.txt','location')`,
		`INSERT INTO resource_bindings(id,resource_id,space_id,owner_type,owner_id) VALUES('binding','resource',1,'entry','pending')`,
		`INSERT INTO task_pending_ops(id,task_type,queue,payload,state,retried,fail_count,worker_pool) VALUES('job','manual:process','default','{"entry_id":"pending"}','active',2,1,'core')`,
		`INSERT INTO api_keys(id,space_id,name,key_hash,api_key) VALUES(1,1,'external caller','API_HASH_SECRET','API_SECRET')`,
		`INSERT INTO chatgpt_hosts(id,ext_agent_host_id) VALUES(1,'host-preserved')`,
		`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,email,credentials,status) VALUES('00000000-0000-0000-0000-000000000001','client-preserved','https://auth.openai.com','subject-preserved','chatgpt@example.test','CHATGPT_TOKEN_SECRET','connected')`,
		`UPDATE maintenance_state SET backup_at='2026-10-01T12:00:00Z',backup_status='success',backup_path='/old-machine/backup',backup_size_bytes=1234,backup_error='',drill_at='2026-10-02T12:00:00Z',drill_status='failed',drill_error='retained historical failure' WHERE id=true`,
	}
	for _, statement := range statements {
		if !db.Migrator().HasTable("task_pending_ops") {
			statement = strings.ReplaceAll(statement, "task_pending_ops", "task_jobs")
		}
		require.NoError(t, db.Exec(statement).Error)
	}
}

func TestClassificationCoversEveryTableColumnAndRejectsNewColumns(t *testing.T) {
	db := pgtest.Open(t)
	require.NoError(t, ValidateClassification(context.Background(), db))
	require.NoError(t, db.Exec("ALTER TABLE public.topics ADD COLUMN accidental_new_field text").Error)
	require.ErrorContains(t, ValidateClassification(context.Background(), db), "topics.accidental_new_field")
}

func TestReplicaPreservesJSONLiteralNullDistinctFromSQLNull(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	require.NoError(t, source.Exec(`INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('op-json-null',1,'owner','create_topic','topic','null'::jsonb,'null'::jsonb)`).Error)
	require.NoError(t, source.Exec(`UPDATE entries SET structured_summary='null'::jsonb WHERE id='pending'`).Error)
	require.NoError(t, source.Exec(`UPDATE entries SET structured_summary=NULL WHERE id='article'`).Error)
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"before_value", "after_value"}, snapshot.JSONNulls["operation_logs"]["op-json-null"])
	require.Contains(t, snapshot.JSONNulls["entries"]["pending"], "structured_summary")
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	var jsonNull, sqlNull bool
	require.NoError(t, target.Raw(`SELECT before_value='null'::jsonb AND after_value='null'::jsonb FROM operation_logs WHERE id='op-json-null'`).Scan(&jsonNull).Error)
	require.True(t, jsonNull)
	require.NoError(t, target.Raw(`SELECT structured_summary IS NULL FROM entries WHERE id='article'`).Scan(&sqlNull).Error)
	require.True(t, sqlNull)
	restored, err := Capture(ctx, target)
	require.NoError(t, err)
	require.Equal(t, snapshot.JSONNulls, restored.JSONNulls)
}

func TestManifestOnlyAllowsCredentialPauseAndLiveTaskLeaseChanges(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO task_pending_ops(id,task_type,queue,payload,state,started_at,completed_at,updated_at) VALUES('completed-job','manual:process','default','{}','completed','2026-10-01T12:00:00Z','2026-10-01T12:01:00Z','2026-10-01T12:01:00Z')`).Error)
	root, uploads := t.TempDir(), t.TempDir()
	expected, err := BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE models SET status='disabled' WHERE id='embed'`).Error)
	actual, err := BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table models")
	require.NoError(t, db.Exec(`UPDATE models SET status='active' WHERE id='embed'`).Error)
	require.NoError(t, db.Exec(`UPDATE models SET status='needs_configuration',updated_at=now() WHERE id='chat'`).Error)
	require.NoError(t, db.Exec(`UPDATE task_pending_ops SET state='pending',delivery=delivery+1,started_at=NULL,updated_at=now(),worker_pool='' WHERE id='job'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))
	require.NoError(t, db.Exec(`UPDATE models SET status='disabled' WHERE id='chat'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table models")
	require.NoError(t, db.Exec(`UPDATE task_pending_ops SET started_at='2026-10-01T11:59:00Z' WHERE id='completed-job'`).Error)
	actual, err = BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table task_pending_ops")
}

func TestPortableReplicaRoundtripEveryPersistentTable(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	root := t.TempDir()
	uploads := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "1/pending"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "1/pending/raw.txt"), []byte("Original review attachment"), 0600))
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	loaded, err := Load(root)
	require.NoError(t, err)
	require.Equal(t, snapshot.Tables, loaded.Tables)
	files, err := Files(root)
	require.NoError(t, err)
	for _, file := range files {
		data, err := os.ReadFile(filepath.Join(root, file.Path))
		require.NoError(t, err)
		for _, secret := range []string{"MODEL_SECRET", "SPACE_SECRET", "PASSWORD", "QUERY_SECRET", "HEADER_SECRET", "RSS_SECRET", "API_HASH_SECRET", "API_SECRET", "CHATGPT_TOKEN_SECRET"} {
			require.NotContains(t, string(data), secret, file.Path)
		}
	}
	require.Contains(t, string(mustRead(t, filepath.Join(root, ".reliquary/config/users.json"))), "preserved-owner-password-hash")
	target := pgtest.Open(t)
	newUploads := t.TempDir()
	_, err = Import(ctx, target, root, newUploads, nil)
	require.NoError(t, err)
	restored, err := Capture(ctx, target)
	require.NoError(t, err)
	for table, rows := range snapshot.Tables {
		left, right := comparableCredentialRows(table, rows, restored.Tables[table], snapshot.Required)
		want, _ := json.Marshal(left)
		got, _ := json.Marshal(right)
		require.JSONEq(t, string(want), string(got), table)
	}
	require.Equal(t, "Original review attachment", string(mustRead(t, filepath.Join(newUploads, "1/pending/raw.txt"))))
	var job struct {
		State     string
		Retried   int
		FailCount int
	}
	require.NoError(t, target.Table("task_pending_ops").Where("id='job'").First(&job).Error)
	require.Equal(t, "active", job.State)
	require.Equal(t, 2, job.Retried)
	require.Equal(t, 1, job.FailCount)
}

func TestReplicaTamperingStopsLoadAndVerify(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	root := t.TempDir()
	snapshot, err := Capture(context.Background(), db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	path := filepath.Join(root, ".reliquary/data/topics.json")
	require.NoError(t, os.WriteFile(path, []byte("[]\n"), 0600))
	_, err = Load(root)
	require.ErrorContains(t, err, "checksum mismatch")
	result, err := VerifyReplica(context.Background(), db, root, "", false)
	require.NoError(t, err)
	require.False(t, result.OK)
	result, err = VerifyReplica(context.Background(), db, root, "", true)
	require.NoError(t, err)
	require.True(t, result.OK)
	_, err = Load(root)
	require.NoError(t, err)
}

func TestScrubRemovesNestedSecretsPausesModelsAndKeepsOriginalDatabase(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	result, err := Scrub(ctx, db)
	require.NoError(t, err)
	require.Contains(t, result.PausedModels, "chat")
	require.Empty(t, result.PausedSources)
	require.NotEmpty(t, result.Required)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	data, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, secret := range []string{"MODEL_SECRET", "SPACE_SECRET", "QUERY_SECRET", "RSS_SECRET", "CHATGPT_TOKEN_SECRET", "API_SECRET"} {
		require.NotContains(t, string(data), secret)
	}
	var modelStatus string
	require.NoError(t, db.Raw("SELECT status FROM models WHERE id='chat'").Scan(&modelStatus).Error)
	require.Equal(t, "needs_configuration", modelStatus)
}

func TestAtomicWriteKeepsUnchangedFileAndReplacesChangedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "safe.json")
	require.NoError(t, AtomicWrite(path, []byte("first")))
	first, err := os.Stat(path)
	require.NoError(t, err)
	require.NoError(t, AtomicWrite(path, []byte("first")))
	same, err := os.Stat(path)
	require.NoError(t, err)
	require.True(t, os.SameFile(first, same))
	require.NoError(t, AtomicWrite(path, []byte("second")))
	second, err := os.Stat(path)
	require.NoError(t, err)
	require.False(t, os.SameFile(first, second))
	require.Equal(t, "second", string(mustRead(t, path)))
}

func TestVectorManifestComparesCoverageAndProfileNotValuesOrChunkIDs(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	seed(t, db)
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	for _, statement := range []string{`INSERT INTO chunks(id,space_id,library_id,entry_id,content,chunk_index,start_at,end_at,content_hash) VALUES('chunk-a',1,'library','article','observations',0,0,12,'chunkhash')`, `INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,library_id,model_id,dimension,embedding) VALUES('chunk-a',0,'chunk-a','article','library','embed',3,'[1,0,0]')`, `INSERT INTO article_embeddings(entry_id,space_id,library_id,model_id,dimension,embedding,content_hash) VALUES('article',1,'library','embed',3,'[1,0,0]','articlehash')`, `INSERT INTO image_embeddings(entry_id,space_id,library_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES('article',1,'library','../../attachments/article/image.png','embed',3,'[1,0,0]','imagehash')`, `UPDATE entities SET name_embedding='[1,0,0]',embedding_model_id='embed',embedding_dimension=3 WHERE id='entity-a'`} {
		require.NoError(t, db.Exec(statement).Error)
	}
	wanted, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Len(t, wanted.Vectors, 4)
	require.Contains(t, wanted.Retrieval[1].IDs, "article")
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings"} {
		require.NoError(t, db.Exec("UPDATE "+table+" SET embedding='[0,1,0]'").Error)
	}
	require.NoError(t, db.Exec("UPDATE entities SET name_embedding='[0,1,0]' WHERE id='entity-a'").Error)
	require.NoError(t, db.Exec("UPDATE chunks SET id='chunk-b' WHERE id='chunk-a'").Error)
	require.NoError(t, db.Exec("UPDATE embeddings SET chunk_id='chunk-b',source_id='chunk-b' WHERE chunk_id='chunk-a'").Error)
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareVectors(wanted, actual))
	require.NoError(t, db.Exec("UPDATE image_embeddings SET content_hash='tampered'").Error)
	actual, err = BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareVectors(wanted, actual))
	require.NoError(t, db.Exec("UPDATE image_embeddings SET content_hash='imagehash'").Error)
	require.NoError(t, db.Exec("UPDATE models SET parameters=jsonb_set(parameters,'{embedding_parameters}','{\"dimension\":4}') WHERE id='embed'").Error)
	actual, err = BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareVectors(wanted, actual))
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

func TestReplicaAtomicDebouncedChangesAndAppendOnlyLogs(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	root := t.TempDir()
	writer := NewWriter(db, root, "", 120*time.Millisecond)
	t.Cleanup(func() { require.NoError(t, writer.Close()) })
	require.NoError(t, writer.Flush(context.Background()))
	topicPath := filepath.Join(root, ".reliquary/data/topics.json")
	previousTopic := mustRead(t, topicPath)
	require.NoError(t, db.Exec("UPDATE topics SET description=? WHERE id='topic'", strings.Repeat("changed", 8)).Error)
	for i := 0; i < 20; i++ {
		writer.Schedule()
	}
	require.Equal(t, previousTopic, mustRead(t, topicPath), "trailing debounce must defer changes")
	require.Eventually(t, func() bool {
		data, _ := os.ReadFile(filepath.Join(root, ".reliquary/data/topics.json"))
		return bytes.Contains(data, []byte(strings.Repeat("changed", 8)))
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, writer.Start())
	logPath := filepath.Join(root, ".reliquary/operation_logs.jsonl")
	require.NoError(t, writer.Flush(context.Background()))
	before := mustRead(t, logPath)
	oldInfo, err := os.Stat(logPath)
	require.NoError(t, err)
	// Each class changes through raw SQL, exercising the periodic fallback too.
	for _, statement := range []string{`UPDATE entities SET description='new description' WHERE id='entity-a'`, `UPDATE entity_aliases SET alias='New Alias' WHERE id='alias'`, `UPDATE entity_merge_records SET actor='reviewed owner' WHERE id='merge'`, `UPDATE entry_topics SET source='model' WHERE topic_id='topic'`, `UPDATE follow_feed_states SET last_viewed_at=now()`, `UPDATE entries SET content_type_corrected=false WHERE id='article'`, `UPDATE entries SET metadata=jsonb_set(metadata,'{intake,reason}','"new reason"') WHERE id='pending'`, `UPDATE entry_repost_sources SET author='new author'`, `UPDATE models SET display_name='renamed model'`, `INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('op-2',1,'owner','star','article','{}','{"starred":true}')`} {
		require.NoError(t, db.Exec(statement).Error)
	}
	require.Eventually(t, func() bool {
		snapshot, err := Capture(context.Background(), db)
		if err != nil {
			return false
		}
		files, err := replicaFiles(snapshot)
		if err != nil {
			return false
		}
		for path, wanted := range files {
			actual, err := os.ReadFile(filepath.Join(root, ".reliquary", path))
			if err != nil || !bytes.Equal(wanted, actual) {
				return false
			}
		}
		return true
	}, 5*time.Second, 25*time.Millisecond)
	require.NoError(t, writer.Flush(context.Background()))
	after := mustRead(t, logPath)
	require.True(t, bytes.HasPrefix(after, before))
	newInfo, err := os.Stat(logPath)
	require.NoError(t, err)
	require.True(t, os.SameFile(oldInfo, newInfo))
	require.NoError(t, writer.Flush(context.Background()))
	require.Equal(t, after, mustRead(t, logPath))
	temp, err := filepath.Glob(filepath.Join(root, ".reliquary", "data", "*.tmp"))
	require.NoError(t, err)
	require.Empty(t, temp)
	require.NoError(t, db.Exec("UPDATE operation_logs SET actor='tampered' WHERE id='op-1'").Error)
	require.ErrorContains(t, writer.Flush(context.Background()), "was modified")
	require.NoError(t, db.Exec("UPDATE operation_logs SET actor='owner' WHERE id='op-1'").Error)
}
