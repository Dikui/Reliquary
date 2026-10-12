package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/bits"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/types"
)

func ingestMissingContentPending(k *types.Entry) bool {
	intake, _ := k.IntakeMetadata()
	return intake != nil && intake.Decision == "rejected" && intake.Reason == "正文缺失"
}

func ingestComparableBody(body string) string {
	if i := strings.Index(body, "\n\n## 图片\n"); i >= 0 {
		body = body[:i]
	}
	mappings := map[string]string{}
	for _, ref := range docparser.MarkdownImageReferences(body) {
		mappings[ref.URL] = "image"
	}
	body = replaceIngestImageTargets(body, mappings)
	return strings.TrimSpace(body)
}

// Match the existing exact-body and SimHash rules, including the minimum body
// length, ratio window and unchanged configurable distance threshold.
func sameIngestBody(before, after string) bool {
	before, after = ingestComparableBody(before), ingestComparableBody(after)
	bh, bs, bc := messageBodyFingerprint(before)
	ah, as, ac := messageBodyFingerprint(after)
	if bh == ah {
		return true
	}
	return bc >= minimumSimHashRunes && ac >= minimumSimHashRunes && bc >= ac*4/5 && bc <= ac*5/4 && bits.OnesCount64(uint64(bs)^uint64(as)) <= messageSimHashThreshold()
}

