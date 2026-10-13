package maintenance

import (
	"context"
	"path/filepath"
	"reliquary/internal/testutil/pgtest"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
)

func TestMarkdownMigrationPreservesRetainedData(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := migration.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, migration.Migrate(7))
	require.NoError(t, db.Exec(`
 INSERT INTO spaces(id,name,business,parser_engine_config) VALUES(1,'Owner','personal','{"endpoint":"https://example.org/retired"}');
 INSERT INTO libraries(id,space_id,name,embedding_model_id,summary_model_id,chunking_config) VALUES('library',1,'Library','','','{"chunk_size":700,"parser_engine_rules":[],"table_metadata_instructions":"retired"}');
 INSERT INTO entries(id,space_id,library_id,type,title,source,source_kind,content_type,processing_mode,vault_body,vault_original_path,ingest_attachments,content_type_uncertain,processing_review_status,processing_review_content_type,collect_reason,starred,structured_summary,metadata,missing_materials,incomplete) VALUES
 ('full',1,'library','manual','Full source','','article','research','full','Retained Markdown body','attachments/full/old.pdf','[]',true,'pending','research','User reason',true,'{"overview":"Summary","points":["one","two","three"],"key_terms":[]}','{"user_key":"keep","process_overrides":{"parser_engine_rules":[],"chunking_config":{"chunk_size":700}}}','[{"type":"image","source_url":"https://example.org/img","reason":"missing"},{"type":"file","reason":"retired"}]',true),
 ('summary',1,'library','manual','Old tutorial','','article','tutorial','summary_only','','','[]',false,'','','Reference reason',false,NULL,'{}','[]',false),
 ('unknown',1,'library','manual','Unclassified source','','article','','full','Full unknown body','','[]',true,'pending','','',false,NULL,'{}','[{"type":"content","reason":"retired"}]',true);
 UPDATE entries SET parse_status='completed',enable_status='enabled',follow_ready_at=NULL WHERE id='full';
 INSERT INTO topics(id,space_id,name,description) VALUES('topic',1,'Topic','Preserved description');
 INSERT INTO entry_topics(space_id,entry_id,topic_id) VALUES(1,'full','topic');
 INSERT INTO operation_logs(id,space_id,actor,operation_type,object_id,before_value,after_value) VALUES('log',1,'user:fixture','processing_review_ignore','full','{}','{"note":"preserve historical audit"}');
 INSERT INTO ingest_assets(id,space_id,name,file_type,size,path,created_at) VALUES('image',1,'image.png','png',3,'image.png',now()),('original',1,'old.pdf','pdf',3,'old.pdf',now());
 `).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	beforeManifest, err := BuildManifest(ctx, db, t.TempDir(), "")
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(8))
	require.NoError(t, ValidateClassification(ctx, db))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	projectMarkdownSnapshot(before)
	require.Equal(t, before.Tables, after.Tables)
	afterManifest, err := BuildManifest(ctx, db, t.TempDir(), "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(beforeManifest, afterManifest))
	require.NoError(t, db.Exec(`UPDATE entries SET title='Tampered' WHERE id='full'`).Error)
	changed, err := BuildManifest(ctx, db, t.TempDir(), "")
	require.NoError(t, err)
	require.NotEmpty(t, CompareManifest(beforeManifest, changed))
	require.NoError(t, migration.Migrate(7))
	require.NoError(t, migration.Migrate(8))
}
