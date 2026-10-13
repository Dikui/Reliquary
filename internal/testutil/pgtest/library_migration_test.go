package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryNamingMigrationPreservesRowsAndSearch(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(6))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal');
INSERT INTO knowledge_bases(id,space_id,name,embedding_model_id,summary_model_id) VALUES('library',1,'Library','','');
INSERT INTO entries(id,space_id,knowledge_base_id,type,title,source,parse_status,enable_status) VALUES('entry',1,'library','manual','Libraryneedle','','completed','enabled');
INSERT INTO chunks(id,space_id,knowledge_base_id,entry_id,content,chunk_type,is_enabled,chunk_index,start_at,end_at) VALUES('chunk',1,'library','entry','Libraryneedle evidence','text',true,0,0,22);
INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,knowledge_base_id,dimension,embedding,is_enabled) VALUES('chunk',0,'chunk','entry','library',3,'[1,0,0]',true);
INSERT INTO article_embeddings(space_id,entry_id,knowledge_base_id,model_id,dimension,embedding,content_hash) VALUES(1,'entry','library','fixture',3,'[1,0,0]','hash');
INSERT INTO image_embeddings(space_id,entry_id,knowledge_base_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES(1,'entry','library','image.png','fixture',3,'[1,0,0]','hash');
INSERT INTO entry_repost_sources(id,space_id,knowledge_base_id,entry_id,source_fingerprint,match_method,created_at) VALUES('repost',1,'library','entry','fingerprint','url',now());
INSERT INTO task_dead_letters(space_id,task_type,scope,scope_id,payload,fail_count) VALUES(1,'manual:process','knowledge_base','library','{"knowledge_base_id":"library","knowledge_id":"entry"}',1);
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(7))
	after := baselineContents(t, db)
	for table, raw := range before {
		var rows, actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rows))
		for _, row := range rows {
			if value, ok := row["knowledge_base_id"]; ok {
				row["library_id"] = value
				delete(row, "knowledge_base_id")
			}
		}
		if table == "knowledge_bases" {
			table = "libraries"
		}
		require.NoError(t, json.Unmarshal([]byte(after[table]), &actual))
		require.ElementsMatch(t, rows, actual, table)
	}
	var count int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM entries WHERE library_id='library' AND search_seq_id @@@ 'search_text:Libraryneedle'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Raw(`SELECT count(*) FROM chunks WHERE library_id='library' AND seq_id @@@ 'content:Libraryneedle'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	for _, index := range []string{"chunks_search_idx", "entries_summary_search_idx"} {
		require.NoError(t, db.Raw(`SELECT count(*) FROM paradedb.schema(?::regclass) WHERE name='library_id'`, index).Scan(&count).Error)
		require.EqualValues(t, 1, count)
	}
	var id string
	require.NoError(t, db.Raw(`SELECT entry_id FROM embeddings WHERE library_id='library' ORDER BY embedding <=> '[1,0,0]'::halfvec LIMIT 1`).Scan(&id).Error)
	require.Equal(t, "entry", id)
	require.Error(t, db.Exec(`INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id) VALUES('second',1,'Forbidden','','')`).Error, "the single-library constraint must reject a second active row")
	require.NoError(t, m.Migrate(6))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(7))
	require.Equal(t, after, baselineContents(t, db))
}
