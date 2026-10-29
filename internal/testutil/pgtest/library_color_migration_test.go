package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryColorMigrationPreservesRowsAndAssignsInCreationOrder(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	if err := m.Migrate(34); err != migrate.ErrNoChange {
		require.NoError(t, err)
	}
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Color migration','personal');
INSERT INTO libraries(id,space_id,name,directory,created_at,description,chunking_config,
                      embedding_model_id,summary_model_id)
SELECT 'color-' || n, 1, 'Library ' || n, 'color-' || n,
       '2026-01-01'::timestamptz + n * interval '1 day', 'Retained description', '{"chunk_size":777}', '', ''
FROM generate_series(10,1,-1) n;
INSERT INTO libraries(id,space_id,name,directory,created_at,embedding_model_id,summary_model_id) VALUES
('tie-b',1,'Tie B','tie-b','2026-02-01','',''),('tie-a',1,'Tie A','tie-a','2026-02-01','','');
INSERT INTO entries(id,space_id,library_id,type,title,source,vault_body)
VALUES('retained',1,'color-1','manual','Original title','','Original Markdown');
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(35))
	var colors []int
	require.NoError(t, db.Raw(`SELECT color FROM libraries WHERE space_id=1 ORDER BY created_at, id`).
		Scan(&colors).Error)
	require.Equal(t, []int{1, 2, 3, 4, 5, 6, 7, 8, 1, 2, 3, 4}, colors)
	after := baselineContents(t, db)
	var rows []map[string]any
	require.NoError(t, json.Unmarshal([]byte(after["libraries"]), &rows))
	for _, row := range rows {
		delete(row, "color")
	}
	data, err := json.Marshal(rows)
	require.NoError(t, err)
	after["libraries"] = string(data)
	for table, original := range before {
		require.JSONEq(t, original, after[table], table)
	}
	for _, value := range []any{0, 9, -1, nil} {
		require.Error(t, db.Exec(`UPDATE libraries SET color=? WHERE id='color-1'`, value).Error)
	}
	require.NoError(t, m.Migrate(34))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(35))
}
