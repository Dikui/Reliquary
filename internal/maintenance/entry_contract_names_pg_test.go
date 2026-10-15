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

func TestEntryContractV5ReplicaImportsNewColumnsAndOwnedJSON(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, source.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(22))
	seed(t, source)
	require.NoError(t, source.Exec(`UPDATE entries SET related_article_id='article',custom_metadata='{"article_id":"opaque"}' WHERE id='pending';
 INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,parse_status,metadata) VALUES('legacy-report',1,'library','manual','Legacy report','','report','completed','{}');
 UPDATE entries SET metadata='{"related_article_ids":["article"],"request_id":"legacy-report","custom":{"knowledge_id":"opaque"}}' WHERE source_kind='report';
 UPDATE task_pending_ops SET payload='{"space_id":1,"knowledge_id":"article"}';
 INSERT INTO ingest_discards(space_id,library_id,fingerprint,normalized_url,article_id,reason) VALUES(1,'library',repeat('a',64),'https://example.test/discard','article','Retain reason');`).Error)
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	path := filepath.Join(root, ".reliquary/manifest.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var manifest ReplicaManifest
	require.NoError(t, json.Unmarshal(data, &manifest))
	manifest.FormatVersion = 5
	data, err = json.Marshal(manifest)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, data, 0600))
	loaded, err := Load(root)
	require.NoError(t, err)
	require.True(t, loaded.UsesLegacyEntryNames())
	require.Contains(t, loaded.Tables["ingest_discards"][0], "entry_id")
	require.NotContains(t, loaded.Tables["ingest_discards"][0], "article_id")
	target := pgtest.Open(t)
	_, err = Import(ctx, target, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	var related, metadata, payload string
	require.NoError(t, target.Raw("SELECT related_entry_id,custom_metadata::text FROM entries WHERE id='pending'").Row().Scan(&related, &metadata))
	require.Equal(t, "article", related)
	require.JSONEq(t, `{"article_id":"opaque"}`, metadata)
	require.NoError(t, target.Raw("SELECT metadata::text FROM entries WHERE id='legacy-report'").Scan(&metadata).Error)
	require.JSONEq(t, `{"related_entry_ids":["article"],"request_id":"legacy-report","custom":{"knowledge_id":"opaque"}}`, metadata)
	require.NoError(t, target.Raw("SELECT payload::text FROM task_pending_ops LIMIT 1").Scan(&payload).Error)
	require.Contains(t, payload, `"entry_id"`)
	require.NotContains(t, payload, `"knowledge_id"`)
	current, err := Capture(ctx, target)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", current))
	verification, err := VerifyReplica(ctx, target, root, "", false)
	require.NoError(t, err)
	require.True(t, verification.OK, verification.Differences)
	// Current-format snapshots cannot silently accept a retired envelope column.
	require.Error(t, validateEntryReplicaColumns("entries", []map[string]any{{"related_article_id": "x"}}, 6, 23))
	require.Error(t, validateEntryReplicaColumns("ingest_discards", []map[string]any{{"article_id": "x", "entry_id": "y"}}, 5, 22))
}
