package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/redistest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/application/service/file"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

func TestDiscardReleasesOriginalIngestAssetsAndKeepsURLLedger(t *testing.T) {
	for _, mode := range []string{"delete", "review_discard", "delete_list", "shared_owner"} {
		t.Run(mode, func(t *testing.T) {
			h, _ := newIngestHarness(t)
			uploads := t.TempDir()
			h.service.resourceCatalog = NewResourceCatalog(repository.NewResourceRepository(h.db))
			h.service.fileSvc = file.NewResourceCatalogFileService(file.NewLocalFileService(uploads, ""), h.service.resourceCatalog)
			queue := h.service.task
			h.service.task = redistest.NewClient(t, h.db)
			body := "# Collected evidence\n\nAcme reports the original measurements and documented instrument calibration procedure."
			asset, err := h.service.UploadIngestAsset(h.ctx, "original.txt", []byte(body))
			require.NoError(t, err)
			h.service.task = queue
			var original types.IngestAsset
			require.NoError(t, h.db.First(&original, "id=?", asset.AssetID).Error)
			physical, _, err := h.service.resourceCatalog.ResolvePath(h.ctx, original.Path)
			require.NoError(t, err)
			abs := filepath.Join(uploads, filepath.FromSlash(strings.TrimPrefix(physical, "local://")))
			_, err = os.Stat(abs)
			require.NoError(t, err)
			var cleanup taskqueue.Job
			require.NoError(t, h.db.Where("task_type=?", types.TypeIngestAssetCleanup).First(&cleanup).Error)
			url := "https://example.test/asset-release/" + mode
			h.processor.reject = mode == "review_discard"
			result, err := h.service.Ingest(h.ctx, &types.IngestRequest{URL: url, Content: body, Acquisition: "verbatim", Publisher: "Acme", File: &types.IngestFile{AssetID: asset.AssetID}})
			require.NoError(t, err)
			if mode == "review_discard" {
				require.Equal(t, "pending_review", result.Status)
			} else {
				h.drain(t)
			}
			require.NoError(t, h.db.First(&original, "id=?", asset.AssetID).Error)
			require.Equal(t, result.EntryID, original.EntryID)
			require.NotNil(t, original.ReferencedAt)
			if mode == "shared_owner" {
				other := &types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "file", Title: "Other retained owner", SourceKind: "mine", FilePath: original.Path, FileType: "txt"}
				require.NoError(t, h.db.Create(other).Error)
				require.NoError(t, h.service.resourceCatalog.Bind(h.ctx, original.Path, types.ResourceOwnerEntry, other.ID, types.ResourceRelationSourceFile))
			}
			switch mode {
			case "review_discard":
				_, err = h.service.ReviewEntryIntake(h.ctx, result.EntryID, "discard", types.SourceInfo{})
			case "delete_list":
				err = h.service.DeleteEntryList(h.ctx, []string{result.EntryID})
			default:
				err = h.service.DeleteEntry(h.ctx, result.EntryID)
			}
			require.NoError(t, err)
			var count int64
			require.NoError(t, h.db.Model(&types.IngestAsset{}).Where("id=?", asset.AssetID).Count(&count).Error)
			require.Zero(t, count)
			_, err = os.Stat(abs)
			if mode == "shared_owner" {
				require.NoError(t, err)
			} else {
				require.True(t, os.IsNotExist(err), "original upload must be removed after article discard: %v", err)
			}
			checks, err := h.service.CheckURLs(h.ctx, []string{url})
			require.NoError(t, err)
			require.Equal(t, "discarded", checks[0].Status)
			require.NoError(t, h.service.HandleIngestAssetCleanup(h.ctx, taskqueue.NewTask(types.TypeIngestAssetCleanup, cleanup.Payload)))
			var payload ingestAssetCleanupPayload
			require.NoError(t, json.Unmarshal(cleanup.Payload, &payload))
			require.Equal(t, asset.AssetID, payload.AssetID)
		})
	}
}
