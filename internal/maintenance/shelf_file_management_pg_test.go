package maintenance_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/application/service"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Medium spec, task 20 / ADR 0024. Synthetic unassigned files and current
// selections survive portable restoration; new data blocks a lossy downgrade.
func TestShelfFileManagementMigrationAndReplica(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(49))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business,credentials) VALUES(1,'File owner','personal','{}');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,shelf_enabled,shelf_config) VALUES('library',1,'File shelf','file-shelf','','','managed',true,'{}');
 INSERT INTO entities(id,space_id,library_id,entity_type,kind,canonical_name,normalized_name,created_at,updated_at) VALUES('file',1,'library','product','document_file','Original file','original file',now(),now());
 INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at) VALUES('batch',1,'library',1,'confirmed','{"type":"web"}','[]',now(),now())`).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	old, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(oldRoot, "", old))
	require.NoError(t, migration.Migrate(50))
	restoredOld := pgtest.Open(t)
	_, err = maintenance.Import(ctx, restoredOld, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, restoredOld, oldRoot))
	require.NoError(t, migration.Migrate(49), "empty file management data permits downgrade")
	require.NoError(t, migration.Migrate(50))
	version := types.ShelfDocumentVersion{ID: "version", SpaceID: 1, LibraryID: "library", FileID: "file", CloudURL: "https://example.invalid/original", Ordinal: 1, VersionLabel: " Customer original ", Stage: "draft", IsCurrent: true, OccurredAt: time.Now(), CreatedAt: time.Now(), Source: types.OperationSource{Type: "web"}, ChangesetID: "batch", ParseStatus: "not_parsed", Conditions: []types.ShelfCondition{}}
	require.NoError(t, db.Create(&version).Error)
	selection := types.ShelfDocumentSelection{ID: "selection", SpaceID: 1, LibraryID: "library", FileID: "file", VersionID: "version", SelectedAt: time.Now()}
	require.NoError(t, db.Create(&selection).Error)
	snapshot, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(root, "", snapshot))
	fresh := pgtest.Open(t)
	_, err = maintenance.Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, fresh, root))
	rows, err := service.NewEntityService(fresh, nil).ShelfDocumentVersions(ctx, "file")
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Nil(t, rows[0].OwnerID)
	require.Equal(t, " Customer original ", rows[0].VersionLabel)
	require.ErrorContains(t, migration.Migrate(49), "shelf file management data exists")
}
