package maintenance

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

func TestLoginRemovalPreservesBusinessDataAndImportsLegacyReplica(t *testing.T) {
	ctx := context.Background()
	db := legacyAuthenticationDB(t)
	seed(t, db)
	seedAuthenticationMetadata(t, db)
	before := collectionTableContents(t, db)
	original, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Contains(t, original.Tables["users"][0], "password_hash")
	oldRoot, root := t.TempDir(), t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", original))
	require.NoError(t, WriteSnapshot(root, "", original))
	expected, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)

	migration := collectionMigration(t, db)
	require.NoError(t, migration.Migrate(48))
	require.False(t, db.Migrator().HasTable("auth_tokens"))
	require.False(t, db.Migrator().HasColumn("users", "password_hash"))
	require.NoError(t, ValidateClassification(ctx, db))
	after := collectionTableContents(t, db)
	require.Len(t, after, len(before)-1)
	for table, raw := range before {
		if table == "auth_tokens" {
			continue
		}
		if table == "users" {
			var rows []map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &rows))
			for _, row := range rows {
				delete(row, "password_hash")
			}
			data, err := json.Marshal(rows)
			require.NoError(t, err)
			raw = string(data)
		}
		// Includes unchanged API-key hashes, ChatGPT credentials and grants.
		require.JSONEq(t, raw, after[table], table)
	}
	current, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, current.Tables, "auth_tokens")
	require.NotContains(t, current.Tables["users"][0], "password_hash")
	require.NoError(t, WriteSnapshot(root, "", current))
	actual, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(expected, actual))

	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
	require.False(t, target.Migrator().HasTable("auth_tokens"))
	require.False(t, target.Migrator().HasColumn("users", "password_hash"))
	require.NoError(t, target.Exec("UPDATE entries SET vault_body='tampered' WHERE id='article'").Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, target, oldRoot), "entries")

	require.NoError(t, migration.Migrate(47))
	require.NoError(t, ValidateClassification(ctx, db))
	var count int64
	require.NoError(t, db.Table("auth_tokens").Count(&count).Error)
	require.Zero(t, count)
	var passwordHash string
	require.NoError(t, db.Table("users").Select("password_hash").Where("id='owner'").Scan(&passwordHash).Error)
	require.Empty(t, passwordHash)
	require.NoError(t, migration.Migrate(48))
	require.NoError(t, ValidateClassification(ctx, db))
}
