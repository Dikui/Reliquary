package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestRedisPendingOperationsMigrationPreservesBusinessRows(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(8))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business) VALUES(1,'Migration owner','personal');
 INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id) VALUES('library',1,'Preserved library','','');
 INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status,enable_status) VALUES('entry',1,'library','manual','Preserved title','','completed','enabled');
 INSERT INTO task_jobs(id,task_type,queue,payload,state) VALUES('retired','manual:process','default','{"entry_id":"entry"}','pending');
 INSERT INTO task_dead_letters(space_id,task_type,scope,scope_id,payload,fail_count) VALUES(1,'manual:process','knowledge_base','library','{"detail":"keep raw payload"}',3);
 INSERT INTO task_queue_counters(queue,day,processed,failed) VALUES('default','2026-10-01',17,3);
 `).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(9))
	after := baselineContents(t, db)
	require.NotContains(t, after, "task_jobs")
	require.JSONEq(t, "[]", after["task_pending_ops"])
	for table, raw := range before {
		if table == "task_jobs" || table == "schema_migrations" {
			continue
		}
		if table == "task_dead_letters" {
			var rows []map[string]any
			require.NoError(t, json.Unmarshal([]byte(raw), &rows))
			for _, row := range rows {
				if row["scope"] == "knowledge_base" {
					row["scope"] = "library"
				}
			}
			data, err := json.Marshal(rows)
			require.NoError(t, err)
			require.JSONEq(t, string(data), after[table], table)
		} else {
			require.JSONEq(t, raw, after[table], table)
		}
	}
	require.NoError(t, m.Migrate(8))
	require.NoError(t, m.Migrate(9))
}

func TestRedisMigrationFullUpDownUp(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
	version, dirty, err := m.Version()
	require.NoError(t, err)
	require.EqualValues(t, 10, version)
	require.False(t, dirty)
}
