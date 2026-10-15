package pgtest

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestEntryContractMigration22To23PreservesEveryRow(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(22))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Entry contract','personal');
 INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id) VALUES('contract',1,'Contract','own','contract','','');
 INSERT INTO entries(id,space_id,library_id,type,title,source,parse_status,vault_body,related_article_id,metadata) VALUES('retained',1,'contract','manual','Title','','failed','Body','previous','{"custom":{"article_id":"opaque"}}');
 INSERT INTO ingest_discards(space_id,library_id,fingerprint,normalized_url,article_id,reason) VALUES(1,'contract',repeat('a',64),'https://example.test','retained','Original reason');
 INSERT INTO topics(id,space_id,name) VALUES('topic',1,'Topic');
 INSERT INTO topic_backfill_jobs(id,space_id,topic_id,topic_name,topic_description,status,total,processed,failed_articles,failed_batches) VALUES('job',1,'topic','Topic','Description','failed',2,2,2,1);
 INSERT INTO topic_backfill_batches(id,job_id,ordinal,entry_ids,article_count,status) VALUES('batch','job',1,'["retained","removed"]',2,'failed');`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(23))
	after := baselineContents(t, db)
	pairs := map[string][2]string{"entries": {"related_article_id", "related_entry_id"}, "ingest_discards": {"article_id", "entry_id"}, "topic_backfill_batches": {"article_count", "entry_count"}, "topic_backfill_jobs": {"failed_articles", "failed_entries"}}
	for table, original := range before {
		if pair, ok := pairs[table]; ok {
			var rows []map[string]any
			require.NoError(t, json.Unmarshal([]byte(original), &rows))
			for _, row := range rows {
				row[pair[1]] = row[pair[0]]
				delete(row, pair[0])
			}
			expected, err := json.Marshal(rows)
			require.NoError(t, err)
			require.JSONEq(t, string(expected), after[table], table)
		} else {
			require.JSONEq(t, original, after[table], table)
		}
	}
	var leftovers int
	require.NoError(t, db.Raw(`SELECT count(*) FROM information_schema.columns WHERE table_schema='public' AND column_name LIKE '%article%'`).Scan(&leftovers).Error)
	require.Zero(t, leftovers)
	require.Error(t, db.Exec(`UPDATE topic_backfill_batches SET entry_count=0`).Error)
	require.Error(t, db.Exec(`UPDATE topic_backfill_jobs SET failed_entries=-1`).Error)
	require.NoError(t, m.Migrate(22))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(23))
	require.Equal(t, after, baselineContents(t, db))
}
