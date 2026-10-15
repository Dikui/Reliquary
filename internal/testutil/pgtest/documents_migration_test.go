package pgtest

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestImportedDocumentsMigrationPreservesPreviousTablesAndRetainsVectors(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(5))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Document migration','personal');
INSERT INTO knowledge_bases(id,name,space_id,embedding_model_id,summary_model_id) VALUES('library','Library',1,'','');
INSERT INTO entries(id,space_id,knowledge_base_id,type,title,source,source_kind,parse_status,vault_body,metadata) VALUES
 ('article',1,'library','manual','Retained article','https://example.test/original','article','completed','Retained body','{"published_at":"2026-10-01"}'),
 ('pending',1,'library','manual','Retained review','','article','rejected','Review original','{"intake":{"decision":"rejected","reason":"Review reason"}}');
INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('log',1,'user:owner','ingest','article','null','{"reason":"retain"}');
INSERT INTO task_jobs(id,task_type,queue,payload,state) VALUES('job','manual:process','default','{"entry_id":"pending"}','pending');
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(6))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(5))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(6))
	require.NoError(t, db.Exec(`
INSERT INTO entries(id,space_id,knowledge_base_id,type,title,source,source_kind,parse_status,processing_mode,vault_body,structured_summary,metadata) VALUES
 ('mine',1,'library','manual','Imported note','','mine','completed','full','Original note','{"overview":"Recorded evidence"}','{"published_at":"2026-10-02"}');
INSERT INTO article_embeddings(space_id,entry_id,knowledge_base_id,model_id,dimension,embedding,content_hash) VALUES(1,'mine','library','fixture',3,'[1,0,0]','summary');
INSERT INTO image_embeddings(space_id,entry_id,knowledge_base_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES(1,'mine','library','attachments/mine/image.png','fixture',3,'[1,0,0]','image');
UPDATE entries SET starred=true WHERE id='mine';
`).Error)
	var date string
	require.NoError(t, db.Raw("SELECT to_char(search_published_at AT TIME ZONE 'UTC','YYYY-MM-DD') FROM entries WHERE id='mine'").Scan(&date).Error)
	require.Equal(t, "2026-10-02", date)
	for _, table := range []string{"article_embeddings", "image_embeddings"} {
		var count int64
		require.NoError(t, db.Table(table).Where("entry_id='mine'").Count(&count).Error)
		require.EqualValues(t, 1, count, table)
	}
	require.NoError(t, db.Exec("UPDATE entries SET structured_summary='{}' WHERE id='mine'").Error)
	var count int64
	require.NoError(t, db.Table("article_embeddings").Where("entry_id='mine'").Count(&count).Error)
	require.Zero(t, count)
	t.Logf("version 5 to 6 preserved every row and column in %d tables", len(before))
}
