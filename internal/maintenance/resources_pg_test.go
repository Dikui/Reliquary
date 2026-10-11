package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestVaultOnlyRestoreDistinguishesCurrentUploadsFromHistoricalArchive(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	root, uploads, restoredUploads := t.TempDir(), t.TempDir(), t.TempDir()
	current := "1/pending/raw.txt"
	historical := "1/pending/obsolete-raw.md"
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "1/pending"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, current), []byte("Original current attachment"), 0600))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, historical), []byte("Historical original retained after processing"), 0600))
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	require.NoError(t, os.Remove(filepath.Join(uploads, historical)))
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	loaded, err := Load(root)
	require.NoError(t, err)
	require.NotNil(t, loaded.ActiveUploads)
	require.Len(t, *loaded.ActiveUploads, 1)
	require.Equal(t, current, (*loaded.ActiveUploads)[0].Path)
	expected, err := BuildManifest(ctx, source, root, uploads)
	require.NoError(t, err)
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, restoredUploads, nil)
	require.NoError(t, err)
	require.Equal(t, []byte("Original current attachment"), mustRead(t, filepath.Join(restoredUploads, current)))
	_, err = os.Stat(filepath.Join(restoredUploads, historical))
	require.True(t, os.IsNotExist(err), "archived historical files are not current uploads")
	require.Equal(t, []byte("Historical original retained after processing"), mustRead(t, filepath.Join(root, ".reliquary/resources", historical)))
	actual, err := BuildManifest(ctx, target, root, restoredUploads)
	require.NoError(t, err)
	require.Equal(t, expected.Files["uploads"], actual.Files["uploads"])
	require.Empty(t, CompareManifest(expected, actual), "all preserved tables and both file groups remain exact")
	verified, err := VerifyReplica(ctx, target, root, restoredUploads, false)
	require.NoError(t, err)
	// Credential pause markers make the sidecar stale until the usual flush;
	// the active-upload check itself must already match the original inventory.
	require.NotContains(t, verified.Differences, "active upload inventory differs from current uploads")
	// Removing the final active file produces an explicit empty inventory, while
	// both historical archive copies remain available without resurrection.
	require.NoError(t, os.Remove(filepath.Join(uploads, current)))
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	loaded, err = Load(root)
	require.NoError(t, err)
	require.NotNil(t, loaded.ActiveUploads)
	require.Empty(t, *loaded.ActiveUploads)
	manifestBytes := mustRead(t, filepath.Join(root, ".reliquary/manifest.json"))
	require.Contains(t, string(manifestBytes), `"active_uploads": []`)
	emptyDestination := t.TempDir()
	require.NoError(t, RestoreResources(root, emptyDestination, loaded.ActiveUploads))
	emptyFiles, err := Files(emptyDestination)
	require.NoError(t, err)
	require.Empty(t, emptyFiles)
	legacyDestination := t.TempDir()
	require.NoError(t, RestoreResources(root, legacyDestination, nil))
	legacyFiles, err := Files(legacyDestination)
	require.NoError(t, err)
	require.Len(t, legacyFiles, 2, "legacy replicas without the inventory retain their former all-archive behavior")
}

func TestTamperedActiveUploadInventoryFailsBeforeDatabaseImport(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	root, uploads := t.TempDir(), t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "current.txt"), []byte("Active upload content"), 0600))
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	path := filepath.Join(root, ".reliquary/manifest.json")
	original := mustRead(t, path)
	target := pgtest.Open(t)
	for _, test := range []struct {
		Name   string
		Change func(*ReplicaManifest)
	}{
		{"wrong hash", func(manifest *ReplicaManifest) { (*manifest.ActiveUploads)[0].SHA256 = "modified" }},
		{"wrong size", func(manifest *ReplicaManifest) { (*manifest.ActiveUploads)[0].Size++ }},
		{"missing archive", func(manifest *ReplicaManifest) { (*manifest.ActiveUploads)[0].Path = "not-archived.txt" }},
		{"unsafe path", func(manifest *ReplicaManifest) { (*manifest.ActiveUploads)[0].Path = "../outside.txt" }},
		{"duplicate entry", func(manifest *ReplicaManifest) {
			*manifest.ActiveUploads = append(*manifest.ActiveUploads, (*manifest.ActiveUploads)[0])
		}},
	} {
		t.Run(test.Name, func(t *testing.T) {
			var manifest ReplicaManifest
			require.NoError(t, json.Unmarshal(original, &manifest))
			test.Change(&manifest)
			encoded, err := json.Marshal(manifest)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, encoded, 0600))
			_, err = Import(ctx, target, root, t.TempDir(), nil)
			require.Error(t, err)
			var rows int64
			require.NoError(t, target.Table("spaces").Count(&rows).Error)
			require.Zero(t, rows, "invalid inventory is rejected before any database transaction writes")
		})
	}
	var nullInventory map[string]any
	require.NoError(t, json.Unmarshal(original, &nullInventory))
	nullInventory["active_uploads"] = nil
	encoded, err := json.Marshal(nullInventory)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, encoded, 0600))
	_, err = Load(root)
	require.ErrorContains(t, err, "must be an array")
}
