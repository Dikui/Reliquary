package maintenance

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQueueMigrationComparisonDoesNotHideNewOperations(t *testing.T) {
	expected := &Manifest{FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"task_jobs": {Rows: 1, Data: []map[string]any{{"id": "retired", "state": "pending"}}},
	}}
	actual := &Manifest{FormatVersion: FormatVersion, Tables: map[string]TableManifest{
		"task_pending_ops": {},
	}}
	require.Empty(t, CompareManifest(expected, actual), "the migration intentionally removes old messages")
	actual.Tables["task_pending_ops"] = TableManifest{Rows: 1, Data: []map[string]any{{"id": "unexpected", "state": "pending"}}}
	require.NotEmpty(t, CompareManifest(expected, actual), "restoration must not conceal new operation data")
	mixed := &Snapshot{Tables: map[string][]map[string]any{
		"task_jobs":        {},
		"task_pending_ops": {{"id": "preserve-me"}},
	}}
	require.ErrorContains(t, migrateQueueSnapshot(mixed), "both legacy tasks and pending operations")
	require.Len(t, mixed.Tables["task_pending_ops"], 1)
}
