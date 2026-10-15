package maintenance

import (
	"context"
	"fmt"
	"gorm.io/gorm"
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

// All pre-upgrade rows must survive 19, 21 and 22, including regenerated notices.
func TestGraphReplicaMergeUpgrade18To22(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer m.Close()
	require.NoError(t, m.Migrate(18))
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO graph_pending_sync(id,space_id,entity_id,relation_id,relation_version,deleted) VALUES('merge-live',1,'entity-a','gone',7,false),('merge-deleted',1,'gone','gone',8,true)`).Error)
	capture := func(database *gorm.DB, stripCompleted bool) map[string]string {
		var tables []string
		require.NoError(t, database.Raw("SELECT tablename FROM pg_tables WHERE schemaname='public' AND tablename<>'schema_migrations' ORDER BY tablename").Scan(&tables).Error)
		rows := map[string]string{}
		for _, table := range tables {
			expression := "to_jsonb(t)"
			if stripCompleted && table == "graph_pending_sync" {
				expression += "-'completed_at'"
			}
			var data string
			require.NoError(t, database.Raw(fmt.Sprintf(`SELECT COALESCE(jsonb_agg(%s ORDER BY %s::text),'[]'::jsonb)::text FROM public.%q t`, expression, expression, table)).Scan(&data).Error)
			rows[table] = data
		}
		return rows
	}
	before := capture(db, false)
	for _, version := range []uint{19, 21, 22} {
		require.NoError(t, m.Migrate(version))
		require.NoError(t, ValidateClassification(context.Background(), db))
		actual := capture(db, true)
		for table, data := range before {
			require.JSONEq(t, data, actual[table], "version %d table %s", version, table)
		}
		for table, data := range actual {
			if _, existed := before[table]; !existed {
				require.JSONEq(t, "[]", data, "new table %s", table)
			}
		}
	}
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
	require.NoError(t, ValidateClassification(context.Background(), db))
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.EqualValues(t, pgtest.LatestMigrationVersion(t), version)
	require.False(t, dirty)
}
