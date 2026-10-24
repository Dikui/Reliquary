package maintenance

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIKB20AuditEveryInstalledColumn(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	rows, err := Audit(ctx, db)
	require.NoError(t, err)
	columns, err := CatalogSchema(ctx, db)
	require.NoError(t, err)
	require.Len(t, rows, len(columns))
	for _, row := range rows {
		require.Contains(t, []string{"replica", "backup_only", "regenerated"}, row.Owner, row)
	}
	if path := os.Getenv("RELIQUARY_AUDIT_OUTPUT"); path != "" {
		data, err := json.MarshalIndent(rows, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, append(data, '\n'), 0o600))
	}
	// Deliberately omit an existing table and column, as well as unknown DDL.
	original := Rules["evaluation_answers"]
	delete(Rules, "evaluation_answers")
	_, err = Audit(ctx, db)
	Rules["evaluation_answers"] = original
	require.ErrorContains(t, err, "evaluation_answers")
	Rules["evaluation_answers"] = Rule{Sidecar: "question_id entry_id"}
	_, err = Audit(ctx, db)
	Rules["evaluation_answers"] = original
	require.ErrorContains(t, err, "evaluation_answers.grade")
	require.NoError(t, db.Exec("CREATE TABLE public.unowned_table(id text)").Error)
	_, err = Audit(ctx, db)
	require.ErrorContains(t, err, "unowned_table")
	require.NoError(
		t,
		db.Exec("DROP TABLE public.unowned_table; ALTER TABLE evaluation_answers ADD COLUMN unowned_column text").Error,
	)
	_, err = Audit(ctx, db)
	require.ErrorContains(t, err, "evaluation_answers.unowned_column")
}

func TestIKB20ReplicaFileTamperReportsSpecificRow(t *testing.T) {
	db := pgtest.Open(t)
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO evaluation_sets(id,space_id,name) VALUES('tampered-set',1,'Original set')`).Error)
	ctx := context.Background()
	root := t.TempDir()
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	require.NoError(t, WriteSnapshot(root, "", snapshot))
	path := filepath.Join(root, ".reliquary/data/evaluation_sets.json")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t,
		os.WriteFile(path,
			[]byte(strings.ReplaceAll(string(data),
				"Original set",
				"Changed set")),
			0o600))
	result, err := VerifyReplica(ctx, db, root, "", false)
	require.NoError(t, err)
	require.False(t, result.OK)
	require.Contains(t, strings.Join(result.Differences, ";"), "table evaluation_sets row tampered-set")
}

func TestIKB20ImportOrderCoversActualForeignKeys(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	seed(t, db)
	snapshot, err := Capture(ctx, db)
	require.NoError(t, err)
	order, err := replicaImportOrder(db, snapshot)
	require.NoError(t, err)
	position := map[string]int{}
	for i, table := range order {
		position[table] = i
	}
	var dependencies []struct{ Child, Parent string }
	require.NoError(t, db.Raw(`SELECT child.relname AS child,parent.relname AS parent FROM pg_constraint fk JOIN pg_class child ON child.oid=fk.conrelid JOIN pg_class parent ON parent.oid=fk.confrelid WHERE fk.contype='f'`).Scan(&dependencies).Error)
	for table := range snapshot.Tables {
		_, ok := position[table]
		require.True(t, ok, table)
	}
	for _, dependency := range dependencies {
		child, childPresent := position[dependency.Child]
		parent, parentPresent := position[dependency.Parent]
		if childPresent && parentPresent && dependency.Child != dependency.Parent {
			require.Less(t, parent, child, dependency)
		}
	}
}

func TestIKB20DiagnosticsIdentifyCompositeRowsAndPreserveTaskPayloads(t *testing.T) {
	before := []map[string]any{{"question_id": "q", "entry_id": "e", "grade": 1}}
	after := []map[string]any{{"question_id": "q", "entry_id": "e", "grade": 2}}
	differences := rowDifferences("evaluation_answers", before, after)
	require.Len(t, differences, 1)
	require.Contains(t, differences[0], `"question_id":"q"`)
	require.Contains(t, differences[0], `"entry_id":"e"`)
	op := map[string]any{"id": "op", "state": "active", "delivery": json.Number("4"), "generation": json.Number("7"), "payload": map[string]any{"entry_id": "e"}, "retried": 2, "completed_at": nil}
	copy := map[string]any{}
	for key, value := range op {
		copy[key] = value
	}
	copy["state"], copy["delivery"] = "dispatched", json.Number("5")
	copy["dispatched_at"] = "2026-10-14T12:00:00Z"
	left, right := comparableOperationRows([]map[string]any{op}, []map[string]any{copy})
	require.Empty(t, rowDifferences("task_pending_ops", left, right))
	for _, key := range strings.Fields("payload generation retried process_at last_error") {
		original, present := copy[key]
		copy[key] = "tampered"
		left, right = comparableOperationRows([]map[string]any{op}, []map[string]any{copy})
		require.NotEmpty(t, rowDifferences("task_pending_ops", left, right), key)
		if present {
			copy[key] = original
		} else {
			delete(copy, key)
		}
	}
}
