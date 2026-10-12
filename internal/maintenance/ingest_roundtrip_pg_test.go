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

func TestIngestMaterialsAndIdentitySurvivePortableReplicaRoundtrip(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	require.NoError(t, source.Exec(`
UPDATE entries SET acquisition='extracted',incomplete=true,external_completion_attempted=true,
 ingest_api_key_id=1,ingest_api_key_name='Collector original name',last_ingest_api_key_id=1,last_ingest_api_key_name='Collector last name',
 missing_materials='[{"type":"image","source_url":"https://example.test/missing.png","reason":"source does not supply image","api_key_id":1,"api_key_name":"Collector last name","submitted_at":"2026-10-12T01:00:00Z"}]',
 ingest_attachments='[{"asset_id":"asset","name":"report.txt","type":"txt","path":"local://1/exports/report.txt","size":18}]',content_type_hint='research',related_article_id='article' WHERE id='pending';
UPDATE operation_logs SET api_key_id=1,api_key_name='Collector original name' WHERE id='op-1';
INSERT INTO ingest_assets(id,space_id,name,file_type,size,path,created_at,referenced_at,entry_id) VALUES('asset',1,'report.txt','txt',18,'local://1/exports/report.txt','2026-10-12T00:00:00Z','2026-10-12T01:00:00Z','pending');
INSERT INTO ingest_discards(space_id,normalized_url,article_id,discarded_at,reason) VALUES(1,'https://example.test/discarded','discarded-article','2026-10-12T02:00:00Z','User discarded duplicate');
`).Error)
	root, uploads := t.TempDir(), t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "1/exports"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "1/exports/report.txt"), []byte("Original attachment"), 0600))
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	loaded, err := Load(root)
	require.NoError(t, err)
	for _, table := range []string{"entries", "operation_logs", "ingest_assets", "ingest_discards"} {
		require.Equal(t, snapshot.Tables[table], loaded.Tables[table], table)
	}
	target := pgtest.Open(t)
	targetUploads := t.TempDir()
	_, err = Import(ctx, target, root, targetUploads, nil)
	require.NoError(t, err)
	restored, err := Capture(ctx, target)
	require.NoError(t, err)
	for table, rows := range snapshot.Tables {
		left, right := comparableCredentialRows(table, rows, restored.Tables[table], snapshot.Required)
		want, _ := json.Marshal(left)
		got, _ := json.Marshal(right)
		require.JSONEq(t, string(want), string(got), table)
	}
	require.Equal(t, "Original attachment", string(mustRead(t, filepath.Join(targetUploads, "1/exports/report.txt"))))
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	require.NoError(t, ValidateClassification(ctx, target))
}
