package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func newIngestAssetHarness(t *testing.T, catalog bool) (*entryService, context.Context, string) {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Assets fixture','personal'); INSERT INTO api_keys(id,space_id,name,key_hash) VALUES(1,1,'Asset collector','asset-fixture-key-hash')`).Error)
	root := t.TempDir()
	files := file.NewLocalFileService(root, "")
	s := &entryService{db: db, fileSvc: files, task: taskqueue.NewClient(db)}
	if catalog {
		s.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(db))
		s.fileSvc = file.NewResourceCatalogFileService(files, s.resourceCatalog)
	}
	ctx := types.WithPrincipal(context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1)), types.Principal{Type: types.PrincipalAPIKey, ID: "1"})
	return s, ctx, root
}

func TestIngestAssetsUploadReferenceAndClockedExpiry(t *testing.T) {
	for _, catalog := range []bool{false, true} {
		t.Run(map[bool]string{false: "physical", true: "resource_catalog"}[catalog], func(t *testing.T) {
			s, ctx, root := newIngestAssetHarness(t, catalog)
			keep, err := s.UploadIngestAsset(ctx, "kept.png", ingestAssetTestPNG(t))
			require.NoError(t, err)
			remove, err := s.UploadIngestAsset(ctx, "expired.png", ingestAssetTestPNG(t))
			require.NoError(t, err)
			asset, data, err := s.loadIngestAsset(ctx, keep.AssetID)
			require.NoError(t, err)
			require.Equal(t, ingestAssetTestPNG(t), data)
			require.Equal(t, "png", keep.Type)
			require.EqualValues(t, len(data), keep.Size)
			require.NoError(t, s.referenceIngestAsset(ctx, keep.AssetID, "article-1"))
			require.Error(t, s.referenceIngestAsset(ctx, keep.AssetID, "article-2"))
			var doomed types.IngestAsset
			require.NoError(t, s.db.First(&doomed, "id=?", remove.AssetID).Error)
			var jobs []taskqueue.Job
			require.NoError(t, s.db.Where("task_type=?", types.TypeIngestAssetCleanup).Find(&jobs).Error)
			require.Len(t, jobs, 2)
			for _, job := range jobs {
				require.Equal(t, types.QueueMaintenance, job.Queue)
				var payload ingestAssetCleanupPayload
				require.NoError(t, json.Unmarshal(job.Payload, &payload))
				var uploaded types.IngestAsset
				require.NoError(t, s.db.First(&uploaded, "id=?", payload.AssetID).Error)
				require.WithinDuration(t, uploaded.CreatedAt.Add(ingestAssetLifetime), job.ProcessAt, time.Microsecond)
			}
			require.NoError(t, s.CleanupIngestAssets(ctx, doomed.CreatedAt.Add(24*time.Hour-time.Microsecond)))
			var count int64
			require.NoError(t, s.db.Model(&types.IngestAsset{}).Count(&count).Error)
			require.EqualValues(t, 2, count)
			require.NoError(t, s.CleanupIngestAssets(ctx, doomed.CreatedAt.Add(24*time.Hour)))
			require.NoError(t, s.db.Model(&types.IngestAsset{}).Count(&count).Error)
			require.EqualValues(t, 1, count)
			_, data, err = s.loadIngestAsset(ctx, keep.AssetID)
			require.NoError(t, err)
			require.Equal(t, ingestAssetTestPNG(t), data)
			_, _, err = s.loadIngestAsset(ctx, remove.AssetID)
			require.Error(t, err)
			var files []string
			require.NoError(t, filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
				if err == nil && !entry.IsDir() {
					files = append(files, path)
				}
				return err
			}))
			require.Len(t, files, 1)
			require.NoError(t, s.db.Exec(`UPDATE api_keys SET revoked_at=now() WHERE id=1`).Error)
			_, err = s.UploadIngestAsset(ctx, "revoked.txt", []byte("refused"))
			require.Error(t, err)
			_, _, err = s.loadIngestAsset(ctx, asset.ID)
			require.Error(t, err)
		})
	}
}

func TestIngestAssetsRejectUnsupportedAndOversize(t *testing.T) {
	s, ctx, _ := newIngestAssetHarness(t, false)
	t.Setenv("MAX_FILE_SIZE_MB", "1")
	for _, item := range []struct {
		name string
		body []byte
	}{{"script.exe", []byte("unsupported")}, {"empty.pdf", nil}, {"too-big.pdf", []byte(strings.Repeat("x", 1024*1024+1))}} {
		_, err := s.UploadIngestAsset(ctx, item.name, item.body)
		require.Error(t, err)
	}
	var count int64
	require.NoError(t, s.db.Model(&types.IngestAsset{}).Count(&count).Error)
	require.Zero(t, count)
	require.NoError(t, s.db.Model(&taskqueue.Job{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestIngestAssetCleanupWaitsForRolledBackReference(t *testing.T) {
	s, ctx, _ := newIngestAssetHarness(t, false)
	result, err := s.UploadIngestAsset(ctx, "claimed.png", ingestAssetTestPNG(t))
	require.NoError(t, err)
	tx := s.db.Begin()
	require.NoError(t, tx.Error)
	t.Cleanup(func() { _ = tx.Rollback().Error })
	copy := *s
	copy.db = tx
	require.NoError(t, copy.referenceIngestAsset(ctx, result.AssetID, "uncommitted-article"))
	done := make(chan error, 1)
	go func() { done <- s.CleanupIngestAssets(ctx, time.Now().Add(24*time.Hour)) }()
	require.Eventually(t, func() bool {
		var waiting int64
		if err := s.db.Raw(`SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%ingest_assets%'`).Scan(&waiting).Error; err != nil {
			return false
		}
		return waiting > 0
	}, 3*time.Second, 10*time.Millisecond, "expiry job must wait for the pending claim rather than skip its only cleanup")
	require.NoError(t, tx.Rollback().Error)
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("expiry cleanup did not resume after claim rollback")
	}
	var count int64
	require.NoError(t, s.db.Model(&types.IngestAsset{}).Where("id=?", result.AssetID).Count(&count).Error)
	require.Zero(t, count)
}

func ingestAssetTestPNG(t *testing.T) []byte {
	t.Helper()
	data, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII=")
	require.NoError(t, err)
	return data
}
