package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"reliquary/internal/application/service"
	"reliquary/internal/config"
	"reliquary/internal/maintenance"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func sourcesAdminFixture(t *testing.T) (*gorm.DB, string, string) {
	t.Helper()
	db := pgtest.Open(t)
	directory, err := filepath.Abs("../../migrations/versioned")
	require.NoError(t, err)
	migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
	require.NoError(t, err)
	require.NoError(t, migration.Migrate(4))
	a, b := migration.Close()
	require.NoError(t, a)
	require.NoError(t, b)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Export owner','personal');
 INSERT INTO data_sources(id,space_id,knowledge_base_id,name,type,config,sync_schedule,status) VALUES('old-source',1,'library','Retained source','rss','{"url":"https://example.test/feed","credentials":{"token":"private-export-token"}}','0 * * * *','active');
 INSERT INTO sync_logs(id,data_source_id,space_id,status,result) VALUES('old-log','old-source',1,'success','{"created":3}');`).Error)
	dsn := db.Dialector.(*postgres.Dialector).DSN
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	password, _ := parsed.User.Password()
	root := t.TempDir()
	for key, value := range map[string]string{"DB_DRIVER": "postgres", "DB_HOST": parsed.Hostname(), "DB_PORT": parsed.Port(), "DB_USER": parsed.User.Username(), "DB_PASSWORD": password, "DB_NAME": parsed.Path[1:], "VAULT_ROOT": root, "LOCAL_STORAGE_BASE_DIR": t.TempDir()} {
		t.Setenv(key, value)
	}
	return db, dsn, root
}
func TestMaintenanceExportSourcesAndMigratePreserveHistory(t *testing.T) {
	db, dsn, root := sourcesAdminFixture(t)
	var output bytes.Buffer
	handled, err := runServerCommand([]string{"maintenance", "export-sources"}, bytes.NewReader(nil), &output)
	require.True(t, handled)
	require.NoError(t, err)
	require.NotContains(t, output.String(), "private-export-token")
	var result maintenance.SourcesExportResult
	require.NoError(t, json.Unmarshal(output.Bytes(), &result))
	require.Equal(t, 1, result.DataSources)
	require.Equal(t, 1, result.SyncLogs)
	payload, err := os.ReadFile(result.Path)
	require.NoError(t, err)
	require.NotContains(t, string(payload), "private-export-token")
	require.Contains(t, string(payload), "Retained source")
	repository, err := filepath.Abs("../..")
	require.NoError(t, err)
	t.Chdir(repository)
	output.Reset()
	require.NoError(t, runMaintenanceDatabase(context.Background(), db, dsn, []string{"migrate"}, bytes.NewReader(nil), &output))
	var version int
	require.NoError(t, db.Table("schema_migrations").Select("version").Scan(&version).Error)
	require.Equal(t, 12, version)
	var paused bool
	require.NoError(t, db.Table("maintenance_state").Select("paused").Scan(&paused).Error)
	require.True(t, paused)
	files, err := filepath.Glob(filepath.Join(root, ".reliquary/exports/*.json"))
	require.NoError(t, err)
	require.Len(t, files, 1, "migrate reuses the completed explicit export")
}
func TestMaintenanceExportFailureLeavesVersionFourPaused(t *testing.T) {
	db, dsn, root := sourcesAdminFixture(t)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".reliquary"), 0700))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".reliquary/exports"), []byte("block export directory"), 0600))
	var output bytes.Buffer
	err := runMaintenanceDatabase(context.Background(), db, dsn, []string{"migrate"}, bytes.NewReader(nil), &output)
	require.Error(t, err)
	var state struct {
		Version int
		Dirty   bool
	}
	require.NoError(t, db.Table("schema_migrations").Take(&state).Error)
	require.Equal(t, 4, state.Version)
	require.False(t, state.Dirty)
	var paused bool
	require.NoError(t, db.Table("maintenance_state").Select("paused").Scan(&paused).Error)
	require.True(t, paused)
	var sources, logs int64
	require.NoError(t, db.Table("data_sources").Count(&sources).Error)
	require.NoError(t, db.Table("sync_logs").Count(&logs).Error)
	require.EqualValues(t, 1, sources)
	require.EqualValues(t, 1, logs)
}

func TestMaintenanceRestoreOldSnapshotRejectsLegacyLayoutAfterOriginalVerification(t *testing.T) {
	repository, err := filepath.Abs("../..")
	require.NoError(t, err)
	for _, version := range []uint{0, 4} {
		t.Run(fmt.Sprintf("version_%d", version), func(t *testing.T) {
			db, _, root := sourcesAdminFixture(t)
			if version == 0 {
				directory, err := filepath.Abs("../../migrations/versioned")
				require.NoError(t, err)
				migration, err := migrate.New("file://"+filepath.ToSlash(directory), db.Dialector.(*postgres.Dialector).DSN)
				require.NoError(t, err)
				require.NoError(t, migration.Migrate(0))
				a, b := migration.Close()
				require.NoError(t, a)
				require.NoError(t, b)
			}
			ctx := context.Background()
			statement := `INSERT INTO knowledge_bases(id,name,space_id,embedding_model_id,summary_model_id,indexing_strategy) VALUES('library','Restore fixture',1,'','','{"keyword_enabled":true,"vector_enabled":false}');
 INSERT INTO entries(id,space_id,knowledge_base_id,type,source_kind,title,source,channel,content_type,processing_mode,parse_status,enable_status,vault_body,metadata) VALUES('b6cfe58c-2eb4-4cf8-99a7-5482c9c9106c',1,'library','url','article','Old RSS article','https://example.test/restore-article','rss','research','full','completed','enabled','Original restore research observations.','{"intake":{"source":"rss","decision":"approved","source_info":{"original_url":"https://example.test/restore-article"}}}');`
			if version == 0 {
				statement = strings.ReplaceAll(strings.ReplaceAll(statement, "space_id", "tenant_id"), "INSERT INTO entries(", "INSERT INTO knowledges(")
			}
			require.NoError(t, db.Exec(statement).Error)
			var article types.Entry
			uploads := os.Getenv("LOCAL_STORAGE_BASE_DIR")
			var original *maintenance.Manifest
			require.NoError(t, maintenance.WithCurrentNames(db, func(current *gorm.DB) error {
				if err := current.First(&article, "id=?", "b6cfe58c-2eb4-4cf8-99a7-5482c9c9106c").Error; err != nil {
					return err
				}
				vault, err := service.NewVaultService(current, &config.Config{Vault: &config.VaultConfig{Root: root}})
				if err != nil {
					return err
				}
				{
					article.VaultPath = "articles/2026-10/old-rss-restore.md"
					path := filepath.Join(root, filepath.FromSlash(article.VaultPath))
					if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
						return err
					}
					header := service.VaultArticleHeader{ID: article.ID, URL: article.Source, Title: article.Title, ContentType: article.ContentType, CollectedAt: article.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano)}
					if err := os.WriteFile(path, append(service.RenderVaultArticleHeader(header), []byte(article.VaultBody)...), 0600); err != nil {
						return err
					}
					if err := current.Table("entries").Where("id=?", article.ID).UpdateColumn("vault_path", article.VaultPath).Error; err != nil {
						return err
					}
				}
				if err := os.WriteFile(filepath.Join(uploads, "original.bin"), []byte("Preserved original attachment"), 0600); err != nil {
					return err
				}
				if err := vault.Flush(ctx); err != nil {
					return err
				}
				original, err = maintenance.BuildManifest(ctx, current, root, uploads)
				return err
			}))
			beforeArticle, err := os.ReadFile(filepath.Join(root, article.VaultPath))
			require.NoError(t, err)
			baseline, err := json.Marshal(original)
			require.NoError(t, err)
			originalPath := filepath.Join(t.TempDir(), "snapshot.json")
			require.NoError(t, os.WriteFile(originalPath, baseline, 0600))
			invoke := func(arguments ...string) error {
				var output bytes.Buffer
				err := runMaintenance(arguments, bytes.NewReader(nil), &output)
				if err != nil {
					t.Log(output.String())
				}
				return err
			}
			require.NoError(t, invoke("verify", "--manifest", originalPath, "--vectors"))
			oldLogPath := filepath.Join(root, ".reliquary/data/sync_logs.json")
			oldLog, err := os.ReadFile(oldLogPath)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(oldLogPath, []byte("[]\n"), 0600))
			require.Error(t, invoke("verify", "--manifest", originalPath, "--vectors"), "an altered old table replica must fail before any flush can overwrite it")
			require.NoError(t, os.WriteFile(oldLogPath, oldLog, 0600))
			attachmentPath := filepath.Join(uploads, "original.bin")
			require.NoError(t, os.WriteFile(attachmentPath, []byte("Altered attachment"), 0600))
			require.Error(t, invoke("verify", "--manifest", originalPath, "--vectors"), "altered copied material must fail the original baseline check")
			require.NoError(t, os.WriteFile(attachmentPath, []byte("Preserved original attachment"), 0600))
			require.NoError(t, invoke("verify", "--manifest", originalPath, "--vectors"))
			require.NoError(t, invoke("export-sources"))
			t.Chdir(repository)
			require.NoError(t, invoke("migrate"))
			require.NoError(t, invoke("reindex"))
			require.Error(t, invoke("verify", "--manifest", originalPath, "--vectors"), "the old sync_logs sidecar cannot be accepted before the controlled current-schema refresh")
			require.ErrorContains(t, invoke("flush"), "entry path must belong to its library directory")
			require.FileExists(t, oldLogPath)
			require.FileExists(t, filepath.Join(root, ".reliquary/config/data_sources.json"))
			afterArticle, err := os.ReadFile(filepath.Join(root, article.VaultPath))
			require.NoError(t, err)
			require.Equal(t, beforeArticle, afterArticle, "rejecting an old layout preserves its source file")
			unchanged, err := os.ReadFile(originalPath)
			require.NoError(t, err)
			require.Equal(t, baseline, unchanged, "restore keeps the original package baseline immutable")
			attachment, err := os.ReadFile(attachmentPath)
			require.NoError(t, err)
			require.Equal(t, []byte("Preserved original attachment"), attachment)
			require.NoError(t, db.First(&article, "id=?", article.ID).Error)
			require.Equal(t, types.ChannelRSS, article.Channel)
			require.Equal(t, "Original restore research observations.", article.VaultBody)
			require.NoError(t, db.Exec("UPDATE entries SET collect_reason='unrelated alteration' WHERE id=?", article.ID).Error)
			require.Error(t, invoke("verify", "--manifest", originalPath, "--vectors"), "refresh cannot weaken historical article comparison")
		})
	}
}
