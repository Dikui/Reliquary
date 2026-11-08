package maintenance_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/application/service/file"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Medium spec, task 07 / ADR 0024: the real replica/manifest restoration path
// preserves version metadata and every original, including noncurrent versions.
// All snapshots and file stores contain synthetic data in test temp directories.
func TestShelfDocumentsMigrationAndRestoreRetainOriginals(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(45))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business,credentials) VALUES(1,'Synthetic owner','personal','{}');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id) VALUES('library',1,'Synthetic shelf','synthetic-shelf','','');
 INSERT INTO entities(id,space_id,library_id,entity_type,kind,canonical_name,normalized_name,created_at,updated_at) VALUES('entity-a',1,'library','product','model','Synthetic owner','synthetic owner',now(),now())`).Error)
	ctx := types.WithPrincipal(context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1)), types.Principal{Type: types.PrincipalWebUser, ID: "owner"})
	before, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(oldRoot, "", before))
	require.NoError(t, migration.Migrate(46))
	restoredOld := pgtest.Open(t)
	_, err = maintenance.Import(ctx, restoredOld, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, restoredOld, oldRoot))
	require.NoError(t, migration.Migrate(45), "an empty document table can be removed")
	require.NoError(t, migration.Migrate(46))
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed',shelf_enabled=true,shelf_config='{}' WHERE id='library';
 INSERT INTO entities(id,space_id,library_id,entity_type,kind,canonical_name,normalized_name,created_at,updated_at) VALUES('document-file',1,'library','product','document_file','Synthetic file','synthetic file',now(),now());
 INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at) VALUES('document-batch',1,'library',1,'confirmed','{"type":"web"}','[]',now(),now())`).Error)
	uploads, root, restoredUploads := t.TempDir(), t.TempDir(), t.TempDir()
	svc := service.NewEntityService(db, nil)
	svc.SetEntityFileService(file.NewResourceCatalogFileService(file.NewLocalFileService(uploads, ""), service.NewResourceCatalog(apprepo.NewResourceRepository(db))))
	for i, content := range []string{"Synthetic historical original", "Synthetic current original"} {
		attachment, err := svc.UploadEntityAttachment(ctx, "library", "synthetic.pdf", []byte(content))
		require.NoError(t, err)
		ordinal := int64(i + 1)
		row := types.ShelfDocumentVersion{ID: []string{"old-version", "current-version"}[i], SpaceID: 1, LibraryID: "library", FileID: "document-file", OwnerID: func() *string { value := "entity-a"; return &value }(), AttachmentID: &attachment.ID, Ordinal: ordinal, VersionLabel: []string{"Customer version", "2"}[i], Stage: "final", IsCurrent: i == 1, Origin: "Synthetic source", OccurredAt: time.Now(), CreatedAt: time.Now(), Source: types.OperationSource{Type: "web"}, ChangesetID: "document-batch", ParseStatus: "not_parsed", Conditions: []types.ShelfCondition{}}
		require.NoError(t, db.Omit("RequirementIDs", "ParserKey", "ParseAttempt", "ParseError", "FileCategory").Create(&row).Error)
	}
	snapshot, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, maintenance.WriteSnapshot(root, uploads, snapshot))
	expected, err := maintenance.BuildManifest(ctx, db, root, uploads)
	require.NoError(t, err)
	fresh := pgtest.Open(t)
	_, err = maintenance.Import(ctx, fresh, root, restoredUploads, nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, fresh, root))
	actual, err := maintenance.BuildManifest(ctx, fresh, root, restoredUploads)
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(expected, actual))
	restored := service.NewEntityService(fresh, nil)
	restored.SetEntityFileService(file.NewResourceCatalogFileService(file.NewLocalFileService(restoredUploads, ""), service.NewResourceCatalog(apprepo.NewResourceRepository(fresh))))
	versions, err := restored.ShelfDocumentVersions(ctx, "document-file")
	require.NoError(t, err)
	require.Len(t, versions, 2)
	require.True(t, versions[0].IsCurrent)
	require.False(t, versions[1].IsCurrent)
	for i, version := range versions {
		_, reader, err := restored.OpenShelfDocumentVersion(ctx, "document-file", version.ID)
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		require.Equal(t, []string{"Synthetic current original", "Synthetic historical original"}[i], string(content))
	}
	require.ErrorContains(t, migration.Migrate(45), "shelf document data exists", "versions alone prevent a lossy downgrade")
}
