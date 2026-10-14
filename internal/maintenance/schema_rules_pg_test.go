package maintenance

import (
	"context"
	"fmt"
	"github.com/golang-migrate/migrate/v4"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/testutil/pgtest"
)

func TestLegacyPreservationRulesMatchVersionWithoutMigratingSchema(t *testing.T) {
	for version := uint(0); version <= 15; version++ {
		if version == 13 {
			continue
		}
		t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
			db := pgtest.Open(t)
			directory, err := filepath.Abs("../../migrations/versioned")
			require.NoError(t, err)
			migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
			require.NoError(t, err)
			t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
			if version != 15 {
				require.NoError(t, migration.Migrate(version))
			}
			before, err := CatalogSchema(context.Background(), db)
			require.NoError(t, err)
			require.NoError(t, WithCurrentNames(db, func(tx *gorm.DB) error {
				rules, err := schemaRules(context.Background(), tx)
				require.NoError(t, err)
				if version < 5 {
					require.Contains(t, rules, "data_sources")
					require.Contains(t, rules, "sync_logs")
				} else {
					require.NotContains(t, rules, "data_sources")
					require.NotContains(t, rules, "sync_logs")
				}
				if version < 8 {
					require.Contains(t, rules["entries"].Columns(), "processing_mode")
				} else {
					require.NotContains(t, rules["entries"].Columns(), "processing_mode")
				}
				if version < 9 {
					require.Contains(t, rules, "task_jobs")
					require.NotContains(t, rules, "task_pending_ops")
				} else {
					require.Contains(t, rules, "task_pending_ops")
					require.NotContains(t, rules, "task_jobs")
				}
				if version < 4 {
					require.NotContains(t, rules, "ingest_assets")
					require.NotContains(t, rules, "ingest_discards")
					require.NotContains(t, rules["entries"].Columns(), "acquisition")
					require.NotContains(t, rules["operation_logs"].Columns(), "api_key_id")
				}
				require.NoError(t, ValidateClassification(context.Background(), tx))
				snapshot, err := Capture(context.Background(), tx)
				require.NoError(t, err)
				if version < 4 {
					require.NotContains(t, snapshot.Tables, "ingest_assets")
				}
				manifest, err := BuildManifest(context.Background(), tx, t.TempDir(), "")
				require.NoError(t, err)
				if version < 4 {
					require.NotContains(t, manifest.Tables["entries"].Columns, "acquisition")
					require.NotContains(t, manifest.Tables["operation_logs"].Columns, "api_key_name")
				}
				return nil
			}))
			after, err := CatalogSchema(context.Background(), db)
			require.NoError(t, err)
			require.Equal(t, before, after)
			actual, dirty, err := migration.Version()
			require.NoError(t, err)
			require.Equal(t, version, actual)
			require.False(t, dirty)
		})
	}
}

func TestLegacyRulesRejectUnexpectedFutureColumn(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(3))
	require.NoError(t, db.Exec("ALTER TABLE operation_logs ADD COLUMN api_key_name text").Error)
	require.ErrorContains(t, classifyHistoricalSchema(context.Background(), db), "unclassified database column operation_logs.api_key_name")
}
