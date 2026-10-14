package maintenance

import (
	"context"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"
)

func TestEvaluationReplicaRoundTrip(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO evaluation_sets(id,space_id,name) VALUES('set',1,'Evaluation');
 INSERT INTO evaluation_questions(id,space_id,set_id,method,request,source,status) VALUES('q',1,'set','ordinary','{"query":"test","libraries":["library"]}','manual','confirmed');
 INSERT INTO evaluation_answers(question_id,entry_id,grade) VALUES('q','article',2);
 INSERT INTO evaluation_runs(id,space_id,set_id,set_snapshot,options,status) VALUES('run',1,'set','{"name":"Evaluation"}','{"k":3}','completed');
 INSERT INTO evaluation_run_results(run_id,question_id,question,snapshot,results,metrics) VALUES('run','q','{"request":{"query":"test"}}','{"vector_set":"active"}','{"results":[]}','{"recall_at_k":0}');
 INSERT INTO search_records(id,space_id,source,method,query) VALUES('record',1,'web','ordinary','test');`).Error)
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, snapshot.Tables, "search_records")
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	for _, table := range evaluationPortableTables {
		_, err := os.Stat(filepath.Join(root, ".reliquary", PersistentTables[table].File))
		require.NoError(t, err)
	}
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	current, err := Capture(ctx, fresh)
	require.NoError(t, err)
	for _, table := range evaluationPortableTables {
		require.Equal(t, snapshot.Tables[table], current.Tables[table], table)
	}
	require.NoError(t, WriteSnapshot(root, "", current))
	check, err := VerifyReplica(ctx, fresh, root, "", false)
	require.NoError(t, err)
	require.True(t, check.OK, check.Differences)
	require.NoError(t, fresh.Exec("UPDATE evaluation_answers SET grade=1").Error)
	check, err = VerifyReplica(ctx, fresh, root, "", false)
	require.NoError(t, err)
	require.False(t, check.OK)
}

func TestEvaluationUpgradeVerification(t *testing.T) {
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
	require.NoError(t, m.Migrate(21))
	after, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec(`INSERT INTO evaluation_sets(id,space_id,name) VALUES('unexpected',1,'Must not disappear')`).Error)
	changed, err := BuildManifest(ctx, db, "", "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(before, changed))
}
