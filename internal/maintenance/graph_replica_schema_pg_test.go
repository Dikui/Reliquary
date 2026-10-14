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

func TestGraphReplicaMigrationPreservesAuthorityAndPendingRecords(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Migrate(18))
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at) VALUES('a',1,'A','a','company',now(),now()),('b',1,'B','b','company',now(),now());
 INSERT INTO library_relation_types(library_id,name,position,enabled) VALUES('library','cooperates',0,true);
 INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version) VALUES('r',1,'library','a','cooperates','b',7);
 INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('r','article','[]','run','hash');
 INSERT INTO graph_pending_sync(id,space_id,entity_id,relation_id,relation_version,deleted) VALUES('pending',1,'a','r',7,false),('deleted',1,'gone','gone',8,true);`).Error)
	before, err := BuildManifest(context.Background(), db, "", "")
	require.NoError(t, err)
	var pendingBefore string
	require.NoError(t, db.Raw(`SELECT jsonb_agg(to_jsonb(t) ORDER BY id)::text FROM graph_pending_sync t`).Scan(&pendingBefore).Error)
	require.NoError(t, m.Migrate(19))
	after, err := BuildManifest(context.Background(), db, "", "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	var pendingAfter string
	require.NoError(t, db.Raw(`SELECT jsonb_agg(to_jsonb(t)-'completed_at' ORDER BY id)::text FROM graph_pending_sync t`).Scan(&pendingAfter).Error)
	require.JSONEq(t, pendingBefore, pendingAfter)
	var n int64
	require.NoError(t, db.Table("graph_pending_sync").Where("completed_at IS NULL").Count(&n).Error)
	require.EqualValues(t, 2, n)
	require.NoError(t, m.Migrate(18))
	require.NoError(t, m.Migrate(19))
	require.NoError(t, ValidateClassification(context.Background(), db))
}
