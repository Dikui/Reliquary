package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestIKB07RegistryLibraryConfigurationRoundTrip(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO library_content_types(library_id,name,position,enabled,extract_graph) SELECT id,'项目类型',42,true,false FROM libraries WHERE id='library' ON CONFLICT DO NOTHING`).Error)
	require.NoError(t, db.Exec(`INSERT INTO library_relation_types(library_id,name,position,enabled) SELECT id,'引用',17,true FROM libraries WHERE id='library' ON CONFLICT DO NOTHING`).Error)
	// Product master data and manual exclusions are authoritative export data.
	require.NoError(t, db.Exec(`UPDATE libraries SET entity_mode='managed', entity_schema='{"kinds":[{"key":"model","name":"Model"}],"fields":[]}' WHERE id='library';
	 UPDATE entities SET kind='model',code='MODEL-1',attributes='{"status":"available"}',revision=4,archived_at=now() WHERE id='entity-a';
	 UPDATE entry_entities SET origin='manual',excluded=true,evidence='[{"paragraph":1}]',body_hash='old-body' WHERE entity_id='entity-a';
	 UPDATE library_relation_types SET hierarchical=true,subject_kinds='["model"]',object_kinds='["model"]' WHERE library_id='library' AND name='引用'`).Error)
	ctx := context.Background()
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	registry, err := os.ReadFile(filepath.Join(root, ".reliquary/registry.json"))
	require.NoError(t, err)
	for _, table := range []string{"libraries", "library_content_types", "library_relation_types"} {
		require.Contains(t, string(registry), table)
	}
	fresh := pgtest.Open(t)
	_, err = Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, fresh, root))
	current, err := Capture(ctx, fresh)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	check, err := VerifyReplica(ctx, fresh, root, "", false)
	require.NoError(t, err)
	require.True(t, check.OK, check.Differences)
	require.NoError(t, fresh.Exec(`UPDATE libraries SET review_criteria='changed'`).Error)
	check, err = VerifyReplica(ctx, fresh, root, "", false)
	require.NoError(t, err)
	require.False(t, check.OK)
	path := filepath.Join(root, ".reliquary/data/library_content_types.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, append(data, ' '), 0600))
	_, err = Load(root)
	require.ErrorContains(t, err, "checksum mismatch")
}

func TestIKB07RegistrationDependencies(t *testing.T) {
	PersistentTables["fixture_relation"] = PersistentTable{Columns: Rule{Sidecar: "id entry_id"}, File: "data/fixture_relation.json", After: []string{"entries"}, Since: 13}
	t.Cleanup(func() { delete(PersistentTables, "fixture_relation") })
	order, err := registeredImportOrder([]string{"spaces", "entries"}, map[string][]map[string]any{"fixture_relation": {}})
	require.NoError(t, err)
	require.Equal(t, []string{"spaces", "entries", "fixture_relation"}, order)
	_, err = registeredImportOrder([]string{"spaces"}, map[string][]map[string]any{"fixture_relation": {}})
	require.ErrorContains(t, err, "dependency")
}

func TestIKB07ModernEmptyFieldsSurviveLegacyProjection(t *testing.T) {
	current := &Snapshot{Tables: map[string][]map[string]any{"entries": {{"content_type": "", "source_kind": "mine", "id": "current"}}}}
	projectMarkdownSnapshot(current)
	require.Equal(t, "", current.Tables["entries"][0]["content_type"])
	legacy := &Snapshot{Tables: map[string][]map[string]any{"entries": {{"content_type": "", "source_kind": "mine", "id": "legacy", "processing_mode": "full"}}}}
	projectMarkdownSnapshot(legacy)
	require.Equal(t, "other", legacy.Tables["entries"][0]["content_type"])
	require.NotContains(t, legacy.Tables["entries"][0], "processing_mode")
}

func TestEmptyTableMigrationProjectionRejectsNewRows(t *testing.T) {
	before := &Manifest{SchemaVersion: 27, FormatVersion: FormatVersion, Tables: map[string]TableManifest{}}
	after := &Manifest{SchemaVersion: 28, FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"entry_batch_items": {SHA256: sum([]byte("[]"))},
	}}
	require.Empty(t, CompareManifest(before, after))
	require.NotContains(t, before.Tables, "entry_batch_items", "projection must preserve the original evidence")
	after.Tables["entry_batch_items"] = TableManifest{Rows: 1, SHA256: "unexpected row"}
	require.Contains(t, CompareManifest(before, after), "table entry_batch_items")
	before.SchemaVersion = 28
	require.Contains(t, CompareManifest(before, after), "unexpected table entry_batch_items", "current backups must retain exact table coverage")
	before = &Manifest{SchemaVersion: 31, FormatVersion: FormatVersion, Tables: map[string]TableManifest{}}
	after = &Manifest{SchemaVersion: 32, FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"content_check_settings": {SHA256: sum([]byte("[]"))}, "content_check_runs": {SHA256: sum([]byte("[]"))},
	}}
	require.Empty(t, CompareManifest(before, after))
	require.Empty(t, before.Tables, "projection must preserve the original evidence")
	after.Tables["content_check_runs"] = TableManifest{Rows: 1, SHA256: "unexpected row"}
	require.Contains(t, CompareManifest(before, after), "table content_check_runs")
	before.SchemaVersion = 32
	require.Contains(t, CompareManifest(before, after), "unexpected table content_check_runs")
}