func (s *entryService) supplementIngest(ctx context.Context, kb *types.KnowledgeBase, article *types.Entry, request *types.IngestRequest, keyID uint64, keyName string) (*types.IngestResult, error) {
	beforeMissing := article.IngestMissing()
	oldBody, err := s.readIngestBody(ctx, kb, article)
	if err != nil {
		return nil, err
	}
	summarySupplement := article.VaultPath != "" && article.ProcessingMode == types.ProcessingModeSummaryOnly
	replaceContent := hasIngestMissing(beforeMissing, "content", "")
	suppliedContent := strings.TrimSpace(request.Content) != "" && !validIngestURL(strings.TrimSpace(request.Content))
	if suppliedContent && !replaceContent && !sameIngestBody(oldBody, request.Content) {
		return s.storeIngestConflict(ctx, kb, article, request, keyID, keyName)
	}

	files := s.resolveFileService(ctx, kb)
	remaining := []types.IngestMissing{}
	mappings := map[string]string{}
	links := []string{}
	attachments := article.IngestAttachmentRecords()
	intake, _ := article.IntakeMetadata()
	newBody := oldBody
	changed := false
	usedImages := map[int]bool{}
	usedFile := false
	for _, missing := range beforeMissing {
		resolved := false
		switch missing.Type {
		case "content":
			if suppliedContent {
				copy := *request
				copy.Images = nil
				copy.File = nil
				copy.Missing = nil
				newBody = assembleIngestBody(&copy, nil)
				resolved, changed = true, true
			} else if request.File != nil && (request.File.AssetID != "" || request.File.Data != "") {
				asset, data, err := s.readIngestFile(ctx, request.File)
				if err != nil {
					return nil, err
				}
				parsed, err := s.parseIngestDocument(ctx, asset, data)
				if err != nil {
					return nil, werrors.NewBadRequestError("attachment could not be parsed locally: " + err.Error())
				}
				if parsed != nil && strings.TrimSpace(parsed.MarkdownContent) != "" {
					body, images, err := docparser.NewImageResolver().ResolveAndStore(ctx, parsed, files, article.SpaceID)
					if err != nil {
						return nil, err
					}
					newBody = body
					for _, image := range images {
						intake.Images = append(intake.Images, image.ServingURL)
						if err := rememberIngestStoredFile(ctx, files, image.ServingURL); err != nil {
							return nil, err
						}
					}
					resolved, changed = true, true
				}
			}
		case "image":
			for i, image := range request.Images {
				if usedImages[i] || ingestImageSource(image) != missing.SourceURL || (image.AssetID == "" && image.Data == "") {
					continue
				}
				data, err := decodeIngestInline(image.Data)
				name := "image.png"
				if image.AssetID != "" {
					asset, bytes, loadErr := s.loadIngestAsset(ctx, image.AssetID)
					if loadErr != nil {
						return nil, loadErr
					}
					name, data, err = asset.Name, bytes, nil
					if !docparser.IsImageFormat(asset.Type) {
						return nil, werrors.NewBadRequestError("image asset is not an image")
					}
				}
				if err != nil {
					return nil, werrors.NewBadRequestError(err.Error())
				}
				if _, err := ingestImageDataURI(base64.StdEncoding.EncodeToString(data)); err != nil {
					return nil, werrors.NewBadRequestError(err.Error())
				}
				ref := ""
				if s.vault != nil && article.VaultPath != "" && !summarySupplement {
					ref, err = s.vault.StoreArticleAttachment(article, name, data)
					if err != nil {
						return nil, err
					}
					if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
						changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article, ref})
					}
				} else {
					refs, stored, failures := s.resolveIngestImages(ctx, []types.IngestImage{{Data: base64.StdEncoding.EncodeToString(data)}}, files, article.SpaceID)
					if len(failures) > 0 {
						return nil, werrors.NewBadRequestError(failures[0].Reason)
					}
					ref = refs[0]
					for _, storedImage := range stored {
						intake.Images = append(intake.Images, storedImage.ServingURL)
						if err := rememberIngestStoredFile(ctx, files, storedImage.ServingURL); err != nil {
							return nil, err
						}
					}
				}
				mappings[missing.SourceURL] = ref
				if image.AssetID != "" {
					if err := s.referenceIngestAsset(ctx, image.AssetID, article.ID); err != nil {
						return nil, err
					}
				}
				usedImages[i], resolved, changed = true, true, true
				break
			}
		case "file":
			if !usedFile && request.File != nil && (request.File.AssetID != "" || request.File.Data != "") {
				asset, data, err := s.readIngestFile(ctx, request.File)
				if err != nil {
					return nil, err
				}
				ref := ""
				if s.vault != nil && article.VaultPath != "" && !summarySupplement {
					ref, err = s.vault.StoreArticleAttachment(article, asset.Name, data)
					if err != nil {
						return nil, err
					}
					if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
						changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article, ref})
					}
				} else {
					ref, err = files.SaveBytes(ctx, data, article.SpaceID, asset.Name, false)
					if err != nil {
						return nil, err
					}
					if err := rememberIngestStoredFile(ctx, files, ref); err != nil {
						return nil, err
					}
				}
				label := strings.NewReplacer("[", "\\[", "]", "\\]", "\n", " ").Replace(asset.Name)
				links = append(links, fmt.Sprintf("[%s](<%s>)", label, ref))
				attachments = append(attachments, types.IngestAttachment{AssetID: asset.ID, Name: asset.Name, Type: asset.Type, Path: ref, SourceURL: request.File.SourceURL, Size: int64(len(data))})
				if request.File.AssetID != "" {
					if err := s.referenceIngestAsset(ctx, request.File.AssetID, article.ID); err != nil {
						return nil, err
					}
				}
				usedFile, resolved, changed = true, true, true
			}
		}
		if !resolved {
			remaining = append(remaining, missing)
		}
	}
	if !changed && article.ExternalCompletionAttempted {
		return ingestResult(article, "exists", "已记录本次材料，文章仍等待用户补全。", "", ""), nil
	}
	// A file submitted to restore missing content is also retained as original
	// material. Existing nonmissing attachments remain untouched.
	if replaceContent && changed && !hasIngestMissing(remaining, "content", "") && request.File != nil && (request.File.AssetID != "" || request.File.Data != "") && !usedFile {
		asset, data, err := s.readIngestFile(ctx, request.File)
		if err != nil {
			return nil, err
		}
		path, err := files.SaveBytes(ctx, data, article.SpaceID, asset.Name, false)
		if err != nil {
			return nil, err
		}
		if err := rememberIngestStoredFile(ctx, files, path); err != nil {
			return nil, err
		}
		article.FilePath, article.FileName, article.FileType, article.FileSize = path, asset.Name, asset.Type, int64(len(data))
		ref := path
		if s.vault != nil && article.VaultPath != "" && !summarySupplement {
			ref, err = s.vault.StoreArticleAttachment(article, asset.Name, data)
			if err != nil {
				return nil, err
			}
			if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
				changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article, ref})
			}
			article.VaultOriginalPath = filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(article.VaultPath), ref)))
			links = append(links, fmt.Sprintf("[%s](<%s>)", strings.NewReplacer("[", "\\[", "]", "\\]", "\n", " ").Replace(asset.Name), ref))
		}
		attachments = append(attachments, types.IngestAttachment{AssetID: asset.ID, Name: asset.Name, Type: asset.Type, Path: ref, SourceURL: request.File.SourceURL, Size: int64(len(data))})
		if request.File.AssetID != "" {
			if err := s.referenceIngestAsset(ctx, request.File.AssetID, article.ID); err != nil {
				return nil, err
			}
		}
	}
	if replaceContent && changed && !hasIngestMissing(remaining, "content", "") {
		article.Acquisition = request.Acquisition
		// First-body publication needs local image files before the body becomes
		// final. Resolution uses supplied bytes exclusively.
		resolved, stored, err := docparser.NewImageResolver().ResolveAndStore(ctx, &types.ReadResult{MarkdownContent: newBody}, files, article.SpaceID)
		if err != nil {
			return nil, err
		}
		newBody = resolved
		for _, image := range stored {
			intake.Images = append(intake.Images, image.ServingURL)
			if err := rememberIngestStoredFile(ctx, files, image.ServingURL); err != nil {
				return nil, err
			}
		}
		if s.vault != nil && article.VaultPath != "" && !summarySupplement {
			for _, source := range articleImagesInOrder(newBody, nil) {
				if !strings.HasPrefix(source, "local://") && !strings.HasPrefix(source, "resource://") {
					continue
				}
				reader, err := s.resolveFileServiceForPath(ctx, kb, source).GetFile(ctx, source)
				if err != nil {
					return nil, err
				}
				data, readErr := io.ReadAll(reader)
				reader.Close()
				if readErr != nil {
					return nil, readErr
				}
				name := "image.png"
				if ext := filepath.Ext(source); ext != "" {
					name = "image" + ext
				}
				ref, err := s.vault.StoreArticleAttachment(article, name, data)
				if err != nil {
					return nil, err
				}
				if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
					changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article, ref})
				}
				newBody = replaceIngestImageTargets(newBody, map[string]string{source: ref})
			}
		}
		if ingestMissingContentPending(article) {
			if intake.SourceInfo.Author == "" {
				intake.SourceInfo.Author = request.Author
			}
			if intake.SourceInfo.Publisher == "" {
				intake.SourceInfo.Publisher = request.Publisher
			}
			if intake.SourceInfo.PublishedAt == "" {
				intake.SourceInfo.PublishedAt = normalizeIntakePublicationDate(request.PublishedAt)
			}
		}
	}
	newBody = replaceIngestImageTargets(newBody, mappings)
	newBody = AppendIngestAttachmentLinks(newBody, links)
	remaining = addRemoteIngestImageMissing(newBody, remaining, keyID, keyName)
	if s.vault != nil && article.VaultPath != "" && !summarySupplement && changed {
		change, err := s.vault.BeginArticleSupplement(ctx, article, newBody, mappings, links, replaceContent)
		if err != nil {
			return nil, err
		}
		if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
			changes.supplements = append(changes.supplements, change)
		} else {
			if err := change.finish(); err != nil {
				return nil, err
			}
		}
	}
	if changed {
		path, err := files.SaveBytes(ctx, []byte(newBody), article.SpaceID, article.ID+"-supplement.md", false)
		if err != nil {
			return nil, err
		}
		if err := rememberIngestStoredFile(ctx, files, path); err != nil {
			return nil, err
		}
		intake.OriginalPath = path
	}
	article.Incomplete = len(remaining) > 0
	if summarySupplement && changed && !article.Incomplete {
		metadata := map[string]json.RawMessage{}
		if len(article.Metadata) > 0 {
			if err := json.Unmarshal(article.Metadata, &metadata); err != nil {
				return nil, err
			}
		}
		metadata["completion_pending"] = json.RawMessage("true")
		article.Metadata, err = json.Marshal(metadata)
		if err != nil {
			return nil, err
		}
	}
	article.ExternalCompletionAttempted = article.Incomplete
	article.LastIngestAPIKeyID, article.LastIngestAPIKeyName = keyID, keyName
	if err := article.SetIngestMissing(remaining); err != nil {
		return nil, err
	}
	if err := article.SetIngestAttachments(attachments); err != nil {
		return nil, err
	}
	if err := article.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	pendingContent := ingestMissingContentPending(article)
	if pendingContent && !hasIngestMissing(remaining, "content", "") {
		intake.Decision, intake.Reason = "pending", ""
		article.ParseStatus, article.ErrorMessage = types.ParseStatusPending, ""
		if err := article.SetIntakeMetadata(intake); err != nil {
			return nil, err
		}
	}
	if err := s.persistIngestCompletion(ctx, article); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest_supplement", article.ID, map[string]any{"missing": beforeMissing}, map[string]any{"missing": remaining, "external_completion_attempted": article.ExternalCompletionAttempted}); err != nil {
		return nil, err
	}
	s.bindContentResources(ctx, article.SpaceID, article.ID, newBody)
	if pendingContent && !hasIngestMissing(remaining, "content", "") {
		result, err := s.finishIngestReview(ctx, kb, article, newBody)
		if err == nil && result.Status == "accepted" {
			result.Status = "supplemented"
			result.Message = "已补入正文，正在处理。"
		}
		return result, err
	}
	if !article.Incomplete && changed {
		article.ParseStatus, article.ErrorMessage = types.ParseStatusPending, ""
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", article.ID, article.SpaceID).Updates(map[string]any{"parse_status": article.ParseStatus, "error_message": "", "updated_at": time.Now()}).Error; err != nil {
			return nil, err
		}
		if _, err := s.enqueueManualProcessing(ctx, article, newBody, true); err != nil {
			return nil, err
		}
	}
	if s.vault != nil {
		s.vault.ScheduleRefresh(ctx, article.ID)
	}
	return ingestResult(article, "supplemented", "已补入可用材料。", "", ""), nil
}

