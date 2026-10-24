package maintenance

import (
	"context"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryIntakeMigrationPreservesHistoricalRows(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(11))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Preserved','personal');
 INSERT INTO libraries(id,space_id,name,directory,template,description,embedding_model_id,summary_model_id,source_evidence_rule) VALUES('owner',1,'Owner','owner','external_timely','','','','link_required');
 INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,vault_body) VALUES('retained',1,'owner','manual','Original','https://example.org/original','article','Original body');
 INSERT INTO ingest_discards(space_id,normalized_url,article_id,reason) VALUES(1,'https://example.org/one','retained','Original reason'),(1,'https://example.org/two','removed','Removed owner reason');`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	bm, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Up())
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", after))
	am, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(bm, am))
	for table, rows := range before.Tables {
		if table == "schema_migrations" || table == "library_relation_types" {
			continue
		}
		for _, row := range rows {
			if table == "entries" {
				delete(row, "graph")
			}
			if table == "library_content_types" {
				delete(row, "extract_graph")
			}
		}
		actual := preservedBeforeVectorDefaults(t, table, rows, after.Tables[table])
		if table == "ingest_discards" {
			for _, row := range actual {
				require.Len(t, row["fingerprint"], 64)
				if row["article_id"] == "retained" {
					require.Equal(t, "owner", row["library_id"])
				} else {
					require.Equal(t, "legacy-space:1", row["library_id"])
				}
				delete(row, "library_id")
				delete(row, "fingerprint")
			}
		}
		require.Equal(t, rows, actual, table)
	}
	// New fields remain strict preservation data rather than comparison exceptions.
	require.NoError(t, db.Exec(`UPDATE ingest_discards SET library_id='wrong' WHERE entry_id='retained'`).Error)
	changed, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", changed))
	cm, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(bm, cm))
	// Historical snapshots can be restored into the current schema and verified.
	target := pgtest.Open(t)
	restoreRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(restoreRoot, "", before))
	_, err = Import(ctx, target, restoreRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, restoreRoot))
}
