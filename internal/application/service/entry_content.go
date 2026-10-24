package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"strings"
	"time"

	werrors "reliquary/internal/errors"

	"gorm.io/gorm/clause"
)

// UpdateEntryContent replaces an explicitly selected entry.
func (s *entryService) UpdateEntryContent(ctx context.Context, id string, request *types.EntryContentUpdate) (result *types.IngestResult, err error) {
	if request == nil {
		return nil, werrors.NewBadRequestError("content is required")
	}
	ctx, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	if s.vault == nil {
		return nil, werrors.NewServiceUnavailableError("file library is unavailable")
	}
	err = s.withTaskObject(ctx, id, func(ctx context.Context) error {
		changes := &ingestFileChanges{}
		ctx = context.WithValue(ctx, ingestFileChangesKey{}, changes)
		result, err = entryTaskTransaction(ctx, s, func(copy *entryService) (*types.IngestResult, error) {
			return copy.updateEntryContent(ctx, id, *request, keyID, keyName)
		})
		if err != nil {
			return errors.Join(err, changes.rollback(s.vault))
		}
		return changes.finish()
	})
	if err == nil {
		s.vault.ScheduleRefresh(ctx, id)
	}
	return result, err
}

func (s *entryService) updateEntryContent(ctx context.Context, id string, request types.EntryContentUpdate, keyID uint64, keyName string) (*types.IngestResult, error) {
	content, acquisition := request.Content, request.Acquisition
	spaceID, _ := types.SpaceIDFromContext(ctx)
	entry, err := s.repo.GetEntryByID(ctx, spaceID, id)
	if err != nil {
		return nil, err
	}
	// Serialize writes to the library without excluding foreign-key readers.
	var library types.Library
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).Where("id=? AND space_id=?", entry.LibraryID, spaceID).First(&library).Error; err != nil {
		return nil, err
	}
	if err := lockMessageDeduplication(s.db.WithContext(ctx), entry); err != nil {
		return nil, err
	}
	if err := lockEntitySpace(s.db.WithContext(ctx), spaceID); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", id, spaceID).First(entry).Error; err != nil {
		return nil, err
	}
	if acquisition == "" {
		acquisition = entry.Acquisition
	}
	// Update matches literal Markdown image targets, including relative paths.
	// Retain ingestion's file/data validation without its HTTP-only source rule.
	images := append([]types.IngestImage(nil), request.Images...)
	for i := range images {
		images[i].SourceURL, images[i].URL = "", ""
	}
	if err := validateIngestRequest(&types.IngestRequest{Library: library.ID, Content: content, Acquisition: acquisition, Images: images}); err != nil {
		return nil, err
	}
	if ingestInlineSize(request.Images) > types.MaxIngestInlineRequestBytes {
		return nil, werrors.NewBadRequestError("inline images exceed the 20 MiB request limit")
	}
	if entry.VaultPath == "" {
		return nil, werrors.NewConflictError("资料正文尚未保存，请稍后重试")
	}
	if len(request.Images) == 0 && entry.VaultBody == content && entry.Acquisition == acquisition {
		return ingestResult(entry, "exists", "正文未变化。", "", ""), nil
	}
	if request.BaseBodyHash != "" {
		reader, _, err := s.getEntryBody(ctx, entry)
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if err := errors.Join(readErr, closeErr); err != nil {
			return nil, err
		}
		if fmt.Sprintf("%x", sha256.Sum256(body)) != request.BaseBodyHash {
			return nil, werrors.NewConflictError("正文已被修改，请重新读取后再保存").WithDetails(map[string]string{"reason": "body_changed"})
		}
	}
	switch entry.ParseStatus {
	case types.ParseStatusDeleting:
		return nil, werrors.NewConflictError("资料正在删除，请稍后重试")
	}
	busy, err := s.libraryBatchBusy(ctx, spaceID, library.ID, "")
	if err != nil {
		return nil, err
	}
	if busy {
		return nil, werrors.NewConflictError("所属库有未完成任务，请完成或取消后再更新")
	}
	content, canonical, err := s.prepareEntryContentImages(ctx, entry, content, request.Images)
	if err != nil {
		return nil, err
	}
	for _, ref := range articleImagesInOrder(content, nil) {
		if remoteIngestImage(ref) {
			continue
		}
		if !isArticleAttachmentReference(id, entry.VaultPath, ref) {
			return nil, werrors.NewBadRequestError("正文中的图片只支持本资料已有附件或远程图片链接；新图片请通过 images 提交")
		}
		image, err := s.vault.ReadImage(entry, ref)
		if err != nil {
			return nil, werrors.NewBadRequestError("正文引用的本地图片不可读，请保留有效附件或通过 images 提交图片")
		}
		if err := image.Close(); err != nil {
			return nil, err
		}
	}
	if entry.VaultBody == content && entry.Acquisition == acquisition {
		return ingestResult(entry, "exists", "正文和图片未变化。", "", ""), nil
	}
	for _, image := range request.Images {
		if image.AssetID != "" {
			if err := s.referenceIngestAsset(ctx, image.AssetID, id); err != nil {
				return nil, err
			}
		}
	}
	before := map[string]any{"content_hash": entry.ContentHash, "acquisition": entry.Acquisition}
	if err := s.cleanupLibraryRebuild(ctx, entry); err != nil {
		return nil, err
	}
	change, err := s.vault.beginArticleContentUpdate(entry, content)
	if err != nil {
		return nil, err
	}
	changes := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges)
	changes.contentUpdates = append(changes.contentUpdates, change)
	entry.Acquisition = acquisition
	entry.LastIngestAPIKeyID, entry.LastIngestAPIKeyName = keyID, keyName
	setMessageFingerprint(entry, canonical)
	resetEntryForReparse(entry, &library)
	entry.UpdatedAt = time.Now()
	{
		intake, err := entry.IntakeMetadata()
		if err != nil {
			return nil, err
		}
		if intake == nil {
			intake = &types.EntryIntakeMetadata{Source: "ingest", SourceInfo: types.SourceInfo{OriginalURL: entry.Source}}
		}
		intake.Decision, intake.Reason, intake.UserRejected = "approved", "", false
		// Published material reads its authoritative body from the vault. Do not
		// create another upload copy or retain a stale pre-publication body path.
		intake.OriginalPath = ""
		if err := entry.SetIntakeMetadata(intake); err != nil {
			return nil, err
		}
		if err := advanceSegmentGeneration(entry, "processing"); err != nil {
			return nil, err
		}
	}
	entry.SourceBodyHash = sourceBodyHash(canonical)
	if err := s.repo.UpdateEntry(ctx, entry); err != nil {
		return nil, err
	}
	if err := s.repo.UpdateEntryColumns(ctx, id, map[string]any{
		"source_body_hash": entry.SourceBodyHash, "pending_subtasks_count": 0,
		"acquisition":            entry.Acquisition,
		"last_ingest_api_key_id": keyID, "last_ingest_api_key_name": keyName,
	}); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", id, before, map[string]any{"action": "update_content", "content_hash": entry.ContentHash, "acquisition": acquisition}); err != nil {
		return nil, err
	}
	_, err = s.enqueueManualProcessing(ctx, entry, content, false)
	if err != nil {
		return nil, err
	}
	entry.LibraryName = library.Name
	return ingestResult(entry, "accepted", "正文已更新，正在后台建立索引和处理。", "", ""), nil
}

