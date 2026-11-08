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

// Medium spec, task 27 / ADR 0023: upgrading keeps legacy records intact,
// duplicate and empty certification labels survive portable restoration, and
// downgrade refuses business data instead of renaming or deleting records.
func TestCertificationDisplayNamesMigrationAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(51))
	require.NoError(t, migration.Migrate(52))
	require.NoError(t, migration.Migrate(51), "an empty database remains downgradeable")
	seed(t, db)
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed' WHERE id='library';
 UPDATE entities SET kind='model' WHERE id='entity-b';
 UPDATE entities SET kind='certification',attributes='{"certification":{"owner_id":"entity-b","standard":"synthetic","certification_object":"Container","region":"A"}}' WHERE id='entity-a'`).Error)
	ctx := context.Background()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	legacy := t.TempDir()
	require.NoError(t, WriteSnapshot(legacy, "", before))
	require.NoError(t, migration.Migrate(52))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before.Tables["entities"], after.Tables["entities"])
	legacyDB := pgtest.Open(t)
	_, err = Import(ctx, legacyDB, legacy, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, legacyDB, legacy))

	require.NoError(t, db.Exec(`UPDATE entities SET canonical_name=' Shared label ',normalized_name='shared label' WHERE id='entity-a';
 INSERT INTO entities(id,space_id,library_id,entity_type,kind,canonical_name,normalized_name,attributes,created_at,updated_at) VALUES
 ('same-name',1,'library','product','certification',' Shared label ','shared label','{"certification":{"owner_id":"entity-b","standard":"synthetic","certification_object":"Container","region":"B"}}',now(),now()),
 ('empty-a',1,'library','product','certification','','','{"certification":{"owner_id":"entity-b","standard":"synthetic","certification_object":"Container","region":"C"}}',now(),now()),
 ('empty-b',1,'library','product','certification','','','{"certification":{"owner_id":"entity-b","standard":"synthetic","certification_object":"Container","region":"D"}}',now(),now());
 INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,attributes,effective_at,recorded_at)
 SELECT 'version-'||id,space_id,id,revision,attributes,now(),now() FROM entities WHERE kind='certification'`).Error)
	facts, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", facts))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	actual, err := Capture(ctx, fresh)
	require.NoError(t, err)
	for _, table := range []string{"entities", "entity_aliases", "entity_attribute_versions"} {
		require.Equal(t, facts.Tables[table], actual.Tables[table], table)
	}
	require.ErrorContains(t, migration.Migrate(51), "certification display-name data exists")
}
