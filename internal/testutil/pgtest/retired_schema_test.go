package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestRetirementMigrationPreservesRetainedData(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	if err := m.Migrate(32); err != migrate.ErrNoChange {
		require.NoError(t, err)
	}
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Retained','personal');
INSERT INTO libraries(id,
space_id,
name,
template,
directory,
embedding_model_id,
summary_model_id)
VALUES('retained',
1,
'Retained',
'own',
'retained',
'',
'');
INSERT INTO entries(id,
space_id,
library_id,
type,
title,
source,
parse_status,
vault_body,
graph,
metadata)
VALUES('entry',
1,
'retained',
'manual',
'Title',
'',
'completed',
'Saved body',
true,
'{"custom":"retained"}');
INSERT INTO entities(id,
space_id,
canonical_name,
normalized_name,
entity_type,
created_at,
updated_at)
VALUES('a',
1,
'A',
'a',
'company',
now(),
now()),
('b',
1,
'B',
'b',
'company',
now(),
now());
INSERT INTO library_content_types(library_id,name,position,enabled,extract_graph) VALUES('retained','Note',0,true,true);
INSERT INTO library_relation_types(library_id,
name,
position,
enabled)
VALUES('retained',
'uses',
0,
true),
('retained',
'depends',
1,
true);
INSERT INTO relations(id,
space_id,
library_id,
subject_id,
relation_type,
object_id,
version)
VALUES('r',
1,
'retained',
'a',
'uses',
'b',
1);
INSERT INTO task_pending_ops(id,task_type,queue,state,payload) VALUES
('retired-sync','graph:sync','low','pending','null'),
('retired-check','graph:check','low','pending','{}'),
('retired-rebuild','graph:rebuild','low','pending','{}'),
('retained-task','document:process','normal','pending','{}');
INSERT INTO relation_sources(relation_id,entry_id,evidence,run_id,body_hash) VALUES('r','entry','[]','run','hash');
INSERT INTO relation_suppressions(space_id,
library_id,
subject_id,
relation_type,
object_id,
actor)
VALUES(1,
'retained',
'b',
'uses',
'a',
'user');
INSERT INTO relation_type_merges(library_id,from_type,to_type) VALUES('retained','depends','uses');
INSERT INTO graph_pending_sync(id,
space_id,
entity_id,
relation_id,
relation_version,
deleted)
VALUES('pending',
1,
'a',
'r',
1,
false);
INSERT INTO entry_processing_outputs(entry_id,
space_id,
library_id,
body_hash,
content_type,
relations,
images)
VALUES('entry',
1,
'retained',
'hash',
'Note',
'[{"type":"uses"}]',
'[{"url":"saved.png"}]');`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(33))
	after := baselineContents(t, db)
	for _, table := range []string{
		"relations",
		"relation_sources",
		"relation_suppressions",
		"relation_type_merges",
		"library_relation_types",
		"graph_pending_sync",
	} {
		require.NotContains(t, after, table)
		delete(before, table)
	}
	for table, original := range before {
		field := map[string]string{
			"entries":                  "graph",
			"library_content_types":    "extract_graph",
			"entry_processing_outputs": "relations",
		}[table]
		var rows []map[string]any
		require.NoError(t, json.Unmarshal([]byte(original), &rows))
		if table == "task_pending_ops" {
			kept := rows[:0]
			for _, row := range rows {
				if row["id"] == "retained-task" {
					kept = append(kept, row)
				}
			}
			rows = kept
		}
		for _, row := range rows {
			if field != "" {
				delete(row, field)
			}
		}
		expected, err := json.Marshal(rows)
		require.NoError(t, err)
		require.JSONEq(t, string(expected), after[table], table)
	}
	// Development down recreates empty retired structures; retained data survives.
	require.NoError(t, m.Migrate(32))
	require.NoError(t, m.Migrate(33))
	require.Equal(t, after, baselineContents(t, db))
}
