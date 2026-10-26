package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/stretchr/testify/require"
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
	require.NoError(t, os.MkdirAll(filepath.Dir(article), 0o700))
	require.NoError(
		t,
		os.WriteFile(
			article,
			[]byte(
				"---\nid: article\ntitle: Research article\nsource_unverified: true\nst"+
					"arred: false\ncollect_reason: owner reason\n---\nImmutable body",
			),
			0o600,
		),
	)
	require.NoError(t, os.MkdirAll(filepath.Join(uploads, "1/pending"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(uploads, "1/pending/raw.txt"), []byte("Original attachment"), 0o600))
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

	originalRelation, err := json.Marshal(snapshot.Tables["entry_entities"][0])
	require.NoError(t, err)
	for _, test := range []struct {
		name, mutate, restore, expected string
		restoreArgs                     []any
	}{
		{
			name:   "missing_row",
			mutate: `DELETE FROM entry_entities WHERE entry_id='article'`,
			restore: `INSERT INTO entry_entities
SELECT * FROM jsonb_populate_record(NULL::entry_entities,?::jsonb)`,
			expected:    "table entry_entities",
			restoreArgs: []any{string(originalRelation)},
		},
		{
			name:     "changed_description",
			mutate:   `UPDATE entities SET canonical_name='Changed name' WHERE id='entity-a'`,
			restore:  `UPDATE entities SET canonical_name='Company A' WHERE id='entity-a'`,
			expected: "table entities",
		},
		{
			name: "unexpected_row",
			mutate: `INSERT INTO entities(id,space_id,canonical_name,normalized_name,
created_at,updated_at)
VALUES('unexpected',1,'Unexpected','unexpected',now(),now())`,
			restore:  `DELETE FROM entities WHERE id='unexpected'`,
			expected: "table entities",
		},
		{
			name:     "literal_null_changed_to_sql_null",
			mutate:   `UPDATE entries SET structured_summary=NULL WHERE id='pending'`,
			restore:  `UPDATE entries SET structured_summary='null'::jsonb WHERE id='pending'`,
			expected: "table JSONB null types entries",
		},
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
	require.NoError(t, os.WriteFile(article, []byte("tampered original article"), 0o600))
	require.ErrorContains(t, VerifyImportedReplica(ctx, target, root), "vault file checksum mismatch")
}
