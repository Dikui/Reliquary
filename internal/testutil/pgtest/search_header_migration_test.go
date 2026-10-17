package pgtest

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestSearchHeaderMigrationScopesNamesAndPreservesRows(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(24))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Header migration','personal');
INSERT INTO libraries(id,space_id,name,directory,template,embedding_model_id,summary_model_id)
VALUES('header-library',1,'Header library','header-fixture','own','','');
INSERT INTO entries(id,space_id,library_id,type,title,description,source,source_kind,parse_status,enable_status,vault_body,metadata,structured_summary,starred,created_at,updated_at) VALUES
('linked',1,'header-library','manual','Linked title','Preserved description','','article','completed','enabled','Original Markdown body','{"publisher":"Publisherneedle"}','{"overview":"Summaryneedle","points":["Preserved point"],"key_terms":["Termneedle"]}',true,'2026-10-01T01:02:03Z','2026-10-02T01:02:03Z');
INSERT INTO entities(id,space_id,canonical_name,normalized_name,entity_type,description,created_at,updated_at)
VALUES('header-entity',1,'Canonicalneedle','canonicalneedle','company','Owner-edited introduction',now(),now());
INSERT INTO entity_aliases(id,space_id,entity_id,alias,normalized_alias,entity_type,created_at)
VALUES('header-alias',1,'header-entity','Aliasneedle','aliasneedle','company',now());
INSERT INTO topics(id,space_id,name) VALUES('header-topic',1,'Topicneedle');
INSERT INTO entry_entities(space_id,entry_id,entity_id) VALUES(1,'linked','header-entity');
INSERT INTO entry_topics(space_id,entry_id,topic_id,source) VALUES(1,'linked','header-topic','user');
INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,parse_status,enable_status,vault_body)
VALUES('unrelated',1,'header-library','manual','Unrelated title','','article','completed','enabled','Unrelated body');
INSERT INTO article_embeddings(space_id,entry_id,library_id,model_id,dimension,embedding,content_hash)
VALUES(1,'linked','header-library','fixture',3,'[1,0,0]','preserved');
INSERT INTO image_embeddings(space_id,entry_id,library_id,attachment_path,model_id,dimension,embedding,content_hash)
VALUES(1,'linked','header-library','image.png','fixture',3,'[1,0,0]','preserved');
`).Error)
	before := baselineContents(t, db)
	assertPreserved := func(expected map[string]string) {
		actual := baselineContents(t, db)
		require.Len(t, actual, len(expected))
		for table, original := range expected {
			if table != "entries" {
				require.JSONEq(t, original, actual[table], table)
				continue
			}
			var want, got []map[string]any
			require.NoError(t, json.Unmarshal([]byte(original), &want))
			require.NoError(t, json.Unmarshal([]byte(actual[table]), &got))
			for _, rows := range [][]map[string]any{want, got} {
				for _, row := range rows {
					delete(row, "search_text")
				}
			}
			require.ElementsMatch(t, want, got, "only derived search_text may change")
		}
	}
	require.NoError(t, m.Migrate(25))
	assertPreserved(before)
	var linked, unrelated, missing string
	require.NoError(t, db.Raw("SELECT search_text FROM entries WHERE id='linked'").Scan(&linked).Error)
	require.NoError(t, db.Raw("SELECT search_text FROM entries WHERE id='unrelated'").Scan(&unrelated).Error)
	require.NoError(t, db.Raw("SELECT public.build_entry_search_text('missing',1,'','','{}','{}')").Scan(&missing).Error)
	for _, term := range []string{"Canonicalneedle", "Aliasneedle", "Topicneedle"} {
		require.Contains(t, linked, term)
		require.NotContains(t, unrelated, term)
	}
	require.Empty(t, strings.TrimSpace(missing))
	// A post-upgrade write must not reintroduce expensive read-time indexing.
	require.NoError(t, db.Exec(`INSERT INTO entries(id,space_id,library_id,type,title,description,source,source_kind,parse_status,enable_status)
VALUES('new-entry',1,'header-library','manual','Newneedle',repeat('Synthetic header text ',1500),'','article','completed','enabled')`).Error)
	var mutable int64
	require.NoError(t, db.Raw("SELECT count(*) FROM paradedb.index_info('entries_summary_search_idx'::regclass,false) WHERE mutable").Scan(&mutable).Error)
	require.Zero(t, mutable)
	var ids []string
	started := time.Now()
	require.NoError(t, db.Raw("SELECT id FROM entries WHERE search_seq_id @@@ paradedb.match_disjunction('title','Newneedle')").Scan(&ids).Error)
	require.Equal(t, []string{"new-entry"}, ids)
	t.Logf("header BM25 after inserting a long header: %s", time.Since(started))
	afterInsert := baselineContents(t, db)
	require.NoError(t, m.Migrate(24))
	assertPreserved(afterInsert)
	require.NoError(t, m.Migrate(25))
	assertPreserved(afterInsert)
	var disabled bool
	require.NoError(t, db.Raw("SELECT 'mutable_segment_rows=0'=ANY(reloptions) FROM pg_class WHERE oid='entries_summary_search_idx'::regclass").Scan(&disabled).Error)
	require.True(t, disabled)
}
