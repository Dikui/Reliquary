package maintenance

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/testutil/pgtest"
)

// Compare all preexisting data exactly, after asserting each intentional
// migration addition against its specified value.
func preservedBeforeVectorDefaults(t *testing.T, table string, before, after []map[string]any) []map[string]any {
	t.Helper()
	data, err := json.Marshal(after)
	require.NoError(t, err)
	var rows []map[string]any
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	require.NoError(t, decoder.Decode(&rows))
	if table == "models" {
		kept := rows[:0]
		for _, row := range rows {
			if row["id"] != defaultVectorModelID {
				kept = append(kept, row)
				continue
			}
			expected := migrationVectorModel(row["created_at"], row["updated_at"])
			a, _ := json.Marshal(canonicalRows(table, []map[string]any{expected}))
			b, _ := json.Marshal(canonicalRows(table, []map[string]any{row}))
			require.JSONEq(t, string(a), string(b))
			require.Equal(t, row["created_at"], row["updated_at"])
		}
		rows = kept
	}
	if table == "spaces" {
		for _, row := range rows {
			for _, old := range before {
				if row["id"] == old["id"] && old["embedding_model_id"] == "" {
					require.Equal(t, defaultVectorModelID, row["embedding_model_id"])
					row["embedding_model_id"] = ""
				}
			}
		}
	}
	if table == "embedding_rebuild_runs" {
		for _, row := range rows {
			require.Equal(t, false, row["candidate"])
			require.Nil(t, row["switched_at"])
			delete(row, "candidate")
			delete(row, "switched_at")
		}
	}
	return rows
}

func TestIKB08RegisteredCandidateRoundTrip(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	ctx := context.Background()
	require.NoError(t, db.Exec(`INSERT INTO embedding_rebuild_runs(id,space_id,status,model_id,model_name,dimension,profile_hash,candidate,switched_at) VALUES('candidate',1,'completed','model','fixture',3,'hash',true,now());
 INSERT INTO embedding_rebuild_items(id,run_id,kind,source_id,status) VALUES('item','candidate','article_chunk','source','completed');
 INSERT INTO embedding_rebuild_vectors(run_id,kind,source_id,entry_id,library_id,model_id,dimension,embedding,content_hash) VALUES('candidate','chunk','source','entry','library','model',3,'[1,0,0]','hash');
 INSERT INTO embedding_vector_archives(run_id,space_id,payload) VALUES('candidate',1,'{}')`).Error)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.Equal(t, true, snapshot.Tables["embedding_rebuild_runs"][0]["candidate"])
	require.NotNil(t, snapshot.Tables["embedding_rebuild_runs"][0]["switched_at"])
	require.NotContains(t, snapshot.Tables, "embedding_rebuild_vectors")
	require.NotContains(t, snapshot.Tables, "embedding_vector_archives")
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	restored := pgtest.Open(t)
	_, err = Import(ctx, restored, root, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, restored, root))
	for _, table := range []string{"embedding_rebuild_vectors", "embedding_vector_archives"} {
		var count int64
		require.NoError(t, restored.Table(table).Count(&count).Error)
		require.Zero(t, count)
	}
	require.NoError(t, restored.Exec(`UPDATE embedding_rebuild_runs SET candidate=false WHERE id='candidate'`).Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, restored, root), "embedding_rebuild_runs")
}

func TestIKB08VectorMigrationProjectionRejectsTampering(t *testing.T) {
	stamp := time.Now().UTC().Format(time.RFC3339Nano)
	old := &Manifest{Tables: map[string]TableManifest{
		"embedding_rebuild_runs": {Columns: []string{"id"}, Data: []map[string]any{{"id": "run", "large_counter": json.Number("9007199254740993")}}, Rows: 1},
		"spaces":                 {Columns: []string{"id", "embedding_model_id"}, Data: []map[string]any{{"id": json.Number("1"), "embedding_model_id": ""}}, Rows: 1},
		"models":                 {Columns: []string{"id"}, Data: []map[string]any{}},
	}, Files: map[string][]File{}}
	actual := &Manifest{Tables: map[string]TableManifest{
		"embedding_rebuild_runs": {Columns: []string{"candidate", "id", "switched_at"}},
		"models":                 {Data: []map[string]any{migrationVectorModel(stamp, stamp)}},
	}}
	projected, _ := projectVectorManifest(old, actual)
	// A current manifest is never projected a second time.
	current, _ := projectVectorManifest(projected, actual)
	require.Same(t, projected, current)
	require.Equal(t, json.Number("9007199254740993"), projected.Tables["embedding_rebuild_runs"].Data[0]["large_counter"])
	want := projected.Tables["models"].Data[0]
	require.Equal(t, "qwen3-embedding:0.6b", want["name"])
	require.Equal(t, "", old.Tables["spaces"].Data[0]["embedding_model_id"])
	require.Equal(t, defaultVectorModelID, projected.Tables["spaces"].Data[0]["embedding_model_id"])
	for _, field := range []string{"name", "status", "source", "managed_by", "parameters", "is_builtin"} {
		modified := migrationVectorModel(stamp, stamp)
		modified[field] = "tampered"
		actual.Tables["models"] = TableManifest{Data: []map[string]any{modified}}
		derived, _ := projectVectorManifest(old, actual)
		a, _ := json.Marshal(canonicalRows("models", derived.Tables["models"].Data))
		b, _ := json.Marshal(canonicalRows("models", actual.Tables["models"].Data))
		require.NotEqual(t, string(a), string(b), field)
	}
}
