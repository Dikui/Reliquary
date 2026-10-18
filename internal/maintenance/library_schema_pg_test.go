package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestLibraryUpgradePreservationProjection(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	root := t.TempDir()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(9))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal'); INSERT INTO libraries(id,space_id,name,description,embedding_model_id,summary_model_id) VALUES('old',1,'Library','','embed',''); INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind) VALUES('mine',1,'old','manual','Retained','','mine'),('report',1,'old','manual','Report','','report'); INSERT INTO task_pending_ops(id,task_type,queue,payload,state) VALUES('legacy-library-task','manual:process','default','{"knowledge_id":"mine","library_id":"old"}','pending')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	restoreRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(restoreRoot, "", snapshot))
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(10))
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, migration.Migrate(25))
	require.NoError(t, db.Exec(`UPDATE libraries SET name='Custom media' WHERE system_role='opinions'`).Error)
	current, err = Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	beforeLabels, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(26))
	current, err = Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	afterLabels, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(beforeLabels, afterLabels))
	rows := current.Tables["libraries"]
	names := make(map[string]string, len(rows))
	for _, row := range rows {
		names[stringValue(row["system_role"])] = stringValue(row["name"])
	}
	require.Equal(t, "个人产出", names["mine"])
	require.Equal(t, "Custom media", names["opinions"])
	// A changed new default or lost old value must remain visible to verification.
	require.NoError(t, db.Exec(`UPDATE libraries SET review_criteria='unauthorized' WHERE system_role='mine'`).Error)
	tampered, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(beforeLabels, tampered), "table libraries")
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, restoreRoot))
}

func TestLibraryLabelProjectionRetainsOriginalEvidence(t *testing.T) {
	rows := []map[string]any{{"id": "mine", "system_role": "mine", "name": "我的资料"}}
	canonical, err := json.Marshal(canonicalRows("libraries", rows))
	require.NoError(t, err)
	data, err := json.MarshalIndent(rows, "", "  ")
	require.NoError(t, err)
	data = append(data, '\n')
	expected := &Manifest{SchemaVersion: 25, Tables: map[string]TableManifest{
		"libraries": {Rows: 1, SHA256: sum(canonical), Data: rows},
	}, Files: map[string][]File{"vault": {{Path: ".reliquary/config/libraries.json", SHA256: sum(data), Size: int64(len(data))}}}}
	actual := &Manifest{SchemaVersion: 26}
	projected, _ := projectLibraryLabelsManifest(expected, actual)
	require.Equal(t, "个人产出", projected.Tables["libraries"].Data[0]["name"])
	require.Equal(t, "我的资料", expected.Tables["libraries"].Data[0]["name"])
	require.Equal(t, sum(canonical), expected.Tables["libraries"].SHA256)
	require.Equal(t, sum(data), expected.Files["vault"][0].SHA256)
	require.NotEqual(t, sum(data), projected.Files["vault"][0].SHA256)
	expected.SchemaVersion = 26
	projected, _ = projectLibraryLabelsManifest(expected, actual)
	require.Same(t, expected, projected, "current snapshots must retain user-selected names")
	expected.SchemaVersion = 25
	expected.Files["vault"][0].SHA256 = "altered"
	projected, _ = projectLibraryLabelsManifest(expected, actual)
	require.Equal(t, "altered", projected.Files["vault"][0].SHA256)
	item := expected.Tables["libraries"]
	item.SHA256 = "altered"
	expected.Tables["libraries"] = item
	projected, _ = projectLibraryLabelsManifest(expected, actual)
	require.Same(t, expected, projected, "projection must not replace a mismatched original digest")
}

func TestLibraryDirectoryUpgradeAndReplicaRestore(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(26))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Directories','personal'); INSERT INTO libraries(id,space_id,name,system_role,directory,template,embedding_model_id,summary_model_id) VALUES('personal',1,'个人产出','mine','mine','own','',''),('custom',1,'Custom','','custom-library','own','',''); INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,vault_path,vault_body) VALUES('entry',1,'personal','manual','Personal','mine/entry.md','mine','mine/entry.md','Original body'),('custom-entry',1,'custom','manual','Custom','https://example.com/mine/custom','mine','custom-library/entry.md','Custom body')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	root, restoreRoot := t.TempDir(), t.TempDir()
	for _, destination := range []string{root, restoreRoot} {
		for _, folder := range []string{"mine", "custom-library"} {
			require.NoError(t, os.Mkdir(filepath.Join(destination, folder), 0700))
			require.NoError(t, os.WriteFile(filepath.Join(destination, folder, "entry.md"), []byte("# Original body\n"), 0600))
		}
		require.NoError(t, WriteSnapshot(destination, "", snapshot))
	}
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(27))
	require.NoError(t, os.Rename(filepath.Join(root, "mine"), filepath.Join(root, "personal")))
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	for _, row := range before.Tables["entries"].Data {
		if row["id"] == "entry" {
			require.Equal(t, "mine/entry.md", row["vault_path"])
		}
	}
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, restoreRoot))
	var restoredPath string
	require.NoError(t, restored.Table("entries").Where("id='entry'").Pluck("vault_path", &restoredPath).Error)
	require.Equal(t, "personal/entry.md", restoredPath)
	_, err = os.Stat(filepath.Join(restoreRoot, "mine", "entry.md"))
	require.NoError(t, err, "restore verification retains original vault evidence before runtime directory migration")
	require.NoError(t, db.Exec(`UPDATE entries SET source='changed' WHERE id='entry'`).Error)
	tampered, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, tampered), "table entries")
}

func TestLibraryDirectoryProjectionRetainsOriginalEvidence(t *testing.T) {
	expected := &Manifest{SchemaVersion: 26, Tables: map[string]TableManifest{}, Files: map[string][]File{"vault": {{Path: "mine/entry.md", SHA256: "body-proof", Size: 12}}}}
	for table, rows := range map[string][]map[string]any{
		"libraries": {{"id": "personal", "system_role": "mine", "directory": "mine", "name": "个人产出"}},
		"entries":   {{"id": "entry", "library_id": "personal", "vault_path": "mine/entry.md", "source": "mine/entry.md", "metadata": map[string]any{"directory": "mine"}}},
	} {
		raw, err := json.Marshal(canonicalRows(table, rows))
		require.NoError(t, err)
		expected.Tables[table] = TableManifest{Rows: int64(len(rows)), Data: rows, SHA256: sum(raw)}
	}
	actual := &Manifest{SchemaVersion: 27}
	projected, _ := projectLibraryDirectoriesManifest(expected, actual)
	require.Equal(t, "personal/entry.md", projected.Tables["entries"].Data[0]["vault_path"])
	require.Equal(t, "personal/entry.md", projected.Tables["entries"].Data[0]["source"])
	require.Equal(t, "mine/entry.md", expected.Tables["entries"].Data[0]["vault_path"])
	require.Equal(t, "mine", projected.Tables["entries"].Data[0]["metadata"].(map[string]any)["directory"])
	require.Equal(t, "personal/entry.md", projected.Files["vault"][0].Path)
	require.Equal(t, "body-proof", projected.Files["vault"][0].SHA256)
	require.Equal(t, "mine/entry.md", expected.Files["vault"][0].Path)
	item := expected.Tables["entries"]
	item.SHA256 = "altered"
	expected.Tables["entries"] = item
	projected, _ = projectLibraryDirectoriesManifest(expected, actual)
	require.Same(t, expected, projected, "a mismatched original digest cannot authorize projection")
}
