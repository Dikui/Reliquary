package maintenance_test

import (
	"context"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"reliquary/internal/application/service"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"testing"
)

// Medium spec, task 22: migration and portable restoration preserve legacy
// requirements and single associations, then retain independent lists and links.
func TestShelfRequirementListsMigrationAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(50))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Synthetic owner','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,shelf_enabled) VALUES('shelf',1,'Synthetic shelf','synthetic-shelf','','','managed',true);
 INSERT INTO entities(id,space_id,library_id,kind,canonical_name,normalized_name,created_at,updated_at,attributes) VALUES
 ('req',1,'shelf','document_requirement','参数表','参数表',now(),now(),'{"requirement":{"owner":"Original owner","source_version":"Rev 1","final_due_stage":"late"}}'),
 ('file',1,'shelf','document_file','Synthetic file','synthetic file',now(),now(),'{}'),
 ('model',1,'shelf','model','Synthetic model','synthetic model',now(),now(),'{}');
 INSERT INTO entity_attribute_versions(id,space_id,entity_id,revision,attributes,effective_at,recorded_at) SELECT 'snapshot',space_id,id,revision,attributes,now(),now() FROM entities WHERE id='req';
 INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at) VALUES('batch',1,'shelf',1,'confirmed','{"type":"web"}','[]',now(),now());
 INSERT INTO shelf_document_versions(id,space_id,library_id,file_id,owner_id,requirement_id,cloud_url,ordinal,version_label,stage,is_current,occurred_at,created_at,source,changeset_id,parse_status) VALUES('v1',1,'shelf','file','model','req','https://example.invalid/file',1,'Rev A','final',true,now(),now(),'{}','batch','not_parsed')`).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	old, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(oldRoot, "", old))
	require.NoError(t, migration.Migrate(51))
	upgraded, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"requirement": map[string]any{"list_name": "历史未分清单", "owner": "Original owner", "source_version": "Rev 1", "final_due_stage": "late"}}, upgraded.Tables["entity_attribute_versions"][0]["attributes"])
	versions, err := service.NewEntityService(db, nil).ShelfDocumentVersions(ctx, "file")
	require.NoError(t, err)
	require.Equal(t, []string{"req"}, versions[0].RequirementIDs)
	restored := pgtest.Open(t)
	_, err = maintenance.Import(ctx, restored, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, restored, oldRoot))
	require.NoError(t, migration.Migrate(50), "legacy-only state can downgrade without losing business data")
	require.NoError(t, migration.Migrate(51))
	require.NoError(t, db.Exec(`UPDATE entities SET attributes=jsonb_set(attributes,'{requirement,list_name}','"01"') WHERE id='req';
 INSERT INTO entities(id,space_id,library_id,kind,canonical_name,normalized_name,created_at,updated_at,attributes) VALUES('req2',1,'shelf','document_requirement','参数表','参数表',now(),now(),'{"requirement":{"list_name":"02","final_due_stage":"late"}}');
 UPDATE shelf_document_versions SET requirement_id=NULL,requirement_ids='["req","req2"]' WHERE id='v1'`).Error)
	snapshot, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = maintenance.Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, fresh, root))
	versions, err = service.NewEntityService(fresh, nil).ShelfDocumentVersions(ctx, "file")
	require.NoError(t, err)
	require.Equal(t, []string{"req", "req2"}, versions[0].RequirementIDs)
	require.ErrorContains(t, migration.Migrate(50), "scoped requirement data exists")
}
