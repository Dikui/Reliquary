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

// Medium spec, task 04 / ADR 0023: old shelves acquire only empty field
// definitions; new facts survive replicas and prevent a lossy downgrade.
func TestShelfModelMigrationAndReplicaPreserveFacts(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(43))
	seed(t, db)
	schema := types.ProductShelfSchema()
	schema.Fields = []types.EntityFieldDefinition{}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed',shelf_enabled=true,entity_schema=?::jsonb WHERE id='library'`, string(encoded)).Error)
	ctx := context.Background()
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	oldManifest, err := BuildManifest(ctx, db, oldRoot, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(44))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", after))
	current, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, current))
	var saved types.Library
	require.NoError(t, db.First(&saved, "id='library'").Error)
	fields := []types.EntityFieldDefinition{}
	for _, field := range types.ProductShelfFields() {
		if field.Key != "certification" && field.Key != "current_stage" && field.Key != "requirement" {
			fields = append(fields, field)
		}
	}
	require.Equal(t, fields, saved.EntitySchema.Fields)
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, oldRoot))
	require.NoError(t, migration.Migrate(43), "empty definitions may be removed")
	require.NoError(t, migration.Migrate(44))
	require.NoError(t, db.Exec(`UPDATE entities SET kind='model',attributes='{"sale_statuses":[{"region":"synthetic","status":"GA","raw":" GA ","conditions":[]}],"combination_rules":[]}' WHERE id='entity-a';
 INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,attributes,effective_at,recorded_at)
 SELECT 'shelf-version',space_id,id,revision,attributes,now(),now() FROM entities WHERE id='entity-a'`).Error)
	facts, err := Capture(ctx, db)
	require.NoError(t, err)
	root = t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", facts))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	actual, err := Capture(ctx, fresh)
	require.NoError(t, err)
	require.Equal(t, facts.Tables["entities"], actual.Tables["entities"])
	require.Equal(t, facts.Tables["entity_attribute_versions"], actual.Tables["entity_attribute_versions"])
	require.ErrorContains(t, migration.Migrate(43), "shelf model data exists")
}
