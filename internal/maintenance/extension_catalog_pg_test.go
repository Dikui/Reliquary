package maintenance

import (
	"context"
	"reliquary/internal/testutil/pgtest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCatalogClassificationCoversEveryNonSystemTableAndColumn(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	columns, err := CatalogSchema(ctx, db)
	require.NoError(t, err)
	tables, publicTables := map[string][]string{}, map[string]bool{}
	for _, column := range columns {
		key := column.Schema + "." + column.Table
		tables[key] = append(tables[key], column.Name)
		if column.Schema == "public" {
			publicTables[key] = true
		}
		category, found := CatalogClassification(column)
		require.True(t, found, key+"."+column.Name)
		require.Contains(t, []Category{Header, Sidecar, Regenerated, Configuration}, category)
	}
	rules, err := schemaRules(ctx, db)
	require.NoError(t, err)
	require.Len(t, publicTables, len(rules))
	require.Len(t, tables, len(rules)+38)
	require.Len(t, ExtensionCatalogRegistry, 39, "public.spatial_ref_sys is included in both the existing public list and extension ownership review")
	require.NoError(t, ValidateClassification(ctx, db))
	manifest, err := BuildManifest(ctx, db, t.TempDir(), "")
	require.NoError(t, err)
	require.Len(t, manifest.Tables, len(tables))
	for key, rule := range ExtensionCatalogRegistry {
		name := key
		if strings.HasPrefix(key, "public.") {
			name = strings.TrimPrefix(key, "public.")
		}
		entry, found := manifest.Tables[name]
		require.True(t, found, key)
		require.True(t, entry.Regenerated, key)
		require.Equal(t, rule.DefaultRows, entry.Rows, key)
		require.Equal(t, rule.DefaultSHA256, entry.SHA256, key)
		require.ElementsMatch(t, tables[key], entry.Columns, key)
	}
	t.Logf("reviewed tables=%d public=%d non-public=%d columns=%d extension-owned=%d", len(tables), len(publicTables), len(tables)-len(publicTables), len(columns), len(ExtensionCatalogRegistry))
}

func TestCatalogGuardRejectsUnknownSchemasEmptyTablesAndChangedExtensionColumns(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`CREATE SCHEMA maintenance_unreviewed`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE maintenance_unreviewed.metadata(id text,description text)`).Error)
	require.ErrorContains(t, ValidateClassification(ctx, db), "unclassified database table maintenance_unreviewed.metadata")
	require.NoError(t, db.Exec(`DROP TABLE maintenance_unreviewed.metadata`).Error)
	require.NoError(t, db.Exec(`CREATE TABLE maintenance_unreviewed.empty_metadata()`).Error)
	require.ErrorContains(t, ValidateClassification(ctx, db), "unclassified database table maintenance_unreviewed.empty_metadata")
	require.NoError(t, db.Exec(`DROP TABLE maintenance_unreviewed.empty_metadata`).Error)
	require.NoError(t, db.Exec(`ALTER TABLE topology.topology ADD COLUMN user_metadata text`).Error)
	require.ErrorContains(t, ValidateClassification(ctx, db), "unclassified database column topology.topology.user_metadata")
	require.NoError(t, db.Exec(`ALTER TABLE topology.topology DROP COLUMN user_metadata`).Error)
	require.NoError(t, ValidateClassification(ctx, db))
	require.NoError(t, db.Exec(`ALTER TABLE pgivm.pg_ivm_immv DROP COLUMN lastivmupdate`).Error)
	require.ErrorContains(t, ValidateClassification(ctx, db), "missing classified extension column pgivm.pg_ivm_immv.lastivmupdate")
}

func TestCatalogGuardRejectsAlteredOwnershipAndApplicationMaterializedView(t *testing.T) {
	ctx := context.Background()
	t.Run("extension ownership", func(t *testing.T) {
		db := pgtest.Open(t)
		require.NoError(t, db.Exec(`ALTER EXTENSION postgis_tiger_geocoder DROP TABLE tiger.direction_lookup`).Error)
		require.ErrorContains(t, ValidateClassification(ctx, db), "extension ownership, version or relation kind differs for tiger.direction_lookup")
	})
	t.Run("same-column materialized view", func(t *testing.T) {
		db := pgtest.Open(t)
		require.NoError(t, db.Exec(`DROP TABLE follow_feed_states CASCADE`).Error)
		require.NoError(
			t,
			db.Exec(
				`CREATE MATERIALIZED VIEW follow_feed_states AS SELECT 1::bigint AS space_id,now() AS last_viewed_at`,
			).Error,
		)
		require.ErrorContains(
			t,
			ValidateClassification(ctx, db),
			"unreviewed application relation kind for public.follow_feed_states",
		)
	})
}

func TestNonDefaultExtensionDataStopsMaintenanceWithoutChangingOriginal(t *testing.T) {
	ctx := context.Background()
	for _, test := range []struct{ Name, Table, Change string }{
		{"custom SRS", "public.spatial_ref_sys", `INSERT INTO public.spatial_ref_sys(srid,auth_name,auth_srid,srtext,proj4text) VALUES(998998,'user-specific',998998,'Custom reference definition','+proj=longlat')`},
		{"modified standard SRS", "public.spatial_ref_sys", `UPDATE public.spatial_ref_sys SET srtext='Modified standard reference definition' WHERE srid=4326`},
		{"geocoder setting", "tiger.geocode_settings", `INSERT INTO tiger.geocode_settings(name,setting) VALUES('custom_precision','7')`},
		{"modified seeded dictionary", "tiger.pagc_lex", `UPDATE tiger.pagc_lex SET word='Modified dictionary word' WHERE id=(SELECT min(id) FROM tiger.pagc_lex)`},
		{"materialized view definition", "pgivm.pg_ivm_immv", `INSERT INTO pgivm.pg_ivm_immv(immvrelid,viewdef,ispopulated,lastivmupdate) VALUES('public.entries'::regclass,'SELECT title FROM public.entries',false,'0'::xid8)`},
		{"topology metadata", "topology.topology", `INSERT INTO topology.topology(id,name,srid,precision,hasz,useslargeids) VALUES(999,'custom_topology',4326,0,false,false)`},
		{"tokenizer typmod cache", "paradedb._typmod_cache", `SELECT paradedb._save_typmod(ARRAY['unreviewed-custom-tokenizer'])`},
	} {
		t.Run(test.Name, func(t *testing.T) {
			db := pgtest.Open(t)
			require.NoError(t, db.Exec(test.Change).Error)
			var before, after string
			query := "SELECT COALESCE(string_agg(to_jsonb(t)::text,E'\\n' ORDER BY to_jsonb(t)::text COLLATE \"C\"),'') FROM " + test.Table + " t"
			require.NoError(t, db.Raw(query).Scan(&before).Error)
			_, err := Capture(ctx, db)
			require.ErrorContains(t, err, "non-default extension data in "+test.Table)
			_, err = Scrub(ctx, db)
			require.ErrorContains(t, err, "maintenance stopped to preserve custom data")
			require.NoError(t, db.Raw(query).Scan(&after).Error)
			require.Equal(t, before, after, "failure preserves every original extension row")
		})
	}
}
