package maintenance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Medium spec, task 28 / ADR 0023: legacy part facts remain intact, new
// composition fields survive replica restore, and downgrade protects new data.
func TestShelfCompositionModelsMigrationAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(52))
	seed(t, db)
	schema := types.ProductShelfSchema()
	for i := range schema.Fields {
		schema.Fields[i].Archived = false
	}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed',shelf_enabled=true,entity_schema=?::jsonb WHERE id='library';`, string(encoded)).Error)
	require.NoError(t, db.Exec(`UPDATE entities SET kind='model' WHERE id='entity-a';
 UPDATE entities SET kind='part',attributes='{"pack_model":"Pack","cluster_model":" Legacy cluster ","container_model":"Legacy container"}' WHERE id='entity-b';
 INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,attributes,effective_at,recorded_at)
 SELECT 'legacy-version',space_id,id,revision,attributes,now(),now() FROM entities WHERE id='entity-b';
 INSERT INTO library_relation_types(library_id,name,enabled,position,fields,subject_kinds,object_kinds) VALUES
 ('library','composed_of',true,0,'[{"key":"module_type","name":"模块类型","type":"text","kinds":[],"archived":false}]','["model"]','["part"]');
 INSERT INTO relations(id,space_id,library_id,subject_id,object_id,relation_type,version,attributes,created_at,updated_at)
 VALUES('relation',1,'library','entity-a','entity-b','composed_of',1,'{"module_type":"synthetic"}',now(),now())`).Error)
	ctx := context.Background()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(53))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	for _, table := range []string{"entities", "entity_attribute_versions", "relations"} {
		require.Equal(t, before.Tables[table], after.Tables[table], table)
	}
	var library types.Library
	require.NoError(t, db.Preload("RelationTypes").First(&library, "id='library'").Error)
	for _, field := range library.EntitySchema.Fields {
		if field.Key == "cluster_model" || field.Key == "container_model" {
			require.True(t, field.Archived)
		}
	}
	require.Equal(t, types.ShelfCompositionFields(), library.RelationTypes[0].Fields)
	upgradedRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(upgradedRoot, "", after))
	newManifest, err := BuildManifest(ctx, db, upgradedRoot, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, newManifest))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	require.NoError(t, restored.Preload("RelationTypes").First(&library, "id='library'").Error)
	require.Equal(t, types.ShelfCompositionFields(), library.RelationTypes[0].Fields)
	require.NoError(t, migration.Migrate(52), "legacy values alone do not block downgrade")
	require.NoError(t, migration.Migrate(53))
	require.NoError(t, db.Exec(`UPDATE relations SET attributes='{"module_type":"synthetic","cluster_model":"New cluster","container_model":"New container"}' WHERE id='relation'`).Error)
	facts, err := Capture(ctx, db)
	require.NoError(t, err)
	currentRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(currentRoot, "", facts))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, currentRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, currentRoot))
	require.ErrorContains(t, migration.Migrate(52), "shelf composition model data exists")
}
