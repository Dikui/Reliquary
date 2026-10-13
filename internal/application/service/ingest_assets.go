package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reliquary/internal/infrastructure/docparser"
	"strings"
	"time"

	werrors "reliquary/internal/errors"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ingestAssetLifetime = 24 * time.Hour

type ingestAssetCleanupPayload struct {
	SpaceID uint64 `json:"space_id"`
	AssetID string `json:"asset_id"`
}

// UploadIngestAsset uses the same extension whitelist and upload limit as the
// existing file importer. Its queue job survives process restarts.
func (s *entryService) UploadIngestAsset(ctx context.Context, name string, data []byte) (*types.IngestAssetResult, error) {
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.IngestAssetResult, error) {
			return copy.UploadIngestAsset(ctx, name, data)
		})
	}
	var identityErr error
	ctx, _, _, identityErr = s.ingestIdentity(ctx)
	if identityErr != nil {
		return nil, identityErr
	}
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok || space == 0 {
		return nil, werrors.NewUnauthorizedError("authenticated collector required")
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated collector required")
	}
	if s.db == nil || s.fileSvc == nil || s.task == nil {
		return nil, werrors.NewInternalServerError("ingest asset storage is unavailable")
	}
	name = filepath.Base(strings.ReplaceAll(strings.TrimSpace(name), "\\", "/"))
	if name == "." || name == "" {
		return nil, werrors.NewBadRequestError("附件文件名不能为空")
	}
	fileType := normalizeFileExtension(getFileType(name))
	if !docparser.IsImageFormat(fileType) || !strings.HasPrefix(http.DetectContentType(data), "image/") {
		return nil, werrors.NewBadRequestError("only image attachments are accepted; convert the source to Markdown before ingestion")
	}
	if len(data) == 0 {
		return nil, werrors.NewBadRequestError("附件内容不能为空")
	}
	if int64(len(data)) > utils.GetMaxFileSize() {
		return nil, werrors.NewBadRequestError(fmt.Sprintf("文件大小不能超过%dMB", utils.GetMaxFileSizeMB()))
	}
	path, err := s.fileSvc.SaveBytes(ctx, data, space, name, false)
	if err != nil {
		return nil, err
	}
	asset := &types.IngestAsset{ID: uuid.NewString(), SpaceID: space, Name: name, Type: fileType, Size: int64(len(data)), Path: path, CreatedAt: time.Now().UTC()}
	if err = s.db.WithContext(ctx).Create(asset).Error; err != nil {
		_ = s.fileSvc.DeleteFile(ctx, path)
		return nil, err
	}
	payload, _ := json.Marshal(ingestAssetCleanupPayload{SpaceID: space, AssetID: asset.ID})
	_, err = s.task.Enqueue(taskqueue.NewTask(types.TypeIngestAssetCleanup, payload), taskqueue.Queue(types.QueueMaintenance), taskqueue.ProcessAt(asset.CreatedAt.Add(ingestAssetLifetime)), taskqueue.MaxRetry(5), taskqueue.Timeout(5*time.Minute))
	if err != nil {
		_ = s.db.WithContext(ctx).Delete(asset).Error
		_ = s.fileSvc.DeleteFile(ctx, path)
		return nil, err
	}
	return &types.IngestAssetResult{AssetID: asset.ID, Type: asset.Type, Size: asset.Size, Name: asset.Name}, nil
}

func (s *entryService) loadIngestAsset(ctx context.Context, id string) (*types.IngestAsset, []byte, error) {
	var identityErr error
	ctx, _, _, identityErr = s.ingestIdentity(ctx)
	if identityErr != nil {
		return nil, nil, identityErr
	}
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok || space == 0 {
		return nil, nil, werrors.NewUnauthorizedError("authenticated collector required")
	}
	if s.db == nil || s.fileSvc == nil {
		return nil, nil, werrors.NewInternalServerError("ingest asset storage is unavailable")
	}
	var asset types.IngestAsset
	if err := s.db.WithContext(ctx).Where("id=? AND space_id=?", id, space).First(&asset).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil, werrors.NewBadRequestError("附件不存在或已过期")
		}
		return nil, nil, err
	}
	if asset.ReferencedAt == nil && !asset.CreatedAt.Add(ingestAssetLifetime).After(time.Now()) {
		return nil, nil, werrors.NewBadRequestError("附件已过期，请重新上传")
	}
	if err := s.validateLocalIngestAsset(ctx, asset.Path); err != nil {
		return nil, nil, err
	}
	reader, err := s.fileSvc.GetFile(ctx, asset.Path)
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(io.LimitReader(reader, utils.GetMaxFileSize()+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) != asset.Size || int64(len(data)) > utils.GetMaxFileSize() {
		return nil, nil, werrors.NewBadRequestError("附件大小与上传记录不符")
	}
	return &asset, data, nil
}

