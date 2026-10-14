package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
)

func TestIKB12RelationMigrationReplicaAndTamper(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(17))
	seed(t, db)
	before, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(18))
	after, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec(`
 INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at) VALUES('graph-a',1,'Graph A','graph a','company',now(),now()),('graph-b',1,'Graph B','graph b','company',now(),now());
 INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('library','合作',0,true),('library','投资',1,true);
 INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version) VALUES('graph-r',1,'library','graph-a','合作','graph-b',7);
 INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('graph-r','article','[{"locations":[{"start":1,"end":5}],"speaker":{"entity_id":"graph-a","name":"A","selection":"extracted_person"}}]','run','hash');
 INSERT INTO relation_suppressions(space_id,library_id,subject_id,relation_type,object_id,actor) VALUES(1,'library','graph-b','合作','graph-a','user');
 INSERT INTO relation_type_merges(library_id,from_type,to_type) VALUES('library','投资','合作');
 INSERT INTO graph_pending_sync(id,space_id,entity_id,relation_id,relation_version,deleted) VALUES('pending',1,'graph-a','graph-r',7,false);
 `).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, snapshot.Tables, "graph_pending_sync")
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	var pending int64
	require.NoError(t, fresh.Table("graph_pending_sync").Count(&pending).Error)
	require.Zero(t, pending)
	for _, table := range []string{"relations", "relation_sources", "relation_suppressions", "relation_type_merges", "library_relation_types"} {
		require.FileExists(t, filepath.Join(root, ".reliquary/data", table+".json"))
	}
	for _, sql := range []string{"UPDATE relations SET version=8", "UPDATE relation_sources SET evidence='[]'", "UPDATE relation_suppressions SET actor='changed'", "UPDATE relation_type_merges SET created_at=now()+interval '1 day'", "UPDATE library_relation_types SET enabled=false"} {
		require.Error(t, fresh.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(sql).Error; err != nil {
				return err
			}
			return VerifyImportedReplica(ctx, tx, root)
		}))
		require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	}
	path := filepath.Join(root, ".reliquary/data/relations.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, ' '), 0600))
	_, err = Load(root)
	require.ErrorContains(t, err, "checksum mismatch")
}
