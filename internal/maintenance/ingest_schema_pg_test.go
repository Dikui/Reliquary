package maintenance

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestIngestContractMigrationUpDownUpAndPreservation(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Down())
	require.NoError(t, migration.Up())
	require.NoError(t, migration.Migrate(10))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal'); INSERT INTO libraries(id,space_id,name,directory,template,description,embedding_model_id,summary_model_id) VALUES('own',1,'Own','own','own','','',''); INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,vault_body,content_hash,incomplete,missing_materials) VALUES('preserved',1,'own','manual','Original','','mine','Exact original Markdown','preserved-hash',true,'[{"type":"image","source_url":"https://example.org/chart.png","reason":"unavailable"}]')`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	beforeManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Up())
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", after))
	afterManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(beforeManifest, afterManifest))
	// Every preexisting table, row and column remains exact, including nonregenerable data.
	for table, rows := range before.Tables {
		if table == "schema_migrations" {
			continue
		}
		actual := preservedBeforeVectorDefaults(t, table, rows, after.Tables[table])
		if table == "libraries" {
			for _, row := range actual {
				require.Equal(t, json.Number("1"), row["color"])
				delete(row, "color")
				require.Empty(t, row["prompt_overrides"])
				delete(row, "prompt_overrides")
				require.Equal(t, "standard", row["template"])
				row["template"] = "own" // Migration 34 replaces this fixture's old marker.
			}
		}
		if table == "entries" {
			for _, row := range actual {
				require.Equal(t, "", row["source_ref"])
				require.Equal(t, "", row["identifier"])
				require.Nil(t, row["graph"])
				delete(row, "source_ref")
				delete(row, "source_body_hash")
				delete(row, "identifier")
				delete(row, "graph")
			}
		}
		require.Equal(t, rows, actual, table)
	}
	require.NoError(t, db.Exec(`UPDATE entries SET graph=true WHERE id='preserved'`).Error)
	tampered, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", tampered))
	manifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(beforeManifest, manifest))
}
