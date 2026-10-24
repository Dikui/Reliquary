package maintenance

import (
	"context"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRecoveryCredentialChangesRemainScopedAndPreserveOriginalMetadata(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	ctx := context.Background()
	root := t.TempDir()
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, db.Exec(`UPDATE api_keys SET revoked_at=now(),updated_at=now() WHERE id=1`).Error)
	require.NoError(t, db.Exec(`INSERT INTO api_keys(id,space_id,name,key_hash,api_key) VALUES(2,1,'replacement','replacement-hash','replacement-key')`).Error)
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(expected, actual), "table api_keys")
	options := CompareOptions{AllowReconfigurationAppend: true}
	require.Empty(t, CompareManifestWithSettings(expected, actual, options))
	for _, statement := range []string{
		`UPDATE api_keys SET name='tampered' WHERE id=1`,
		`DELETE FROM api_keys WHERE id=1`,
		`UPDATE entities SET canonical_name='tampered' WHERE id='entity-a'`,
	} {
		t.Run(statement, func(t *testing.T) {
			tx := db.Begin()
			defer tx.Rollback()
			require.NoError(t, tx.Exec(statement).Error)
			bad, err := BuildManifest(ctx, tx, root, "")
			require.NoError(t, err)
			require.NotEmpty(t, CompareManifestWithSettings(expected, bad, options))
		})
	}
	unlisted := *expected
	unlisted.Required = nil
	require.Contains(t, CompareManifestWithSettings(&unlisted, actual, options), "table api_keys")
}

func TestRecoveryLoadsOlderAPIKeyManifestOnlyFromVerifiedOriginalVault(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	root := t.TempDir()
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal'); INSERT INTO api_keys(id,space_id,name,key_hash,api_key) VALUES(1,1,'Caller','original-hash','original-key')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	table := manifest.Tables["api_keys"]
	table.Data = nil
	manifest.Tables["api_keys"] = table
	require.NoError(t, PopulateRecoveryAPIKeyRows(manifest, root))
	require.Len(t, manifest.Tables["api_keys"].Data, 1)
	table.SHA256 = "tampered"
	manifest.Tables["api_keys"] = table
	require.ErrorContains(t, PopulateRecoveryAPIKeyRows(manifest, root), "does not match")
}
