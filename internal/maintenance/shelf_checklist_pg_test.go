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

// Medium spec, task 08 / ADR 0024: old shelves gain empty definitions; list
// metadata, applicability and stages survive replicas and block lossy downgrade.
func TestShelfChecklistMigrationAndReplicaPreserveRequirements(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(46))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Synthetic owner','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,shelf_enabled)
 VALUES('shelf',1,'Synthetic shelf','synthetic-shelf','','','managed',true);
 INSERT INTO library_relation_types(library_id,name,position,enabled,subject_kinds,object_kinds,fields)
 VALUES('shelf','applies_to',0,true,'["document_requirement"]','["product_line"]','[]')`).Error)
	schema := types.ProductShelfSchema()
	schema.Fields = nil
	for _, f := range types.ProductShelfFields() {
		if f.Key != "requirement" && f.Key != "current_stage" {
			schema.Fields = append(schema.Fields, f)
		}
	}
	encoded, err := json.Marshal(schema)
	require.NoError(t, err)
	require.NoError(t, db.Exec("UPDATE libraries SET entity_schema=?::jsonb WHERE id='shelf'", string(encoded)).Error)
	ctx := context.Background()
	old, err := Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", old))
	oldManifest, err := BuildManifest(ctx, db, oldRoot, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(47))
	upgraded, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", upgraded))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, manifest))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, oldRoot))
	require.NoError(t, migration.Migrate(46), "empty definitions can be removed")
	require.NoError(t, migration.Migrate(47))
	require.NoError(t, db.Exec(`INSERT INTO entities(id,space_id,library_id,kind,canonical_name,normalized_name,created_at,updated_at,attributes)
 VALUES('requirement',1,'shelf','document_requirement','Synthetic requirement','synthetic requirement',now(),now(),'{"requirement":{"final_due_stage":"custom","source_version":"Rev 2"}}');
 INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,attributes,effective_at,recorded_at)
 SELECT 'requirement-snapshot',space_id,id,revision,attributes,now(),now() FROM entities WHERE id='requirement';
 INSERT INTO relations(id,space_id,library_id,subject_id,object_id,relation_type,version,attributes)
 VALUES('binding',1,'shelf','requirement','requirement','applies_to',1,'{"module_types":["custom"]}')`).Error)
	saved, err := Capture(ctx, db)
	require.NoError(t, err)
	root = t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", saved))
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	actual, err := Capture(ctx, fresh)
	require.NoError(t, err)
	saved.Tables["entities"][0]["attributes"].(map[string]any)["requirement"].(map[string]any)["list_name"] = "历史未分清单"
	require.Equal(t, saved.Tables["entities"], actual.Tables["entities"])
	require.Equal(t, saved.Tables["relations"], actual.Tables["relations"])
	require.ErrorContains(t, migration.Migrate(46), "shelf checklist data exists")
}
