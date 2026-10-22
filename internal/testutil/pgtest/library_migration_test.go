package pgtest

import (
	"encoding/json"
	"path/filepath"
	"reliquary/internal/types"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestLibraryNamingMigrationPreservesRowsAndSearch(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(6))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal');
INSERT INTO knowledge_bases(id,space_id,name,embedding_model_id,summary_model_id) VALUES('library',1,'Library','','');
INSERT INTO entries(id,space_id,knowledge_base_id,type,title,source,parse_status,enable_status) VALUES('entry',1,'library','manual','Libraryneedle','','completed','enabled');
INSERT INTO chunks(id,space_id,knowledge_base_id,entry_id,content,chunk_type,is_enabled,chunk_index,start_at,end_at) VALUES('chunk',1,'library','entry','Libraryneedle evidence','text',true,0,0,22);
INSERT INTO embeddings(source_id,source_type,chunk_id,entry_id,knowledge_base_id,dimension,embedding,is_enabled) VALUES('chunk',0,'chunk','entry','library',3,'[1,0,0]',true);
INSERT INTO article_embeddings(space_id,entry_id,knowledge_base_id,model_id,dimension,embedding,content_hash) VALUES(1,'entry','library','fixture',3,'[1,0,0]','hash');
INSERT INTO image_embeddings(space_id,entry_id,knowledge_base_id,attachment_path,model_id,dimension,embedding,content_hash) VALUES(1,'entry','library','image.png','fixture',3,'[1,0,0]','hash');
INSERT INTO entry_repost_sources(id,space_id,knowledge_base_id,entry_id,source_fingerprint,match_method,created_at) VALUES('repost',1,'library','entry','fingerprint','url',now());
INSERT INTO task_dead_letters(space_id,task_type,scope,scope_id,payload,fail_count) VALUES(1,'manual:process','knowledge_base','library','{"knowledge_base_id":"library","knowledge_id":"entry"}',1);
`).Error)
	before := baselineContents(t, db)
	require.NoError(t, m.Migrate(7))
	after := baselineContents(t, db)
	for table, raw := range before {
		var rows, actual []map[string]any
		require.NoError(t, json.Unmarshal([]byte(raw), &rows))
		for _, row := range rows {
			if value, ok := row["knowledge_base_id"]; ok {
				row["library_id"] = value
				delete(row, "knowledge_base_id")
			}
		}
		if table == "knowledge_bases" {
			table = "libraries"
		}
		require.NoError(t, json.Unmarshal([]byte(after[table]), &actual))
		require.ElementsMatch(t, rows, actual, table)
	}
	var count int64
	require.NoError(t, db.Raw(`SELECT count(*) FROM entries WHERE library_id='library' AND search_seq_id @@@ 'search_text:Libraryneedle'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	require.NoError(t, db.Raw(`SELECT count(*) FROM chunks WHERE library_id='library' AND seq_id @@@ 'content:Libraryneedle'`).Scan(&count).Error)
	require.EqualValues(t, 1, count)
	for _, index := range []string{"chunks_search_idx", "entries_summary_search_idx"} {
		require.NoError(t, db.Raw(`SELECT count(*) FROM paradedb.schema(?::regclass) WHERE name='library_id'`, index).Scan(&count).Error)
		require.EqualValues(t, 1, count)
	}
	var id string
	require.NoError(t, db.Raw(`SELECT entry_id FROM embeddings WHERE library_id='library' ORDER BY embedding <=> '[1,0,0]'::halfvec LIMIT 1`).Scan(&id).Error)
	require.Equal(t, "entry", id)
	require.Error(t, db.Exec(`INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id) VALUES('second',1,'Forbidden','','')`).Error, "the single-library constraint must reject a second active row")
	require.NoError(t, m.Migrate(6))
	require.Equal(t, before, baselineContents(t, db))
	require.NoError(t, m.Migrate(7))
	require.Equal(t, after, baselineContents(t, db))
}

