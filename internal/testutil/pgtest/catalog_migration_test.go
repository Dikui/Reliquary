package pgtest

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryCatalogMigrationPreservesIndependentCopies(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(32))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Catalog migration','personal');
INSERT INTO libraries(id,space_id,name,directory,template,embedding_model_id,summary_model_id)
VALUES('a',1,'A','a','own','',''),('b',1,'B','b','own','','');
INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('a','发布',0,true),('b','发布',0,true);
INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,vault_body)
VALUES('article-a',1,'a','manual','A','','article','Original A'),('article-b',1,'b','manual','B','','article','Original B');
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,description,name_embedding,embedding_model_id,embedding_dimension,created_at,updated_at)
VALUES('company',1,'Company','company','company','Hand edited','[1,0,0]','test',3,now(),now()),
('product',1,'Product','product','product','Original product',NULL,'',0,now(),now()),
('orphan',1,'Unlinked','unlinked','company','Keep manual profile',NULL,'',0,now(),now());
INSERT INTO entity_aliases(id,space_id,entity_id,entity_type,alias,normalized_alias,created_at)
VALUES('alias',1,'company','company','Alias','alias',now());
INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'article-a','company'),(1,'article-b','company'),(1,'article-a','product'),(1,'article-b','product');
INSERT INTO topics(id,space_id,name,description,library_ids) VALUES('topic',1,'AI','My definition','[]'),('restricted',1,'Restricted','Keep existing out-of-scope association','["a"]');
INSERT INTO entry_topics(space_id,entry_id,topic_id,source) VALUES(1,'article-a','topic','user'),(1,'article-b','topic','model'),(1,'article-b','restricted','user');
INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity','company'),(1,'topic','topic');
INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version)
VALUES('ra',1,'a','company','发布','product',4),('rb',1,'b','company','发布','product',9);
INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash)
VALUES('ra','article-a','[{"speaker":{"entity_id":"company"},"text":"Evidence A"}]','run-a','hash-a'),
('rb','article-b','[{"speaker":{"entity_id":"company"},"text":"Evidence B"}]','run-b','hash-b');
INSERT INTO entry_processing_outputs(entry_id,space_id,library_id,run_id,body_hash,content_type,relations,images,updated_at)
VALUES('article-b',1,'b','run-b','hash-b','other','[{"subject_id":"company","object_id":"product","type":"发布"}]','[]',now());
INSERT INTO topic_backfill_jobs(id,space_id,topic_id,topic_name,status,total)
VALUES('job',1,'topic','AI','pending',2);
INSERT INTO topic_backfill_batches(id,job_id,ordinal,entry_ids,entry_count,status)
VALUES('batch','job',1,'["article-a","article-b"]',2,'pending');
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(33))
	var counts struct{ Entities, Aliases, Topics, Follows, Relations, Sources int64 }
	require.NoError(t, db.Raw(`SELECT (SELECT count(*) FROM entities) entities,(SELECT count(*) FROM entity_aliases) aliases,
(SELECT count(*) FROM topics) topics,(SELECT count(*) FROM follows) follows,(SELECT count(*) FROM relations) relations,
(SELECT count(*) FROM relation_sources) sources`).Scan(&counts).Error)
	require.Equal(t, int64(6), counts.Entities)
	require.Equal(t, int64(2), counts.Aliases)
	require.Equal(t, int64(4), counts.Topics)
	require.Equal(t, int64(4), counts.Follows)
	require.Equal(t, int64(2), counts.Relations)
	require.Equal(t, int64(2), counts.Sources)
	var invalid int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM entry_entities ee JOIN entries e ON e.id=ee.entry_id JOIN entities o ON o.id=ee.entity_id WHERE e.library_id<>o.library_id`).Scan(&invalid).Error)
	require.Zero(t, invalid)
	require.NoError(t, db.Raw(`SELECT count(*) FROM entry_topics et JOIN entries e ON e.id=et.entry_id JOIN topics t ON t.id=et.topic_id WHERE e.library_id<>t.library_id`).Scan(&invalid).Error)
	require.Zero(t, invalid)
	var copied struct {
		ID, Description, Embedding string
		Dimension                  int
	}
	require.NoError(t, db.Raw(`SELECT id,description,name_embedding::text embedding,embedding_dimension dimension FROM entities WHERE library_id='b' AND normalized_name='company'`).Scan(&copied).Error)
	require.Equal(t, "Hand edited", copied.Description)
	require.Equal(t, "[1,0,0]", copied.Embedding)
	require.Equal(t, 3, copied.Dimension)
	var actualSubject, evidenceSpeaker, processedSubject, status string
	require.NoError(t, db.Raw(`SELECT subject_id FROM relations WHERE id='rb'`).Scan(&actualSubject).Error)
	require.NoError(t, db.Raw(`SELECT evidence#>>'{0,speaker,entity_id}' FROM relation_sources WHERE relation_id='rb'`).Scan(&evidenceSpeaker).Error)
	require.NoError(t, db.Raw(`SELECT relations#>>'{0,subject_id}' FROM entry_processing_outputs WHERE entry_id='article-b'`).Scan(&processedSubject).Error)
	require.Equal(t, copied.ID, actualSubject)
	require.Equal(t, copied.ID, evidenceSpeaker)
	require.Equal(t, copied.ID, processedSubject)
	require.NoError(t, db.Raw(`SELECT status FROM topic_backfill_jobs WHERE id='job'`).Scan(&status).Error)
	require.Equal(t, "cancelled", status)
	after := baselineContents(t, db)
	for _, table := range []string{"spaces", "libraries", "entries", "topic_backfill_batches"} {
		require.JSONEq(t, before[table], after[table], table)
	}
	// A new cross-library association and edge must be rejected by the database.
	require.Error(t, db.Exec(`INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'article-b','company')`).Error)
	require.Error(t, db.Exec(`INSERT INTO entry_topics(space_id,entry_id,topic_id) VALUES(1,'article-b','topic')`).Error)
	require.Error(t, db.Exec(`UPDATE relations SET subject_id='company' WHERE id='rb'`).Error)
	require.Error(t, db.Exec(`UPDATE entities SET library_id='a' WHERE id=?`, copied.ID).Error)
}

func TestLibraryCatalogAllMigrationsUpDownUp(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
}
