package pgtest

import (
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"testing"
)

func TestSegmentMigrationPreservesPreviousRows(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(15))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Segment upgrade','personal');
INSERT INTO libraries(id,name,space_id,template,directory,embedding_model_id,summary_model_id) VALUES('segment-library','Segment library',1,'own','segment-library','','');
INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status,vault_body,metadata) VALUES('segment-old',1,'segment-library','manual','Retained title','','failed','Retained original body','{"intake":{"decision":"approved"}}');`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(16))
	after := baselineContents(t, db)
	for table, rows := range before {
		require.JSONEq(t, rows, after[table], table)
	}
	require.JSONEq(t, "[]", after["entry_segment_runs"])
	require.NoError(t, m.Migrate(15))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(16))
}
