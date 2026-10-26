package maintenance

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"reliquary/internal/testutil/pgtest"
)

func TestLibraryPromptsMigrationPreservesReplicaAndDetectsChanges(t *testing.T) {
	ctx := context.Background()
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	m, err := migrate.New("file://"+directory, db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	t.Cleanup(func() { a, b := m.Close(); require.NoError(t, a); require.NoError(t, b) })
	require.NoError(t, m.Migrate(46))
	seed(t, db)
	require.NoError(t, db.Exec(`INSERT INTO libraries(id,name,space_id,embedding_model_id,summary_model_id,chunking_config,indexing_strategy,template,directory,deleted_at)
 VALUES('second','Second',1,'embed','chat','{}','{}','own','second',NULL),('deleted','Deleted',1,'embed','chat','{}','{}','own','deleted',now());
 INSERT INTO system_settings(key,value,value_type,category,description,last_modified_by,created_at,updated_at) VALUES
 ('prompt.article_processing','"Custom understanding"','string','prompts','Original description','owner','2026-01-02T00:00:00Z','2026-02-03T00:00:00Z'),
 ('prompt.summary_merge','"Custom summary"','string','prompts','','owner','2026-01-02T00:00:00Z','2026-02-03T00:00:00Z'),
 ('prompt.evaluation_questions','"Custom questions"','string','prompts','','owner','2026-01-02T00:00:00Z','2026-02-03T00:00:00Z'),
 ('prompt.library.library.summary_merge','"Already scoped"','string','prompts','','owner','2026-01-02T00:00:00Z','2026-02-03T00:00:00Z'),
 ('unrelated.setting','{"keep":true}','string','general','','owner','2026-01-02T00:00:00Z','2026-02-03T00:00:00Z')`).Error)
	before, err := Capture(ctx, db)
	require.NoError(t, err)
	root := t.TempDir()
	require.NoError(t, WriteSnapshot(root, "", before))
	oldManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.NoError(t, m.Migrate(47))
	after, err := Capture(ctx, db)
	require.NoError(t, err)
	projected := &Snapshot{schemaVersion: 46, Tables: map[string][]map[string]any{
		"system_settings": before.Tables["system_settings"], "libraries": before.Tables["libraries"],
	}}
	projectLibraryPromptsSnapshot(projected, 47)
	require.Equal(t, projected.Tables["system_settings"], after.Tables["system_settings"])
	require.Len(t, after.Tables["system_settings"], len(before.Tables["system_settings"])+6)
	for _, original := range before.Tables["system_settings"] {
		require.Contains(t, after.Tables["system_settings"], original, "existing rows must be unchanged")
	}
	require.NoError(t, WriteSnapshot(root, "", after))
	newManifest, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Empty(t, CompareManifest(oldManifest, newManifest))

	oldRoot := t.TempDir()
	require.NoError(t, WriteSnapshot(oldRoot, "", before))
	target := pgtest.Open(t)
	_, err = Import(ctx, target, oldRoot, "", nil)
	require.NoError(t, err)
	require.NoError(t, VerifyImportedReplica(ctx, target, oldRoot))
	require.NoError(t, target.Exec(`UPDATE system_settings SET value='"tampered"' WHERE key='prompt.library.second.article_processing'`).Error)
	require.ErrorContains(t, VerifyImportedReplica(ctx, target, oldRoot), "system_settings")

	require.NoError(t, db.Exec(`UPDATE system_settings SET value='"tampered"' WHERE key='prompt.library.second.article_processing'`).Error)
	corrupt, err := BuildManifest(ctx, db, root, "")
	require.NoError(t, err)
	require.Contains(t, CompareManifest(oldManifest, corrupt), "table system_settings")
	require.Contains(t, CompareManifest(newManifest, corrupt), "table system_settings", "current snapshots remain strict")
}
