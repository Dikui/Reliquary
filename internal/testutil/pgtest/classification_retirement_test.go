package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestClassificationRetirementPreservesRetainedData(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	if err := m.Migrate(33); err != migrate.ErrNoChange {
		require.NoError(t, err)
	}
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Saved','personal');
INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id)
VALUES('saved',1,'Saved','own','saved','','');
INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status,vault_body,
 content_type,content_type_corrected,content_type_hint,metadata,starred)
VALUES('saved',1,'saved','manual','Saved title','','completed','Exact saved body',
 'other',true,'other','{"custom":"saved","published_at":"2026-10-01"}',true);
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at)
VALUES('saved',1,'Saved entity','saved entity','company',now(),now());
INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'saved','saved');
INSERT INTO topics(id,space_id,name) VALUES('retired',1,'Retired direction');
INSERT INTO entry_topics(space_id,entry_id,topic_id) VALUES(1,'saved','retired');
INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'topic','retired'),(1,'entity','saved');
INSERT INTO library_content_types(library_id,name,position,enabled) VALUES('saved','other',0,true);
INSERT INTO entry_processing_outputs(entry_id,space_id,library_id,body_hash,content_type,images)
VALUES('saved',1,'saved','hash','other','[]');
INSERT INTO content_check_settings(space_id,enabled,weekday,time) VALUES(1,true,1,'09:00');
INSERT INTO content_check_runs(id,space_id,model_id,status,window_from,window_to)
VALUES('retired',1,'model','pending',now(),now());
INSERT INTO topic_backfill_jobs(id,space_id,topic_id,topic_name) VALUES('retired',1,'retired','Retired direction');
INSERT INTO topic_backfill_batches(id,job_id,ordinal,entry_ids,entry_count)
VALUES('retired','retired',1,'["saved"]',1);
INSERT INTO task_pending_ops(id,task_type,queue,state,payload)
VALUES('check','content:check','low','pending','null'),
 ('backfill','topic:backfill','low','pending','{}'),
 ('saved','document:process','normal','pending','{}');
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(34))
	after := baselineContents(t, db)
	for _, table := range []string{
		"topics",
		"entry_topics",
		"topic_backfill_jobs",
		"topic_backfill_batches",
		"library_content_types",
		"content_check_settings",
		"content_check_runs",
	} {
		require.NotContains(t, after, table)
		delete(before, table)
	}
	for table, content := range before {
		var rows []map[string]any
		require.NoError(t, json.Unmarshal([]byte(content), &rows))
		kept := rows[:0]
		for _, row := range rows {
			if table == "task_pending_ops" && row["id"] != "saved" {
				continue
			}
			if table == "follows" && row["target_type"] == "topic" {
				continue
			}
			if table == "entries" {
				for _, column := range []string{
					"content_type",
					"content_type_corrected",
					"content_type_hint",
					"search_text",
				} {
					delete(row, column)
				}
			}
			if table == "entry_processing_outputs" {
				delete(row, "content_type")
			}
			if table == "follows" {
				delete(row, "topic_id")
			}
			kept = append(kept, row)
		}
		var actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(after[table]), &actual))
		if table == "entries" {
			for _, row := range actual {
				delete(row, "search_text")
			}
		}
		require.Equal(t, kept, actual, table)
	}
	var searchText string
	require.NoError(t, db.Raw("SELECT search_text FROM entries WHERE id='saved'").Scan(&searchText).Error)
	require.Contains(t, searchText, "Saved entity")
	require.NotContains(t, searchText, "Retired direction")
	// The full migration chain must remain reversible in a temporary database.
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'After cycle','personal');
INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id)
VALUES('after',1,'After','own','after','','');
INSERT INTO entries(id,space_id,library_id,type,title,source,vault_body)
VALUES('after',1,'after','manual','After','','Saved after all migrations');
`).Error)
}
