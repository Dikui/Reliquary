package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestManagedEvolutionMigrationPreservesSnapshots(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	defer migration.Close()
	require.NoError(t, migration.Migrate(37))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Migration fixture','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,entity_schema) VALUES('products',1,'Products','products','','','managed','{"kinds":[{"key":"model","name":"Model"}],"fields":[{"key":"status","name":"Status","type":"text"}]}');
 INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,kind,attributes,revision,created_at,updated_at) VALUES('product',1,'products','Example','example','product','model','{"status":"available"}',3,'2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	previous, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Up())
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Len(t, after.Tables["entity_attribute_versions"], 1)
	require.Equal(t, after.Tables["entities"][0]["attributes"], after.Tables["entity_attribute_versions"][0]["attributes"])
	upgraded := t.TempDir()
	require.NoError(t, WriteSnapshot(upgraded, "", after))
	current, err := BuildManifest(ctx, db, upgraded, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(previous, current))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	var count int64
	require.NoError(t, restored.Table("entity_attribute_versions").Count(&count).Error)
	require.EqualValues(t, 1, count)
}

// ADR 0022: matching choices are durable product data; older snapshots receive
// only the empty default, and current snapshots must detect lost choices.
func TestEntityAutoMatchMigrationPreservesAndRestoresChoices(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(38))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Auto match fixture','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id) VALUES('products',1,'Products','products','','');
 INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,description,created_at,updated_at) VALUES('product',1,'products','Example','example','product','Preserved description','2026-10-01T00:00:00Z','2026-10-01T00:00:00Z')`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, before.Tables["entities"][0], "auto_match")
	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	previous, err := BuildManifest(ctx, db, oldRoot, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(39))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, after.Tables["entities"][0]["auto_match"])
	newRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(newRoot, "", after))
	current, err := BuildManifest(ctx, db, newRoot, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(previous, current))
	require.NotContains(t, before.Tables["entities"][0], "auto_match", "projection does not mutate source evidence")
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, oldRoot))
	var value string
	require.NoError(t, restored.Raw("SELECT auto_match::text FROM entities WHERE id='product'").Scan(&value).Error)
	require.JSONEq(t, `{}`, value)
	require.NoError(t, db.Exec(`UPDATE entities SET auto_match='{"name":false,"code":true}' WHERE id='product'`).Error)
	changed, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(newRoot, "", changed))
	changedManifest, err := BuildManifest(ctx, db, newRoot, "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(previous, changedManifest), "nondefault choices cannot disappear in upgrade projection")
	_, err = Import(ctx, restored, newRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, newRoot))
	require.NoError(t, restored.Raw("SELECT auto_match::text FROM entities WHERE id='product'").Scan(&value).Error)
	require.JSONEq(t, `{"name":false,"code":true}`, value)

	require.ErrorContains(t, migration.Migrate(38), "auto_match contains saved choices")
	require.NoError(t, db.Raw("SELECT auto_match::text FROM entities WHERE id='product'").Scan(&value).Error)
	require.JSONEq(t, `{"name":false,"code":true}`, value, "failed downgrade retains saved choices")
	require.NoError(t, migration.Force(39))
	require.NoError(t, db.Exec("UPDATE entities SET auto_match='{}'").Error)
	require.NoError(t, migration.Migrate(38))
	require.NoError(t, migration.Migrate(39))
	reset, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, map[string]any{}, reset.Tables["entities"][0]["auto_match"])
	delete(reset.Tables["entities"][0], "auto_match")
	require.Equal(t, before.Tables["entities"], reset.Tables["entities"], "up/down/up retains original entity data")
}

// Regression: relation validity is projected from the last known state, and
// migration 40 never discards existing rows to resolve case-insensitive codes.
func TestManagedRelationHistoryMigrationPreservesData(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(39))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Relation history fixture','personal');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode) VALUES('products',1,'Products','products','','','managed');
 INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,code,created_at,updated_at) VALUES('one',1,'products','One','one','product','A100',now(),now()),('two',1,'products','Two','two','product','a100',now(),now());
 INSERT INTO library_relation_types(library_id,name,position,enabled,hierarchical) VALUES('products','line',0,true,true);
 INSERT INTO relations(id,space_id,library_id,subject_id,relation_type,object_id,version,attributes,created_at,updated_at) VALUES('edge',1,'products','one','line','two',2,'{"note":"kept"}','2026-10-01T00:00:00Z','2026-10-02T00:00:00Z')`).Error)
	require.ErrorContains(t, migration.Migrate(40), "case-insensitive product codes conflict")
	var count int64
	require.NoError(t, db.Table("entities").Count(&count).Error)
	require.EqualValues(t, 2, count)
	require.NoError(t, migration.Force(39))
	require.NoError(t, db.Exec("UPDATE entities SET code='A200' WHERE id='two'").Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	previous, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(40))
	require.ErrorContains(t, db.Exec("INSERT INTO entities(id,space_id,library_id,canonical_name,normalized_name,entity_type,code,created_at,updated_at) VALUES('duplicate',1,'products','Duplicate','duplicate','product','a100',now(),now())").Error, "duplicate key")
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Len(t, after.Tables["relations"], 1)
	require.Equal(t, after.Tables["relations"][0]["updated_at"], after.Tables["relations"][0]["valid_from"])
	require.Nil(t, after.Tables["relations"][0]["valid_to"])
	upgraded := t.TempDir()
	require.NoError(t, WriteSnapshot(upgraded, "", after))
	current, err := BuildManifest(ctx, db, upgraded, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(previous, current))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	require.NoError(t, migration.Migrate(39))
	require.NoError(t, migration.Migrate(40))
	reset, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, after.Tables, reset.Tables)
}
