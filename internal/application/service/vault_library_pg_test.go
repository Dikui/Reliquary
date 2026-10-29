package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/maintenance"
	"reliquary/internal/types"
)

func TestVaultLibraryDirectoryMigrationPreservesFilesAndResumes(t *testing.T) {
	h := newVaultDocumentHarness(t, 0)
	var library types.Library
	require.NoError(t, h.db.Where("system_role=?", "official").First(&library).Error)
	require.NoError(t, h.db.Create(&types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: library.ID, Title: "Archived", Type: types.EntryTypeManual, SourceKind: "article", VaultPath: "official/original.md"}).Error)
	original := []byte("---\nlibrary: 官方信息\n---\n\nOriginal body and ../attachments/example.png\n")
	h.write(t, "institutional/original.md", string(original))
	h.write(t, "institutional/reading-notes.txt", "Preserve directory contents")
	h.write(t, "mine/not-imported.md", "No archive record; leave untouched")
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, h.db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	// Offline migrations reconnect before querying a changed table shape.
	pool, err := h.db.DB()
	require.NoError(t, err)
	pool.SetMaxIdleConns(0)
	require.NoError(t, migration.Migrate(26))
	snapshot, err := maintenance.Capture(h.ctx, h.db)
	require.NoError(t, err)
	require.NoError(t, maintenance.WriteSnapshot(h.root, "", snapshot))
	require.NoError(t, migration.Migrate(27))
	require.NoError(t, ensureVaultDirectories(h.db, h.root))
	actual, err := os.ReadFile(filepath.Join(h.root, "official/original.md"))
	require.NoError(t, err)
	require.Equal(t, original, actual)
	require.FileExists(t, filepath.Join(h.root, "official/reading-notes.txt"))
	require.NoDirExists(t, filepath.Join(h.root, "institutional"))
	require.FileExists(t, filepath.Join(h.root, "mine/not-imported.md"))
	require.NoError(t, ensureVaultDirectories(h.db, h.root))
	require.NoError(t, maintenance.VerifyImportedReplica(h.ctx, h.db, h.root))
}

func TestVaultLibraryDirectoryMigrationRejectsConflictBeforeMoving(t *testing.T) {
	h := newVaultDocumentHarness(t, 0)
	for _, role := range []string{"official", "personal"} {
		var library types.Library
		require.NoError(t, h.db.Where("system_role=?", role).First(&library).Error)
		require.NoError(t, h.db.Create(&types.Entry{ID: uuid.NewString(), SpaceID: 1, LibraryID: library.ID, Title: "Archived", Type: types.EntryTypeManual, SourceKind: "article", VaultPath: role + "/original.md"}).Error)
	}
	h.write(t, "institutional/original.md", "Original official file")
	h.write(t, "mine/original.md", "Original personal file")
	h.write(t, "personal/existing.md", "Keep this file")
	err := ensureVaultDirectories(h.db, h.root)
	require.ErrorContains(t, err, "migration conflict")
	require.FileExists(t, filepath.Join(h.root, "institutional/original.md"))
	require.FileExists(t, filepath.Join(h.root, "mine/original.md"))
	content, err := os.ReadFile(filepath.Join(h.root, "personal/existing.md"))
	require.NoError(t, err)
	require.Equal(t, "Keep this file", string(content))
}
