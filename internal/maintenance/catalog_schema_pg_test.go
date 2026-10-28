package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestCatalogMigrationReplicaProjectionAndImport(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(32))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal');
 INSERT INTO libraries(id,space_id,name,directory,template,embedding_model_id,summary_model_id) VALUES('a',1,'A','a','own','',''),('b',1,'B','b','own','','');
 INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,parse_status) VALUES('first',1,'a','manual','First','','mine','completed'),('second',1,'b','manual','Second','','mine','completed');
 INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,merged_into_id,created_at,updated_at) VALUES('entity',1,'Entity','entity','company','',now(),now()),('merged',1,'Merged','merged','company','entity',now(),now()),('orphan',1,'Orphan','orphan','company','',now(),now());
 INSERT INTO entity_aliases(id,space_id,entity_type,entity_id,alias,normalized_alias,resolution_method,created_at) VALUES('alias',1,'company','entity','Alias','alias','exact',now());
 INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'first','entity'),(1,'second','entity');
 INSERT INTO topics(id,space_id,name,description,library_ids) VALUES('topic',1,'Topic','User supplied','["a"]');
 INSERT INTO entry_topics(space_id,entry_id,topic_id,source) VALUES(1,'first','topic','user'),(1,'second','topic','user');
 INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity','entity'),(1,'topic','topic');
 INSERT INTO entity_merge_records(id,space_id,keep_entity_id,merge_entity_id,actor,created_at) VALUES('merge',1,'entity','merged','owner',now());
 INSERT INTO entity_merge_suggestions(id,space_id,new_entity_id,candidate_entity_id,entry_id,reason,status,created_at) VALUES('suggestion',1,'merged','entity','second','Preserve','pending',now());
 INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('b','cooperates',0,true);
 INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version) VALUES('relation',1,'b','entity','cooperates','merged',1);
 INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('relation','second','[{"speaker":{"entity_id":"entity"},"literal":"entity suffix"}]','run','hash');
 INSERT INTO relation_suppressions(space_id,library_id,subject_id,relation_type,object_id,actor) VALUES(1,'b','merged','cooperates','entity','owner');
 INSERT INTO topic_backfill_jobs(id,space_id,topic_id,topic_name,topic_description,status,total) VALUES('job',1,'topic','Topic','User supplied','pending',2);
 INSERT INTO entry_processing_outputs(entry_id,space_id,library_id,body_hash,content_type,relations) VALUES('second',1,'b','hash','reference','[{"subject":"entity","object":"merged","topic":"topic","literal":"entity suffix"}]');
 INSERT INTO entry_segment_runs(id,space_id,entry_id,phase,plan_hash,body_hash,plan,result,status) VALUES('run',1,'second','processing','plan','hash','[{"entity":"entity"}]','{"topics":["topic"],"nested":{"value":"merged"}}','completed');
 `).Error)
	original, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", original))
	before, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(33))
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	after, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, projectCatalogSnapshot(original, 33))
	for _, table := range []string{"entities", "entity_aliases", "topics", "entry_entities", "entry_topics", "follows", "entity_merge_records", "entity_merge_suggestions", "topic_backfill_jobs", "entry_processing_outputs", "entry_segment_runs", "relations", "relation_sources", "relation_suppressions"} {
		require.Equal(t, current.Tables[table], original.Tables[table], table)
	}
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	current.schemaVersion = 33
	require.NoError(t, projectCatalogSnapshot(current, 33))
	require.NoError(t, db.Exec("UPDATE topics SET description='Tampered' WHERE library_id='b'").Error)
	changed, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, changed), "table topics")
}