func (s *entryService) prepareEntryContentImages(ctx context.Context, entry *types.Entry, body string, images []types.IngestImage) (string, string, error) {
	targets := map[string]bool{}
	for _, ref := range articleImagesInOrder(body, nil) {
		targets[ref] = true
	}
	seen := map[string]bool{}
	for _, image := range images {
		source := ingestImageSource(image)
		if source == "" || !targets[source] || seen[source] {
			return "", "", werrors.NewBadRequestError("每张图片的 source_url 必须唯一，且对应正文中的图片引用")
		}
		seen[source] = true
	}
	// Reuse identical existing bytes so retries do not create random filenames,
	// reprocess unchanged pictures or overwrite an attachment used by old chunks.
	byHash, canonicalTargets := map[string]string{}, map[string]string{}
	refs := append(articleImagesInOrder(entry.VaultBody, nil), articleImagesInOrder(body, nil)...)
	for _, ref := range refs {
		if canonicalTargets[ref] != "" || !isArticleAttachmentReference(entry.ID, entry.VaultPath, ref) {
			continue
		}
		file, err := s.vault.ReadImage(entry, ref)
		if err != nil {
			continue
		}
		hash := sha256.New()
		_, readErr := io.Copy(hash, file)
		closeErr := file.Close()
		if readErr == nil && closeErr == nil {
			identity := fmt.Sprintf("data:sha256,%x", hash.Sum(nil))
			byHash[identity], canonicalTargets[ref] = ref, identity
		}
	}
	mappings := map[string]string{}
	changes := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges)
	for _, image := range images {
		data, err := decodeIngestInline(image.Data)
		name := ""
		if image.AssetID != "" {
			asset, bytes, loadErr := s.loadIngestAsset(ctx, image.AssetID)
			if loadErr != nil {
				return "", "", loadErr
			}
			if asset.EntryID != "" && asset.EntryID != entry.ID {
				return "", "", werrors.NewBadRequestError("附件已归入其他资料")
			}
			name, data, err = asset.Name, bytes, nil
		}
		if err != nil {
			return "", "", werrors.NewBadRequestError(err.Error())
		}
		mimeType := http.DetectContentType(data)
		if len(data) == 0 || !strings.HasPrefix(mimeType, "image/") {
			return "", "", werrors.NewBadRequestError("only image attachments are accepted")
		}
		if _, err := utils.PrepareModelImage(ctx, bytes.NewReader(data)); err != nil {
			return "", "", werrors.NewBadRequestError(err.Error())
		}
		if name == "" {
			name = "image" + vaultImageExtension("", mimeType)
		}
		image.Data = base64.StdEncoding.EncodeToString(data)
		identity := ingestCanonicalImageReference(image)
		ref := byHash[identity]
		if ref == "" {
			ref, err = s.vault.StoreArticleAttachment(entry, name, data)
			if err != nil {
				return "", "", err
			}
			changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article: entry, path: ref})
			byHash[identity] = ref
		}
		source := ingestImageSource(image)
		mappings[source], canonicalTargets[source] = ref, identity
	}
	return replaceIngestImageTargets(body, mappings), replaceIngestImageTargets(body, canonicalTargets), nil
}
