package pgtest

import (
	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"path/filepath"
	"testing"
)

func TestEvaluationMigrationPreservesPreviousRowsAndFullCycle(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { m.Close() })
	require.NoError(t, m.Migrate(17))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Evaluation upgrade','personal');
 INSERT INTO libraries(id,name,space_id,template,directory,embedding_model_id,summary_model_id) VALUES('evaluation-library','Library',1,'own','evaluation-library','','');
 INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status,vault_body,metadata) VALUES('evaluation-old',1,'evaluation-library','manual','Retained title','','failed','Retained body','{"intake":{"decision":"approved"}}')`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(21))
	after := baselineContents(t, db)
	for table, rows := range before {
		require.JSONEq(t, rows, after[table], table)
	}
	require.NoError(t, m.Migrate(17))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
	require.NoError(t, m.Down())
	require.NoError(t, m.Up())
}