func TestSystemLibraryLabelMigrationPreservesCustomNamesAndOtherData(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(25))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Labels','personal');
INSERT INTO libraries(id,space_id,name,system_role,directory,template,source_evidence_rule,embedding_model_id,summary_model_id) VALUES
('official',1,'机构发布','institutional','institutional','external_timely','link_required','',''),
('media',1,'人物观点','opinions','opinions','external_timely','link_required','',''),
('reference',1,'参考文献','references','references','external_durable','','',''),
('personal',1,'我的资料','mine','mine','own','','',''),
('ai',1,'AI 报告','ai-reports','ai-reports','generated','','',''),
('custom',1,'机构发布','','custom-library','own','','','');
INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,parse_status,enable_status,vault_body,vault_path)
VALUES('entry',1,'media','manual','Preserved title','','article','completed','enabled','Original body','opinions/unchanged.md');
`).Error)
	before := baselineContents(t, db)
	expected := map[string]string{}
	for table, raw := range before {
		expected[table] = raw
	}
	var libraries []map[string]any
	require.NoError(t, json.Unmarshal([]byte(expected["libraries"]), &libraries))
	names := map[string]string{"official": "官方信息", "media": "媒体信息", "reference": "参考文档", "personal": "个人产出", "ai": "AI产出"}
	for _, library := range libraries {
		if name, ok := names[library["id"].(string)]; ok {
			library["name"] = name
		}
	}
	raw, err := json.Marshal(libraries)
	require.NoError(t, err)
	expected["libraries"] = string(raw)
	check := func(want map[string]string) {
		t.Helper()
		actual := baselineContents(t, db)
		require.Len(t, actual, len(want))
		for table, rows := range want {
			require.JSONEq(t, rows, actual[table], table)
		}
	}
	require.NoError(t, m.Migrate(26))
	check(expected)
	require.NoError(t, m.Migrate(25))
	check(before)
	require.NoError(t, m.Migrate(26))
	check(expected)
}

func TestSystemLibraryDirectoryMigrationPreservesRowsAndCustomDirectories(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(26))
	require.NoError(t, db.Exec(`
INSERT INTO spaces(id,name,business) VALUES(1,'Directories','personal');
INSERT INTO libraries(id,space_id,name,system_role,directory,template,source_evidence_rule,embedding_model_id,summary_model_id) VALUES
('official',1,'Custom official','institutional','institutional','external_timely','link_required','',''),
('media',1,'媒体信息','opinions','opinions','external_timely','link_required','',''),
('reference',1,'参考文档','references','references','external_durable','','',''),
('personal',1,'个人产出','mine','mine','own','','',''),
('ai',1,'AI产出','ai-reports','ai-reports','generated','','',''),
('custom',1,'Custom','','custom-library','own','','','');
INSERT INTO libraries(id,space_id,name,system_role,directory,template,embedding_model_id,summary_model_id,deleted_at) VALUES('custom-personal',1,'Custom personal','mine','custom-personal','own','','',now());
INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,parse_status,enable_status,vault_body,vault_path,metadata) VALUES
('official-entry',1,'official','manual','Official','https://example.com/institutional/source','article','completed','enabled','Original institutional body','institutional/entry.md','{"directory":"institutional"}'),
('media-entry',1,'media','manual','Media','opinions/nested/entry.md','article','completed','enabled','Media body','opinions/nested/entry.md','{}'),
('personal-entry',1,'personal','manual','Personal','mine/entry.md','mine','completed','enabled','Personal body','mine/entry.md','{}'),
('ai-entry',1,'ai','manual','AI','ai-reports/entry.md','report','completed','enabled','AI body','ai-reports/entry.md','{}'),
('custom-entry',1,'custom','manual','Custom','custom-library/entry.md','mine','completed','enabled','Custom body','custom-library/entry.md','{}'),
('custom-personal-entry',1,'custom-personal','manual','Custom personal','custom-personal/entry.md','mine','completed','enabled','Custom personal body','custom-personal/entry.md','{}');
`).Error)
	before := baselineContents(t, db)
	expected := map[string]string{}
	for table, raw := range before {
		expected[table] = raw
	}
	renames := map[string]string{"institutional": "official", "opinions": "media", "mine": "personal", "ai-reports": "ai-output"}
	for _, table := range []string{"libraries", "entries"} {
		var rows []map[string]any
		require.NoError(t, json.Unmarshal([]byte(expected[table]), &rows))
		for _, row := range rows {
			if table == "libraries" {
				if current, ok := renames[row["system_role"].(string)]; ok {
					if row["directory"] == row["system_role"] {
						row["directory"] = current
					}
					row["system_role"] = current
				}
			} else {
				old := row["vault_path"].(string)
				for from, to := range renames {
					if len(old) > len(from) && old[:len(from)+1] == from+"/" {
						row["vault_path"] = to + old[len(from):]
						if row["source"] == old {
							row["source"] = row["vault_path"]
						}
					}
				}
			}
		}
		raw, err := json.Marshal(rows)
		require.NoError(t, err)
		expected[table] = string(raw)
	}
	check := func(want map[string]string) {
		t.Helper()
		actual := baselineContents(t, db)
		require.Len(t, actual, len(want))
		for table, rows := range want {
			require.JSONEq(t, rows, actual[table], table)
		}
	}
	require.NoError(t, m.Migrate(27))
	check(expected)
	require.Error(t, db.Exec(`UPDATE libraries SET directory='changed' WHERE id='personal'`).Error)
	require.NoError(t, m.Migrate(26))
	check(before)
	require.NoError(t, m.Migrate(27))
	check(expected)
}

func TestSystemLibraryDirectoryMigrationRejectsOccupiedDirectory(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(26))
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Collision','personal'); INSERT INTO libraries(id,space_id,name,system_role,directory,template,embedding_model_id,summary_model_id) VALUES('personal',1,'Personal','mine','mine','own','',''),('custom',1,'Occupied','','personal','own','',''); INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,vault_path) VALUES('entry',1,'personal','manual','Personal','mine/entry.md','mine','mine/entry.md')`).Error)
	before := baselineContents(t, db)
	require.ErrorContains(t, m.Migrate(27), "idx_libraries_directory")
	after := baselineContents(t, db)
	for table, rows := range before {
		require.JSONEq(t, rows, after[table], table)
	}
	require.Error(t, db.Exec(`UPDATE libraries SET directory='changed' WHERE id='personal'`).Error)
}

