package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/vaultpath"
)

func TestObsidianConfigurationSurvivesReplicaBackupAndRestoreVerification(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	root := t.TempDir()
	configuration := map[string]string{
		".obsidian/app.json":                  `{"readableLineLength":true}`,
		".obsidian/plugins/example/data.json": `{"readingView":true}`,
		".trash/x.md":                         "# Deleted user note\n",
		".DS_Store":                           "Finder metadata",
		"mine/.trash/x.md":                    "# Deleted personal note\n",
		"reports/.obsidian/app.json":          `{"theme":"obsidian"}`,
		"attachments/._image.png":             "AppleDouble metadata",
	}
	for relative, data := range configuration {
		path := filepath.Join(root, relative)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, []byte(data), 0644))
	}
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	var replica ReplicaManifest
	manifestPath := filepath.Join(root, ".reliquary/manifest.json")
	require.NoError(t, json.Unmarshal(mustRead(t, manifestPath), &replica))
	for _, file := range replica.VaultFiles {
		require.False(t, vaultpath.UserMetadata(file.Path), "user settings are not managed vault data")
	}
	expected, err := BuildManifest(ctx, source, root, "")
	require.NoError(t, err)
	for _, file := range expected.Files["vault"] {
		require.False(t, vaultpath.UserMetadata(file.Path))
	}
	// A legacy replica may have included editor settings in its vault inventory.
	// Its content is now user owned, but unsafe manifest paths still fail below.
	oldApp := configuration[".obsidian/app.json"]
	replica.VaultFiles = append(replica.VaultFiles, File{Path: ".obsidian/app.json", SHA256: sum([]byte(oldApp)), Size: int64(len(oldApp))})
	encoded, err := json.Marshal(replica)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, encoded, 0600))
	configuration[".obsidian/app.json"] = `{"readableLineLength":false,"userChanged":true}`
	require.NoError(t, os.WriteFile(filepath.Join(root, ".obsidian/app.json"), []byte(configuration[".obsidian/app.json"]), 0644))
	fixed := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	for relative := range configuration {
		require.NoError(t, os.Chtimes(filepath.Join(root, relative), fixed, fixed))
	}
	_, err = Load(root)
	require.NoError(t, err)
	verified, err := VerifyReplica(ctx, source, root, "", false)
	require.NoError(t, err)
	require.True(t, verified.OK, "%v", verified.Differences)
	verified, err = VerifyReplica(ctx, source, root, "", true)
	require.NoError(t, err)
	require.True(t, verified.OK)
	require.Empty(t, verified.Repaired)
	actual, err := BuildManifest(ctx, source, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))
	// Physical backup enumeration includes every settings and trash file.
	physical, err := Files(root)
	require.NoError(t, err)
	restoredRoot := t.TempDir()
	for _, file := range physical {
		path := filepath.Join(restoredRoot, file.Path)
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
		require.NoError(t, os.WriteFile(path, mustRead(t, filepath.Join(root, file.Path)), 0644))
	}
	target := pgtest.Open(t)
	_, err = Import(ctx, target, restoredRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, restoredRoot))
	restored, err := BuildManifest(ctx, target, restoredRoot, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, restored))
	for relative, data := range configuration {
		require.Equal(t, data, string(mustRead(t, filepath.Join(root, relative))))
		require.Equal(t, data, string(mustRead(t, filepath.Join(restoredRoot, relative))))
		info, err := os.Stat(filepath.Join(root, relative))
		require.NoError(t, err)
		require.Equal(t, fixed, info.ModTime().UTC(), "verification must not rewrite user configuration")
	}
	// The same filename stays verified inside operational resources and uploads.
	require.NoError(t, os.MkdirAll(filepath.Join(restoredRoot, ".reliquary/resources"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(restoredRoot, ".reliquary/resources/.DS_Store"), []byte("managed original"), 0644))
	changed, err := BuildManifest(ctx, target, restoredRoot, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(restored, changed), "vault unexpected file .reliquary/resources/.DS_Store")
	unsafe := replica
	unsafe.VaultFiles = append(append([]File(nil), replica.VaultFiles...), File{Path: "../.obsidian/app.json"})
	encoded, err = json.Marshal(unsafe)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manifestPath, encoded, 0600))
	_, err = Load(root)
	require.ErrorContains(t, err, "asset path escapes root")
}

func TestBackupManifestIgnoresHistoricalObsidianChecksumsOnlyInVault(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	expected, err := BuildManifest(ctx, db, t.TempDir(), "")
	require.NoError(t, err)
	actual := *expected
	actual.Files = map[string][]File{"vault": append([]File(nil), expected.Files["vault"]...), "uploads": {}}
	expected.Files["vault"] = append(expected.Files["vault"], File{Path: ".obsidian/app.json", SHA256: "before", Size: 1})
	actual.Files["vault"] = append(actual.Files["vault"], File{Path: ".obsidian/app.json", SHA256: "after", Size: 2}, File{Path: ".trash/new.md", SHA256: "trash", Size: 3})
	require.Empty(t, CompareManifest(expected, &actual))
	expected.Files["uploads"] = []File{{Path: ".DS_Store", SHA256: "upload-original", Size: 1}}
	actual.Files["uploads"] = []File{{Path: ".DS_Store", SHA256: "upload-changed", Size: 2}}
	require.Contains(t, CompareManifest(expected, &actual), "uploads file .DS_Store")
}
