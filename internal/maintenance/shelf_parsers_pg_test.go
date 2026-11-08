package maintenance_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/application/service"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

// Medium spec, task 09 / ADR 0024: old versions receive empty parser state;
// portable restoration preserves parser settings and failed attempts, and new
// parser business data prevents a lossy downgrade. All data is synthetic.
func TestShelfParsersMigrationAndReplicaPreserveState(t *testing.T) {
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(47))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business,credentials) VALUES(1,'Parser owner','personal','{}');
 INSERT INTO libraries(id,space_id,name,directory,embedding_model_id,summary_model_id,entity_mode,shelf_enabled,shelf_config) VALUES('library',1,'Parser shelf','parser-shelf','','','managed',true,'{}');
 INSERT INTO entities(id,space_id,library_id,entity_type,kind,canonical_name,normalized_name,created_at,updated_at) VALUES
 ('owner',1,'library','product','model','Parser owner','parser owner',now(),now()),
 ('file',1,'library','product','document_file','Parser file','parser file',now(),now());
 INSERT INTO shelf_changesets(id,space_id,library_id,revision,status,source,items,created_at,updated_at) VALUES('batch',1,'library',1,'confirmed','{"type":"web"}','[]',now(),now())`).Error)
	version := types.ShelfDocumentVersion{ID: "version", SpaceID: 1, LibraryID: "library", FileID: "file", OwnerID: func() *string { value := "owner"; return &value }(), CloudURL: "https://example.org/original", Ordinal: 1, VersionLabel: " Customer revision ", Stage: "final", IsCurrent: true, OccurredAt: time.Now(), CreatedAt: time.Now(), Source: types.OperationSource{Type: "web"}, ChangesetID: "batch", ParseStatus: "not_parsed", Conditions: []types.ShelfCondition{}}
	require.NoError(t, db.Omit("RequirementIDs", "ParserKey", "ParseAttempt", "ParseError", "FileCategory").Create(&version).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	old, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	oldRoot := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(oldRoot, "", old))
	require.NoError(t, migration.Migrate(48))
	restoredOld := pgtest.Open(t)
	_, err = maintenance.Import(ctx, restoredOld, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, restoredOld, oldRoot))
	rows, err := service.NewEntityService(restoredOld, nil).ShelfDocumentVersions(ctx, "file")
	require.NoError(t, err)
	require.Empty(t, rows[0].ParserKey)
	require.Zero(t, rows[0].ParseAttempt)
	require.Equal(t, "not_parsed", rows[0].ParseStatus)
	require.NoError(t, migration.Migrate(47), "old versions with default parser fields may be downgraded")
	require.NoError(t, migration.Migrate(48))
	require.NoError(t, db.Exec(`UPDATE libraries SET shelf_config='{"parsers":[{"key":"external","name":" External reader ","type":"http","url":"http://localhost:7000/parse","authorization":"Bearer synthetic-token","timeout_seconds":5,"concurrency":2}],"default_parser":"external"}' WHERE id='library';
 UPDATE shelf_document_versions SET parser_key='external',parse_attempt=2,parse_status='failed',parse_error='parser HTTP status 503',file_category='Synthetic report' WHERE id='version'`).Error)
	require.Error(t, maintenance.ScanCredentialReader(ctx, db, strings.NewReader("embedded synthetic-token in an opaque log")))
	snapshot, err := maintenance.Capture(ctx, db)
	require.NoError(t, err)
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "synthetic-token")
	root := t.TempDir()
	require.NoError(t, maintenance.WriteSnapshot(root, "", snapshot))
	expected, err := maintenance.BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	fresh := pgtest.Open(t)
	_, err = maintenance.Import(ctx, fresh, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, maintenance.VerifyImportedReplica(ctx, fresh, root))
	actual, err := maintenance.BuildManifest(ctx, fresh, root, "")
	require.NoError(t, err)
	require.Empty(t, maintenance.CompareManifest(expected, actual))
	rows, err = service.NewEntityService(fresh, nil).ShelfDocumentVersions(ctx, "file")
	require.NoError(t, err)
	config := actual.Tables["libraries"].Data[0]["shelf_config"].(map[string]any)
	parser := config["parsers"].([]any)[0].(map[string]any)
	require.Equal(t, " External reader ", parser["name"])
	require.NotContains(t, parser, "authorization")
	require.Equal(t, "external", rows[0].ParserKey)
	require.EqualValues(t, 2, rows[0].ParseAttempt)
	require.Equal(t, "failed", rows[0].ParseStatus)
	require.Equal(t, "parser HTTP status 503", rows[0].ParseError)
	require.Equal(t, " Customer revision ", rows[0].VersionLabel)
	require.ErrorContains(t, migration.Migrate(47), "shelf parser data exists")
}
