package pgtest

import (
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestEntityTypeRemovalPreservesIdentitiesAndReferences(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(48))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Personal','personal');
INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id) VALUES('library',1,'Library','own','library','','');
INSERT INTO entries(id,space_id,library_id,type,title,source) VALUES('entry',1,'library','manual','Source','');
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,created_at,updated_at)
 VALUES('organization',1,'Shared name','sharedname','organization',now(),now()),
 ('person',1,'Shared name','sharedname','person',now(),now());
INSERT INTO entity_aliases(id,space_id,entity_id,entity_type,alias,normalized_alias,created_at)
 VALUES('alias',1,'organization','organization','Known name','knownname',now());
INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'entry','organization');
INSERT INTO follows(space_id,target_type,target_id) VALUES(1,'entity','person');
INSERT INTO entity_candidates(id,space_id,name,normalized_name,entity_type,status,entity_id,created_at,updated_at)
 VALUES('candidate',1,'Claude Code','claudecode','organization','pending',NULL,now(),now());
INSERT INTO entity_candidate_entries(space_id,candidate_id,entry_id,quote,created_at)
 VALUES(1,'candidate','entry','Claude Code is the subject.',now());`).Error)
	require.NoError(t, m.Migrate(49))
	for _, table := range []string{"entities", "entity_aliases", "entity_candidates"} {
		require.False(t, db.Migrator().HasColumn(table, "entity_type"), table)
	}
	var ids []string
	require.NoError(t, db.Table("entities").Order("id").Pluck("id", &ids).Error)
	require.Equal(t, []string{"organization", "person"}, ids)
	var name, linkedID, quote string
	require.NoError(t, db.Raw("SELECT alias FROM entity_aliases WHERE id='alias'").Scan(&name).Error)
	require.Equal(t, "Known name", name)
	require.NoError(t, db.Raw("SELECT entity_id FROM entry_entities WHERE entry_id='entry'").Scan(&linkedID).Error)
	require.Equal(t, "organization", linkedID)
	require.NoError(t, db.Raw("SELECT quote FROM entity_candidate_entries WHERE candidate_id='candidate'").Scan(&quote).Error)
	require.Equal(t, "Claude Code is the subject.", quote)
	var count int64
	require.NoError(t, db.Table("follows").Where("target_id='person'").Count(&count).Error)
	require.EqualValues(t, 1, count)
}