func (s *entryService) persistIngestCompletion(ctx context.Context, k *types.Entry) error {
	return s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", k.ID, k.SpaceID).Updates(map[string]any{
		"incomplete": k.Incomplete, "external_completion_attempted": k.ExternalCompletionAttempted,
		"missing_materials": k.MissingMaterials, "ingest_attachments": k.IngestAttachments,
		"last_ingest_api_key_id": k.LastIngestAPIKeyID, "last_ingest_api_key_name": k.LastIngestAPIKeyName,
		"metadata": k.Metadata, "vault_body": k.VaultBody, "vault_original_path": k.VaultOriginalPath, "acquisition": k.Acquisition, "file_path": k.FilePath, "file_name": k.FileName, "file_type": k.FileType, "file_size": k.FileSize, "parse_status": k.ParseStatus, "error_message": k.ErrorMessage, "updated_at": time.Now(),
	}).Error
}

func (s *entryService) storeIngestConflict(ctx context.Context, kb *types.KnowledgeBase, original *types.Entry, request *types.IngestRequest, keyID uint64, keyName string) (*types.IngestResult, error) {
	now := time.Now()
	noun := "文章"
	if original.SourceKind == "mine" {
		noun = "用户文档"
	}
	message := "正文与已有" + noun + "不同，已另存到关联收录待审。"
	title := request.Title
	if title == "" {
		title = original.Title
	}
	incoming := &types.Entry{ID: uuid.NewString(), SpaceID: original.SpaceID, KnowledgeBaseID: kb.ID, Type: types.EntryTypeManual, SourceKind: original.SourceKind, Source: request.URL, Channel: types.ChannelAPI,
		Title: title, FileName: sanitizeManualDownloadFilename(title), FileType: types.EntryTypeManual, ParseStatus: types.ParseStatusRejected, EnableStatus: "disabled", CreatedAt: now, UpdatedAt: now,
		EmbeddingModelID: kb.EmbeddingModelID, Acquisition: request.Acquisition, ContentTypeHint: request.ContentTypeHint, RelatedArticleID: original.ID, AgentTags: types.JSON("[]"), CollectReason: request.CollectReason, AgentSummary: request.AgentSummary, SourceName: request.SourceName,
		IngestAPIKeyID: keyID, IngestAPIKeyName: keyName, LastIngestAPIKeyID: keyID, LastIngestAPIKeyName: keyName}
	tags, marshalErr := json.Marshal(request.AgentTags)
	if marshalErr != nil {
		return nil, marshalErr
	}
	if request.AgentTags != nil {
		incoming.AgentTags = types.JSON(tags)
	}
	intake := &types.EntryIntakeMetadata{Source: "ingest", Decision: "rejected", Reason: "与已有" + noun + "正文不同", SourceInfo: types.SourceInfo{OriginalURL: request.URL, Author: request.Author, Publisher: request.Publisher, PublishedAt: normalizeIntakePublicationDate(request.PublishedAt)}}
	if err := incoming.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	body, canonical, err := s.storeIngestMaterials(ctx, kb, incoming, request)
	if err != nil {
		return nil, err
	}
	setMessageFingerprint(incoming, canonical)
	// Ingest holds the knowledge base deduplication lock through this lookup
	// and creation. Retry only an exact body hash for this original article.
	var existing types.Entry
	err = s.db.WithContext(ctx).Where("space_id=? AND knowledge_base_id=? AND source_kind=? AND related_article_id=? AND content_hash=? AND parse_status=?",
		incoming.SpaceID, incoming.KnowledgeBaseID, original.SourceKind, original.ID, incoming.ContentHash, types.ParseStatusRejected).
		Order("created_at ASC,id ASC").First(&existing).Error
	if err == nil {
		if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
			if err := changes.removeUnusedStoredFiles(ctx, s.fileSvc); err != nil {
				return nil, err
			}
		}
		return ingestResult(&existing, "conflict", message, intake.Reason, ""), nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := incoming.SetManualMetadata(types.NewManualEntryMetadata("", types.ManualEntryStatusPublish, 1)); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Create(incoming).Error; err != nil {
		return nil, err
	}
	if err := s.referenceIngestMaterials(ctx, incoming, request); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", original.ID, original.SpaceID).Updates(map[string]any{"external_completion_attempted": true, "last_ingest_api_key_id": keyID, "last_ingest_api_key_name": keyName, "updated_at": now}).Error; err != nil {
		return nil, err
	}
	s.bindContentResources(ctx, incoming.SpaceID, incoming.ID, body)
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest_conflict", incoming.ID, nil, map[string]any{"related_article_id": original.ID, "reason": intake.Reason}); err != nil {
		return nil, err
	}
	return ingestResult(incoming, "conflict", message, intake.Reason, ""), nil
}
