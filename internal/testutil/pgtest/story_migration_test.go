package pgtest

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestStoryMigrationPreservesEveryPreviousTable(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(38))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business) VALUES(1,'Preserved space','personal');
 INSERT INTO libraries(id,space_id,name,template,directory,source_evidence_rule,embedding_model_id,summary_model_id)
 VALUES('library',1,'Preserved library','external_timely','official','link_required','','');
 INSERT INTO entries(id,space_id,library_id,type,title,title_manual,description_manual,source,vault_body,metadata)
 VALUES('entry',1,'library','manual','Preserved title',true,true,'https://example.test/source',
 'Original body','{"retained":true}');
 INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at)
 VALUES('entity',1,'Preserved entity','preservedentity','organization',now(),now());
 INSERT INTO entry_entities(space_id,entry_id,entity_id,created_at) VALUES(1,'entry','entity',now());
 INSERT INTO domains(id,space_id,name,normalized_name,definition,created_at,updated_at)
 VALUES('domain',1,'Direction','direction','Preserved definition',now(),now());
 INSERT INTO entry_domains(space_id,entry_id,domain_id,created_at) VALUES(1,'entry','domain',now());
 `).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(39))
	after := baselineContents(t, db)
	for table, rows := range before {
		require.JSONEq(t, rows, after[table], table)
	}
	for _, table := range []string{"stories", "facts", "entry_facts"} {
		require.JSONEq(t, "[]", after[table], table)
	}
	require.NoError(t, m.Migrate(38))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(39))
	require.Equal(t, after, baselineContents(t, db))
}
