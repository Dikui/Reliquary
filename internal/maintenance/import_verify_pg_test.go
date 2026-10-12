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

func TestImportedReplicaVerificationPreservesCredentialsNullTypesAndMaterializedArticle(t *testing.T) {
	ctx := context.Background()
	source := pgtest.Open(t)
	seed(t, source)
	require.NoError(t, source.Exec(`UPDATE entries SET vault_path='articles/materialized.md' WHERE id='article'`).Error)
	require.NoError(t, source.Exec(`UPDATE entries SET structured_summary='null'::jsonb WHERE id='pending'`).Error)
	require.NoError(t, source.Exec(`INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('literal-null',1,'owner','create_topic','topic','null'::jsonb,'null'::jsonb)`).Error)
	root, uploads := t.TempDir(), t.TempDir()
	article := filepath.Join(root, "articles/materialized.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(article), 0700))
	require.NoError(t, os.WriteFile(article, []byte("---\nid: article\ntitle: Research article\ncontent_type: research\nsource_unverified: true\nstarred: false\ncollect_reason: owner reason\n---\nImmutable body"), 0600))
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "1/pending"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "1/pending/raw.txt"), []byte("Original attachment"), 0600))
	snapshot, err := Capture(ctx, source)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, uploads, snapshot))
	before, err := Files(root)
	require.NoError(t, err)
	for _, row := range snapshot.Tables["entries"] {
		if row["id"] == "article" {
			require.NotContains(t, row, "vault_body", "published body belongs to the article file")
		}
	}
	target := pgtest.Open(t)
	imported, err := Import(ctx, target, root, t.TempDir(), func(row map[string]any) error {
		if row["id"] == "article" {
			row["vault_body"] = "Immutable body"
		}
		return nil
	})
	require.NoError(t, err)
	for _, row := range imported.Tables["models"] {
		if row["id"] == "chat" {
			require.Equal(t, "needs_configuration", row["status"])
			require.Equal(t, "active", row["parameters"].(map[string]any)["extra_config"].(map[string]any)["maintenance_original_status"])
		}
	}
	var body string
	require.NoError(t, target.Raw(`SELECT vault_body FROM entries WHERE id='article'`).Scan(&body).Error)
	require.Equal(t, "Immutable body", body)
	require.NoError(t, VerifyImportedReplica(ctx, target, root))
	after, err := Files(root)
	require.NoError(t, err)
	require.Equal(t, before, after, "verification must not flush or alter original files")

	originalRelation, err := json.Marshal(snapshot.Tables["entry_topics"][0])
	require.NoError(t, err)
	for _, test := range []struct {
		name, mutate, restore, expected string
		restoreArgs                     []any
	}{
		{"missing_row", `DELETE FROM entry_topics WHERE entry_id='article'`, `INSERT INTO entry_topics SELECT * FROM jsonb_populate_record(NULL::entry_topics,?::jsonb)`, "table entry_topics", []any{string(originalRelation)}},
		{"changed_description", `UPDATE topics SET description='Changed user description' WHERE id='topic'`, `UPDATE topics SET description='User topic description' WHERE id='topic'`, "table topics", nil},
		{"unexpected_row", `INSERT INTO topics(id,space_id,name,description) VALUES('unexpected',1,'Unexpected','Must not be absorbed into a fresh baseline')`, `DELETE FROM topics WHERE id='unexpected'`, "table topics", nil},
		{"literal_null_changed_to_sql_null", `UPDATE entries SET structured_summary=NULL WHERE id='pending'`, `UPDATE entries SET structured_summary='null'::jsonb WHERE id='pending'`, "table JSONB null types entries", nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			require.NoError(t, target.Exec(test.mutate).Error)
			err := VerifyImportedReplica(ctx, target, root)
			require.ErrorContains(t, err, test.expected)
			require.ErrorContains(t, err, "original files retained")
			files, err := Files(root)
			require.NoError(t, err)
			require.Equal(t, before, files, "a failed first verification must preserve the original vault")
			require.NoError(t, target.Exec(test.restore, test.restoreArgs...).Error)
			require.NoError(t, VerifyImportedReplica(ctx, target, root))
		})
	}
	// Load checks the original article bytes before comparing any database row.
	require.NoError(t, os.WriteFile(article, []byte("tampered original article"), 0600))
	require.ErrorContains(t, VerifyImportedReplica(ctx, target, root), "vault file checksum mismatch")
}
