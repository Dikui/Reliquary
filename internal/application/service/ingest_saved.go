package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"strings"
	"time"

	werrors "reliquary/internal/errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Source identity never uses body similarity or an unprovided source inferred by a model.
func (s *entryService) findExactIngestSource(ctx context.Context, incoming *types.Entry) (*types.Entry, error) {
	conditions, values := []string{}, []any{}
	for _, field := range []struct{ column, value string }{
		{"source_id", incoming.SourceID},
		{"source_ref", incoming.SourceRef},
		{"identifier", incoming.Identifier},
		{"normalized_url", incoming.NormalizedURL},
	} {
		if field.value != "" {
			condition := field.column + " <> '' AND " + field.column + " = ?"
			if field.column != "normalized_url" {
				condition += " AND md5(" + field.column + ") = md5(?)"
				values = append(values, field.value)
			}
			conditions, values = append(conditions, "("+condition+")"), append(values, field.value)
		}
	}
	if len(conditions) == 0 {
		return nil, nil
	}
	var entries []types.Entry
	if err := s.db.WithContext(ctx).Where("space_id=? AND library_id=?", incoming.SpaceID, incoming.LibraryID).
		Where("("+strings.Join(conditions, " OR ")+")", values...).Order("id").Limit(2).Find(&entries).Error; err != nil {
		return nil, err
	}
	if len(entries) > 1 {
		return nil, werrors.NewConflictError("source identifiers match multiple entries; specify an unambiguous source")
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return &entries[0], nil
}

func (s *entryService) saveIngestEntry(ctx context.Context, library *types.Library, entry, existing *types.Entry, request *types.IngestRequest) (*types.IngestResult, error) {
	if existing != nil {
		if err := lockEntitySpace(s.db.WithContext(ctx), entry.SpaceID); err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND space_id=?", existing.ID, entry.SpaceID).First(existing).Error; err != nil {
			return nil, err
		}
		if entry.SourceID != "" && existing.SourceID != "" {
			if entry.SourceID != existing.SourceID {
				return nil, werrors.NewConflictError("source_id is already bound to a different source")
			}
			if *entry.SourceRevision <= *existing.SourceRevision {
				existing.LibraryName = library.Name
				return ingestResult(existing, "stale_revision", "修订号未增加，已保留当前资料。",
					"source_revision_not_newer", ""), nil
			}
		}
		if entry.SourceID == "" {
			entry.SourceID, entry.SourceRevision = existing.SourceID, existing.SourceRevision
		}
		entry.ID, entry.CreatedAt, entry.VaultPath = existing.ID, existing.CreatedAt, existing.VaultPath
		entry.IngestAPIKeyID, entry.IngestAPIKeyName = existing.IngestAPIKeyID, existing.IngestAPIKeyName
		entry.SourceUnverified, entry.Starred = existing.SourceUnverified, existing.Starred
		entry.CustomMetadata = existing.CustomMetadata
		entry.EntitiesManual = existing.EntitiesManual
		entry.DomainsManual = existing.DomainsManual
		entry.TitleManual, entry.DescriptionManual = existing.TitleManual, existing.DescriptionManual
		if existing.DescriptionManual {
			entry.Description, entry.StructuredSummary = existing.Description, existing.StructuredSummary
		}
		entry.RelatedEntryID = existing.RelatedEntryID
		original, err := existing.IntakeMetadata()
		if err != nil {
			return nil, err
		}
		sameSourceTitle := original != nil && original.SourceTitle != "" && original.SourceTitle == request.Title
		if request.Title == "" || existing.TitleManual || sameSourceTitle {
			entry.Title, entry.FileName = existing.Title, existing.FileName
		}
		if request.SourceRef == "" {
			entry.SourceRef = existing.SourceRef
		}
		if request.Identifier == "" {
			entry.Identifier = existing.Identifier
		}
		if request.URL == "" || normalizeMessageURL(request.URL) == existing.NormalizedURL {
			entry.Source = existing.Source
		}
		if request.CollectReason == "" {
			entry.CollectReason = existing.CollectReason
		}
		if request.AgentSummary == "" {
			entry.AgentSummary = existing.AgentSummary
		}
		if len(request.AgentTags) == 0 {
			entry.AgentTags = existing.AgentTags
		}
		if request.SourceName == "" {
			entry.SourceName = existing.SourceName
		}
		if request.Acquisition == "" {
			entry.Acquisition = existing.Acquisition
		}
		entry.Metadata = append(types.JSON(nil), existing.Metadata...)
	}
	if entry.VaultPath == "" {
		entry.VaultPath = vaultEntryPath(library.Directory, entry.Title, entry.ID)
	}
	intake, err := entry.IntakeMetadata()
	if err != nil {
		return nil, err
	}
	if intake == nil {
		intake = &types.EntryIntakeMetadata{Source: "ingest"}
	}
	if request.Title != "" {
		intake.SourceTitle = request.Title
	}
	intake.Decision, intake.Reason, intake.OriginalPath, intake.UserRejected = "approved", "", "", false
	intake.SourceInfo.OriginalURL = entry.Source
	if request.Author != "" {
		intake.SourceInfo.Author = request.Author
	}
	if request.Publisher != "" {
		intake.SourceInfo.Publisher = request.Publisher
	}
	if request.PublishedAt != "" {
		intake.SourceInfo.PublishedAt = normalizeIntakePublicationDate(request.PublishedAt)
	}
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	if err := setRequestedFactMentions(entry, request); err != nil {
		return nil, err
	}
	diagnosticsBody, canonical, err := s.storeIngestMaterials(ctx, library, entry, request)
	if err != nil {
		return nil, err
	}
	body, err := s.archiveIngestImages(ctx, library, entry, existing, diagnosticsBody)
	if err != nil {
		return nil, err
	}
	entry.VaultBody = body
	// The original image bytes now live in entry attachments. Drop temporary
	// upload copies and their references in the same transaction.
	intake, err = entry.IntakeMetadata()
	if err != nil {
		return nil, err
	}
	intake.Images = nil
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	if changes, ok := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges); ok {
		if err := changes.removeUnusedStoredFiles(ctx, s.fileSvc); err != nil {
			return nil, err
		}
		changes.stored = nil
	}
	setMessageFingerprint(entry, canonical)
	entry.SourceBodyHash = sourceBodyHash(canonical)
	if discarded, err := s.lookupIngestDiscard(ctx, library.ID, entryDiscardKeys(entry, intakeTemplateRules(library))); err != nil {
		return nil, err
	} else if discarded != nil {
		return ingestResult(nil, "discarded", "该来源已被人工拒收。", discarded.Reason, discarded.EntryID), nil
	}
	if existing != nil {
		before, _ := existing.IntakeMetadata()
		after, _ := entry.IntakeMetadata()
		if before == nil {
			before = &types.EntryIntakeMetadata{}
		}
		oldSource, _ := json.Marshal(before.SourceInfo)
		newSource, _ := json.Marshal(after.SourceInfo)
		if strings.TrimSuffix(existing.VaultBody, "\n") == strings.TrimSuffix(body, "\n") && existing.Title == entry.Title && existing.Acquisition == entry.Acquisition &&
			before.SourceTitle == after.SourceTitle &&
			existing.SourceRef == entry.SourceRef && existing.Identifier == entry.Identifier &&
			existing.CollectReason == entry.CollectReason &&
			existing.AgentSummary == entry.AgentSummary && bytes.Equal(existing.AgentTags, entry.AgentTags) &&
			existing.SourceName == entry.SourceName && bytes.Equal(oldSource, newSource) &&
			sameRequestedFactMentions(existing, entry) {
			// Advance the revision even when the submitted body and metadata are unchanged.
			if request.SourceID != "" {
				if err := s.db.WithContext(ctx).Model(existing).Updates(map[string]any{
					"source_id": entry.SourceID, "source_revision": entry.SourceRevision,
				}).Error; err != nil {
					return nil, err
				}
			}
			existing.SourceID, existing.SourceRevision = entry.SourceID, entry.SourceRevision
			return ingestResult(existing, "exists", "保存内容未变化。", "", ""), nil
		}
		if err := s.cleanupLibraryRebuild(ctx, existing); err != nil {
			return nil, err
		}
		if err := advanceSegmentGeneration(entry, "processing"); err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", entry.ID, entry.SpaceID).
			Select("*").Omit("library_name", "has_update", "latest_entry_id").Updates(entry).Error; err != nil {
			return nil, err
		}
	} else {
		if err := entry.SetManualMetadata(types.NewManualEntryMetadata("", types.ManualEntryStatusPublish, 1)); err != nil {
			return nil, err
		}
		if err := s.db.WithContext(ctx).Create(entry).Error; err != nil {
			return nil, err
		}
	}
	if request.MentionedFactIDs != nil {
		if err := s.saveExplicitFactMentions(ctx, entry, *request.MentionedFactIDs); err != nil {
			return nil, err
		}
	}
	if err := s.referenceIngestMaterials(ctx, entry, request); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", entry.ID, nil, map[string]any{"library_id": library.ID, "replaced": existing != nil, "acquisition": entry.Acquisition, "collect_reason": entry.CollectReason, "url": entry.Source}); err != nil {
		return nil, err
	}
	if request.RecoveryFailureID != "" {
		if err := s.db.WithContext(ctx).Model(entry).Update("metadata", gorm.Expr("jsonb_set(COALESCE(metadata,'{}'::jsonb),'{recovery_failure_id}',to_jsonb(?::text),true)", request.RecoveryFailureID)).Error; err != nil {
			return nil, err
		}
	}
	s.bindContentResources(ctx, entry.SpaceID, entry.ID, body)
	if _, err := s.enqueueManualProcessing(ctx, entry, body, false); err != nil {
		return nil, err
	}
	entry.LibraryName = library.Name
	return ingestResult(entry, "accepted", "已保存，正在后台建立索引和处理。", "", ""), nil
}

