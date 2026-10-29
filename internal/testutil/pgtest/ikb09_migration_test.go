package pgtest

import (
	"encoding/json"
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"testing"
)

func TestIKB09MigrationPreservesPreviousVersionAndRoundTrips(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
	require.NoError(t, m.Migrate(14))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Upgrade','personal');
 INSERT INTO libraries(id,space_id,name,directory,template,embedding_model_id,summary_model_id) VALUES('one',1,'One','one','own','',''),('two',1,'Two','two','own','','');
 INSERT INTO topics(id,space_id,name,description) VALUES('topic',1,'Preserved','Description');
 INSERT INTO follow_feed_states(space_id,last_viewed_at) VALUES(1,'2026-10-14T01:02:03.123456Z');`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(15))
	after := baselineContents(t, db)
	for table, raw := range before {
		var want, got []map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &want))
		require.NoError(t, json.Unmarshal([]byte(after[table]), &got))
		if table == "topics" {
			for _, row := range got {
				require.Equal(t, []any{}, row["library_ids"])
				delete(row, "library_ids")
			}
		}
		require.ElementsMatch(t, want, got, table)
	}
	var states []struct {
		LibraryID    string
		LastViewedAt string
	}
	require.NoError(t, db.Raw("SELECT library_id,last_viewed_at::text FROM follow_feed_library_states ORDER BY library_id").Scan(&states).Error)
	require.Len(t, states, 2)
	require.Equal(t, states[0].LastViewedAt, states[1].LastViewedAt)
	require.NoError(t, m.Migrate(14))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Up())
	t.Logf("all up/down/up succeeded; 14 to 15 preserved all prior columns and row counts in %d tables", len(before))
}
