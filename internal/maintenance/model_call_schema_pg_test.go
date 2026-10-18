package maintenance

import (
	"context"
	"github.com/golang-migrate/migrate/v4"
	"gorm.io/driver/postgres"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestModelCallsStayOutOfReplicaAndSQLManifestDetectsTampering(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	cost := 0.2
	call := types.ModelCall{ID: uuid.NewString(), SpaceID: 1, RequestedAt: time.Now().UTC(), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeOther, Currency: "USD", Cost: &cost, CostStatus: "calculated", Attempt: 1, Success: true}
	require.NoError(t, db.Create(&call).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, snapshot.Tables, "model_calls")
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	before, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.EqualValues(t, 1, before.Tables["model_calls"].Rows)
	require.False(t, before.Tables["model_calls"].Regenerated)
	require.Nil(t, before.Tables["model_calls"].Data)
	require.NoError(t, db.Exec("UPDATE model_calls SET cost=0.3 WHERE id=?", call.ID).Error)
	after, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, after), "table model_calls")
	require.Contains(t, CompareManifestWithOptions(before, after, true, false), "table model_calls")
	require.NoError(t, db.Exec("UPDATE model_calls SET cost=0.2 WHERE id=?", call.ID).Error)
	call.ID = uuid.NewString()
	require.NoError(t, db.Create(&call).Error)
	appended, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, appended), "table model_calls")
	require.Empty(t, CompareManifestWithOptions(before, appended, true, false))
}
func TestOldModelCallManifestProjectsOnlyEmptyLedger(t *testing.T) {
	old := &Manifest{SchemaVersion: 21, FormatVersion: FormatVersion, Tables: map[string]TableManifest{}, Files: map[string][]File{}}
	current := &Manifest{SchemaVersion: 22, FormatVersion: FormatVersion, Tables: map[string]TableManifest{"model_calls": {SHA256: sum([]byte("[]"))}}, Files: map[string][]File{}}
	require.Empty(t, CompareManifest(old, current))
	current.Tables["model_calls"] = TableManifest{Rows: 1, SHA256: "changed"}
	require.Contains(t, CompareManifest(old, current), "table model_calls")
	require.NotContains(t, old.Tables, "model_calls")
}

func TestCallPricingManifestPreservesOldLedgerAndDetectsNewUsage(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(28))
	call := types.ModelCall{ID: uuid.NewString(), SpaceID: 1, RequestedAt: time.Now().UTC(), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeRerank, Currency: "USD", CostStatus: "unknown", Attempt: 1, Success: true}
	require.NoError(t, db.Omit("TextInputTokens", "ImageInputTokens", "ImageInputPrice").Create(&call).Error)
	before, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(29))
	after, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec("UPDATE model_calls SET text_input_tokens=10,image_input_tokens=20,image_input_price=3 WHERE id=?", call.ID).Error)
	changed, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, changed), "table model_calls")
	require.Contains(t, CompareManifest(after, changed), "table model_calls")
}

func TestCallContentManifestDetectsTamperingAndProjectsVersion23(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(23))
	call := types.ModelCall{ID: uuid.NewString(), SpaceID: 1, RequestedAt: time.Now().UTC(), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeOther, Currency: "USD", CostStatus: "unknown", Attempt: 1, Success: true}
	require.NoError(t, db.Omit("ProcessingAttempt", "SpanID", "TextInputTokens", "ImageInputTokens", "ImageInputPrice").Create(&call).Error)
	before, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(24))
	after, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.Empty(t, CompareManifest(before, after))
	require.NoError(t, db.Exec("INSERT INTO model_call_contents(id,request_gzip,response_gzip,request_bytes,response_bytes,response_type,http_status,complete) VALUES(?,?::bytea,?::bytea,3,3,'text/plain',200,true)", call.ID, "abc", "def").Error)
	current, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.Contains(t, CompareManifest(before, current), "table model_call_contents")
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NotContains(t, snapshot.Tables, "model_call_contents")
	require.NoError(t, db.Exec("UPDATE model_call_contents SET response_gzip='changed'::bytea WHERE id=?", call.ID).Error)
	changed, err := BuildManifest(ctx, db, t.TempDir(), t.TempDir())
	require.NoError(t, err)
	require.Contains(t, strings.Join(CompareManifest(current, changed), "\n"), call.ID)
	require.NoError(t, db.Exec("ALTER TABLE model_call_contents ADD COLUMN unowned text").Error)
	require.Error(t, ValidateClassification(ctx, db))
}
