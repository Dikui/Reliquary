package maintenance

import (
	"context"
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
