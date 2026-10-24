package maintenance

import (
	"context"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestRetiredSnapshotImportsOnlyRetainedData(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(32))
	seed(t, db)
	require.NoError(
		t,
		db.Exec(
			`INSERT INTO entities(id,
space_id,
canonical_name,
normalized_name,
entity_type,
created_at,
updated_at)
VALUES('a',
1,
'A',
'a',
'company',
now(),
now()),
('b',
1,
'B',
'b',
'company',
now(),
now());
INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('library','uses',0,true);
INSERT INTO relations(id,
space_id,
library_id,
subject_id,
relation_type,
object_id,
version)
VALUES('r',
1,
'library',
'a',
'uses',
'b',
1);
INSERT INTO task_pending_ops(id,task_type,queue,state,payload) VALUES
('retired-sync','graph:sync','low','pending','null'),
('retired-check','graph:check','low','pending','{}'),
('retired-rebuild','graph:rebuild','low','pending','{}'),
('retained-task','document:process','normal','pending','{}');
INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('r','article','[]','run','hash');
INSERT INTO entry_processing_outputs(entry_id,
space_id,
library_id,
body_hash,
content_type,
relations,
images)
VALUES('article',
1,
'library',
'hash',
'other',
'[{"type":"uses"}]',
'[]');`,
		).Error,
	)
	original, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", original))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	restored, err := Capture(ctx, target)
	require.NoError(t, err)
	for _, table := range retiredTables {
		require.NotContains(t, restored.Tables, table)
	}
	require.NoError(t, WriteSnapshot(root, "", restored))
	result, err := VerifyReplica(ctx, target, root, "", false)
	require.NoError(t, err)
	require.True(t, result.OK, result.Differences)
	require.NoError(t, target.Exec("UPDATE entries SET vault_body='tampered' WHERE id='article'").Error)
	require.Error(t, VerifyImportedReplica(ctx, target, root))
}

func TestClassificationSnapshotImportsRetainedData(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(33))
	seed(t, db)
	require.NoError(t, db.Exec(`
INSERT INTO topics(id,space_id,name) VALUES('retired',1,'Retired direction');
INSERT INTO entry_topics(space_id,entry_id,topic_id) VALUES(1,'article','retired');
INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'topic','retired');
INSERT INTO content_check_settings(space_id,enabled,weekday,time) VALUES(1,true,1,'09:00');
INSERT INTO content_check_runs(id,space_id,model_id,status,window_from,window_to)
VALUES('retired',1,'model','pending',now(),now());
INSERT INTO task_pending_ops(id,task_type,queue,state,payload)
VALUES('retired-check','content:check','low','pending','null'),
 ('retired-topic','topic:backfill','low','pending','{}'),
 ('retained-task','document:process','normal','pending','{}');
`).Error)
	original, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", original))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	restored, err := Capture(ctx, target)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", restored))
	result, err := VerifyReplica(ctx, target, root, "", false)
	require.NoError(t, err)
	require.True(t, result.OK, result.Differences)
	require.NoError(t, target.Exec("UPDATE entries SET vault_body='tampered' WHERE id='article'").Error)
	require.Error(t, VerifyImportedReplica(ctx, target, root))
}