func TestLibraryReviewRoutingMigrationPreservesCustomPoliciesAndModels(t *testing.T) {
	db := Open(t)
	directory, err := filepath.Abs("../../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(29))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business) VALUES(1,'Owner','personal');
 INSERT INTO libraries(id,space_id,name,template,system_role,directory,source_evidence_rule,review_criteria,summary_model_id,embedding_model_id,model_tiers) VALUES
 ('routing-official',1,'Official','external_timely','official','routing-official','link_or_publisher_date','发布者是机构（官方、公司、媒体、研究机构、行业协会）；有实质信息，不是广告、招聘、纯转发；正文完整。','old-generation','','{"light":"old-review","entity_resolution":"old-entity","generation":"old-generation","rerank":"old-rerank"}'),
 ('routing-media',1,'Media','external_timely','media','routing-media','link_required','My custom review policy','','','{}'),
 ('routing-reference',1,'Reference','external_durable','references','routing-reference','','长期有参考价值；正文完整，不是摘要或目录。','','','{"light":"reference-review"}');
 `).Error)
	require.NoError(t, m.Migrate(30))
	var criteria string
	require.NoError(t, db.Raw("SELECT review_criteria FROM libraries WHERE id='routing-official'").Scan(&criteria).Error)
	const version30OfficialCriteria = "发布者是机构（官方、公司、媒体、研究机构、行业协会）；至少包含一个可明确识别的事实、变化或研究结论。产品发布、调价、活动、人员变动和有过程及结果的客户案例可收录；研究摘要可作为独立发布页收录，不要求包含链接论文全文。排除纯口号、空导流、招聘和无新增信息的转发。完整性以提交的独立发布内容为准：缺失内容影响主要事实理解时待审，否则保留并提示缺失。"
	require.Equal(t, version30OfficialCriteria, criteria, "frozen migration must not depend on current template text")
	require.NoError(t, db.Raw("SELECT review_criteria FROM libraries WHERE id='routing-media'").Scan(&criteria).Error)
	require.Equal(t, "My custom review policy", criteria)
	var models types.ModelTierConfig
	require.NoError(t, db.Raw("SELECT model_tiers FROM libraries WHERE id='routing-media'").Row().Scan(&models))
	require.Equal(t, "old-review", models.Light)
	require.Equal(t, "old-generation", models.Generation)
	require.Equal(t, "old-entity", models.EntityResolution)
	require.Equal(t, "old-rerank", models.Rerank)
	require.NoError(t, db.Raw("SELECT model_tiers FROM libraries WHERE id='routing-reference'").Row().Scan(&models))
	require.Equal(t, "reference-review", models.Light)
	require.NoError(t, m.Migrate(29))
	require.NoError(t, db.Raw("SELECT model_tiers FROM libraries WHERE id='routing-media'").Row().Scan(&models))
	require.Equal(t, "old-generation", models.Generation, "rollback retains explicit bindings")
}