// Reuse existing attachment bytes before allocating files, so exact retries remain exact.
func (s *entryService) archiveIngestImages(ctx context.Context, library *types.Library, entry, existing *types.Entry, body string) (string, error) {
	byHash := map[string]string{}
	if existing != nil {
		for _, ref := range articleImagesInOrder(existing.VaultBody, nil) {
			if !isArticleAttachmentReference(entry.ID, entry.VaultPath, ref) {
				continue
			}
			reader, err := s.vault.ReadImage(existing, ref)
			if err != nil {
				continue
			}
			data, readErr := io.ReadAll(reader)
			closeErr := reader.Close()
			if readErr == nil && closeErr == nil {
				byHash[fmt.Sprintf("%x", sha256.Sum256(data))] = ref
			}
		}
	}
	mappings := map[string]string{}
	for _, ref := range articleImagesInOrder(body, nil) {
		if remoteIngestImage(ref) {
			continue
		}
		if _, ok := mappings[ref]; ok {
			continue
		}
		read := func(ctx context.Context, path string) (io.ReadCloser, error) {
			if isArticleAttachmentReference(entry.ID, entry.VaultPath, path) {
				return s.vault.ReadImage(entry, path)
			}
			return s.resolveFileServiceForPath(ctx, library, path).GetFile(ctx, path)
		}
		data, err := readVaultImage(ctx, ref, read)
		if err != nil {
			return "", werrors.NewBadRequestError("provided image is unreadable: " + err.Error())
		}
		if _, err := utils.PrepareModelImage(ctx, bytes.NewReader(data)); err != nil {
			return "", werrors.NewBadRequestError(err.Error())
		}
		identity := fmt.Sprintf("%x", sha256.Sum256(data))
		target := byHash[identity]
		if target == "" {
			target, err = s.vault.StoreArticleAttachment(entry, "image"+vaultImageExtension(ref, http.DetectContentType(data)), data)
			if err != nil {
				return "", err
			}
			changes := ctx.Value(ingestFileChangesKey{}).(*ingestFileChanges)
			changes.vaultFiles = append(changes.vaultFiles, ingestVaultFile{article: entry, path: target})
			byHash[identity] = target
		}
		mappings[ref] = target
	}
	entry.UpdatedAt = time.Now()
	return replaceIngestImageTargets(body, mappings), nil
}
