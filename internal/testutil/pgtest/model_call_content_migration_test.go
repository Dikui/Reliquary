package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestCallContentMigration23To24PreservesEveryRow(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(23))
	require.NoError(t, db.Exec(`INSERT INTO model_calls(id,space_id,requested_at,model_id,model_name,provider,purpose,entry_id,currency,cost_status,duration_ms,success,error_category,attempt,retry_number)
 VALUES('00000000-0000-0000-0000-000000000024',1,'2026-10-15T01:02:03Z','old-model','Old model','fixture','article_processing','old-entry','USD','unknown',123,false,'interrupted',2,3);`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(24))
	after := baselineContents(t, db)
	for table, original := range before {
		if table == "model_calls" {
			var rows []map[string]any
			require.NoError(t, json.Unmarshal([]byte(original), &rows))
			for _, row := range rows {
				row["processing_attempt"] = 0
				row["span_id"] = ""
			}
			expected, err := json.Marshal(rows)
			require.NoError(t, err)
			require.JSONEq(t, string(expected), after[table])
		} else {
			require.JSONEq(t, original, after[table], table)
		}
	}
	require.JSONEq(t, "[]", after["model_call_contents"])
	require.JSONEq(t, "[]", after["model_call_content_deletions"])
	require.NoError(t, m.Migrate(23))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(24))
	require.Equal(t, after, baselineContents(t, db))
}
