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

// Medium spec, product shelf task 01 / ADR 0023: migration 41 relaxes only
// managed indexes, preserves old replicas, and refuses a lossy downgrade.
func TestManagedNamesMigrationPreservesLibraryScopes(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(40))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Name fixtures','personal');
	 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode) VALUES
	 ('managed',1,'Managed','managed','','','managed'),('extracted',1,'Extracted','extracted','','','extracted');
	 INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,code,created_at,updated_at) VALUES
	 ('platform',1,'managed','Shared','shared','product','platform','CODE',now(),now()),
	 ('company',1,'extracted','Shared','shared','company','platform','CODE',now(),now());`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	previous, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(41))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, before.Tables["entities"], after.Tables["entities"], "the discriminator is derived, not portable data")
	upgraded := t.TempDir()
	require.NoError(t, WriteSnapshot(upgraded, "", after))
	current, err := BuildManifest(ctx, db, upgraded, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(previous, current))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))

	// Extracted code ownership remains library-wide even across entity types.
	require.ErrorContains(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,code,created_at,updated_at)
	 VALUES('bad-code',1,'extracted','Other','other','product','model','code',now(),now())`).Error, "duplicate key")
	// Extracted canonical ownership remains per storage type, regardless of kind.
	require.ErrorContains(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,created_at,updated_at)
	 VALUES('bad-name',1,'extracted','Shared','shared','company','model',now(),now())`).Error, "duplicate key")
	require.NoError(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,created_at,updated_at)
	 VALUES('other-type',1,'extracted','Shared','shared','product','platform',now(),now())`).Error)
	// Managed kind ownership also applies to entities converted from other types.
	require.ErrorContains(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,created_at,updated_at)
	 VALUES('bad-managed',1,'managed','Shared','shared','company','platform',now(),now())`).Error, "duplicate key")
	require.NoError(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,code,created_at,updated_at)
	 VALUES('model',1,'managed','Shared','shared','product','model','code',now(),now())`).Error)
	require.ErrorContains(t, migration.Migrate(40), "cross-kind names")
	require.NoError(t, migration.Force(41))
	// Alias collisions must also block downgrade when canonical names differ.
	require.NoError(t, db.Exec(`UPDATE entities SET canonical_name='Other',normalized_name='other',code='OTHER' WHERE id='model';
	 INSERT INTO entity_aliases(id,space_id,library_id,entity_type,entity_id,alias,normalized_alias,resolution_method,created_at)
	 VALUES('alias-platform',1,'managed','product','platform','Shared alias','shared alias','manual',now()),
	 ('alias-model',1,'managed','product','model','Shared alias','shared alias','manual',now())`).Error)
	require.ErrorContains(t, migration.Migrate(40), "cross-kind names")
	require.NoError(t, migration.Force(41))
	require.NoError(t, db.Exec(`DELETE FROM entity_aliases WHERE library_id='managed'; UPDATE entities SET code='code' WHERE id='model'`).Error)
	require.ErrorContains(t, migration.Migrate(40), "cross-kind names")
	require.NoError(t, migration.Force(41))
	require.NoError(t, db.Exec(`DELETE FROM entities WHERE id='model'`).Error)
	require.NoError(t, migration.Migrate(40))
	require.NoError(t, migration.Migrate(41))
}

// Medium spec, product shelf task 01 / ADR 0023: confirmed pairs are portable
// master data and must not disappear when rebuilding from an exported replica.
func TestManagedNamesConfirmationsSurviveReplicaRestore(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed' WHERE id='library';
	 UPDATE entities SET kind=CASE id WHEN 'entity-a' THEN 'platform' ELSE 'model' END WHERE library_id='library';
	 INSERT INTO entity_name_confirmations(space_id,library_id,entity_a_id,kind_a,entity_b_id,kind_b,normalized_name)
	 VALUES(1,'library','entity-a','platform','entity-b','model','shared product')`).Error)
	ctx := context.Background()
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Len(t, snapshot.Tables["entity_name_confirmations"], 1)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	current, err := Capture(ctx, fresh)
	require.NoError(t, err)
	require.Equal(t, snapshot.Tables["entity_name_confirmations"], current.Tables["entity_name_confirmations"])
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.ErrorContains(t, migration.Migrate(40), "confirmed cross-kind names")
}