func (s *entryService) ResolveIngestAsset(ctx context.Context, id string) (*types.IngestAsset, []byte, error) {
	return s.loadIngestAsset(ctx, id)
}

func (s *entryService) referenceIngestAsset(ctx context.Context, id, entryID string) error {
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok || space == 0 {
		return werrors.NewUnauthorizedError("authenticated collector required")
	}
	now := time.Now().UTC()
	result := s.db.WithContext(ctx).Model(&types.IngestAsset{}).
		Where("id=? AND space_id=? AND ((referenced_at IS NULL AND created_at>?) OR entry_id=?)", id, space, now.Add(-ingestAssetLifetime), entryID).
		Updates(map[string]any{"referenced_at": now, "entry_id": entryID})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return werrors.NewBadRequestError("附件不存在、已过期或已归入其他文章")
	}
	return nil
}

func (s *entryService) ReferenceIngestAsset(ctx context.Context, id, entryID string) error {
	return s.referenceIngestAsset(ctx, id, entryID)
}

// CleanupIngestAssets accepts a clock value so the 24 hour boundary can be
// tested without waiting. PostgreSQL row locks serialize cleanup and claims.
func (s *entryService) CleanupIngestAssets(ctx context.Context, now time.Time) error {
	return s.cleanupIngestAssets(ctx, now, "")
}

func (s *entryService) cleanupIngestAssets(ctx context.Context, now time.Time, id string) error {
	if s.db == nil || s.fileSvc == nil {
		return errors.New("ingest asset cleanup is unavailable")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		query := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("referenced_at IS NULL AND created_at<=?", now.UTC().Add(-ingestAssetLifetime)).Order("id")
		if space, ok := types.SpaceIDFromContext(ctx); ok {
			query = query.Where("space_id=?", space)
		}
		if id != "" {
			query = query.Where("id=?", id)
		}
		var assets []types.IngestAsset
		if err := query.Find(&assets).Error; err != nil {
			return err
		}
		for _, asset := range assets {
			if err := s.validateLocalIngestAsset(ctx, asset.Path); err != nil {
				return err
			}
			if err := s.fileSvc.DeleteFile(ctx, asset.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			if err := tx.Delete(&asset).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *entryService) validateLocalIngestAsset(ctx context.Context, path string) error {
	if strings.HasPrefix(path, "resource://") && s.resourceCatalog != nil {
		physical, _, err := s.resourceCatalog.ResolvePath(ctx, path)
		if err != nil {
			return err
		}
		path = physical
	}
	if !strings.HasPrefix(path, "local://") {
		return werrors.NewBadRequestError("收录附件必须来自本地文件")
	}
	return nil
}

func (s *entryService) HandleIngestAssetCleanup(ctx context.Context, task *taskqueue.Task) error {
	var payload ingestAssetCleanupPayload
	if err := json.Unmarshal(task.Payload(), &payload); err != nil {
		return fmt.Errorf("invalid asset cleanup payload: %w", taskqueue.SkipRetry)
	}
	if payload.AssetID == "" || payload.SpaceID == 0 {
		return fmt.Errorf("missing asset cleanup identity: %w", taskqueue.SkipRetry)
	}
	ctx = context.WithValue(ctx, types.SpaceIDContextKey, payload.SpaceID)
	return s.cleanupIngestAssets(ctx, time.Now().UTC(), payload.AssetID)
}

// releaseIngestArticleAssets removes uploaded images retained alongside
// article-owned copies. Other resource owners keep their shared bytes.
func (s *entryService) releaseIngestArticleAssets(ctx context.Context, entryID string) error {
	if s.db == nil {
		return nil
	}
	space, ok := types.SpaceIDFromContext(ctx)
	if !ok || space == 0 {
		return werrors.NewUnauthorizedError("authenticated article owner required")
	}
	var assets []types.IngestAsset
	if err := s.db.WithContext(ctx).Where("space_id=? AND entry_id=?", space, entryID).Order("id").Find(&assets).Error; err != nil {
		return err
	}
	if len(assets) == 0 {
		return nil
	}
	if s.fileSvc == nil {
		return errors.New("ingest asset storage is unavailable")
	}
	releaser := entryResourceOwners(s.resourceCatalog, entryID)
	for _, asset := range assets {
		if err := s.validateLocalIngestAsset(ctx, asset.Path); err != nil {
			return err
		}
		if releaser.deletable(ctx, asset.Path) {
			if err := s.fileSvc.DeleteFile(ctx, asset.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
		if err := s.db.WithContext(ctx).Delete(&asset).Error; err != nil {
			return err
		}
	}
	return nil
}
