package service

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"path"
	werrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Legacy shapes let processing tests exercise already stored articles without a production creation entry point.
type legacyManualFixturePayload struct {
	Title, Content, Status, Channel string
	ProcessConfig                   *types.EntryProcessOverrides
	SourceInfo                      types.SourceInfo
	SourceMetadata                  map[string]string
}

func (s *entryService) createLegacyFileFixture(ctx context.Context,
	libraryID string, file *multipart.FileHeader, metadata map[string]string, enableMultimodel *bool, customFileName string, channel string,
	processOverrides *types.EntryProcessOverrides,
) (*types.Entry, error) {
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			return copy.createLegacyFileFixture(ctx, libraryID, file, metadata, enableMultimodel, customFileName, channel, processOverrides)
		})
	}

	logger.Info(ctx, "Start creating entry from file")

	fileName := path.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
	if customFileName != "" {
		fileName = path.Base(strings.ReplaceAll(customFileName, "\\", "/"))
	}

	logger.Infof(ctx, "Library ID: %s, file: %s", libraryID, fileName)

	// Get library configuration
	logger.Info(ctx, "Getting library configuration")
	library, err := s.libraryService.GetLibraryByID(ctx, libraryID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get library: %v", err)
		return nil, err
	}

	// Early reject before the whole-file hash below. resolveFileImportProcessConfig
	// gates the same extension set and provides a clear unsupported-type error.

	// Calculate file hash for deduplication
	logger.Info(ctx, "Calculating file hash")
	hash, err := calculateFileHash(file)
	if err != nil {
		logger.Errorf(ctx, "Failed to calculate file hash: %v", err)
		return nil, err
	}

	// Check if file already exists
	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	logger.Infof(ctx, "Checking if file exists, space ID: %d", spaceID)
	exists, existingEntry, err := s.repo.CheckEntryExists(ctx, spaceID, libraryID, &types.EntryCheckParams{
		Type:     "file",
		FileName: fileName,
		FileType: getFileType(fileName),
		FileSize: file.Size,
		FileHash: hash,
	})
	if err != nil {
		logger.Errorf(ctx, "Failed to check entry existence: %v", err)
		return nil, err
	}
	if exists {
		if err := s.recordKnownMessageRepost(ctx, existingEntry, libraryID, types.SourceInfo{Author: metadata["author"], Publisher: metadata["publisher"], OriginalURL: metadata["original_url"], PublishedAt: metadata["published_at"]}, "file_hash"); err != nil {
			return nil, err
		}
		logger.Infof(ctx, "File already exists: %s", fileName)
		// Update creation time for existing entry
		if err := s.repo.UpdateEntryColumn(ctx, existingEntry.ID, "created_at", time.Now()); err != nil {
			logger.Errorf(ctx, "Failed to update existing entry: %v", err)
			return nil, err
		}
		return existingEntry, types.NewDuplicateFileError(existingEntry)
	}

	// Convert metadata to JSON format if provided
	var metadataJSON types.JSON
	if metadata != nil {
		metadataBytes, err := json.Marshal(metadata)
		if err != nil {
			logger.Errorf(ctx, "Failed to marshal metadata: %v", err)
			return nil, err
		}
		metadataJSON = types.JSON(metadataBytes)
	}

	// 验证文件名安全性
	safeFilename, isValid := secutils.ValidateInput(fileName)
	if !isValid {
		logger.Errorf(ctx, "Invalid filename: %s", fileName)
		return nil, werrors.NewValidationError("文件名包含非法字符")
	}

	_, err = resolveFileImportProcessConfig(ctx, library, getFileType(safeFilename), processOverrides, enableMultimodel)
	if err != nil {
		return nil, err
	}

	// Prepare entry record
	logger.Info(ctx, "Preparing entry record")
	entry := &types.Entry{
		ID:               uuid.New().String(),
		SpaceID:          spaceID,
		LibraryID:        libraryID,
		Type:             "file",
		Channel:          defaultChannel(channel),
		Title:            safeFilename,
		FileName:         safeFilename,
		FileType:         getFileType(safeFilename),
		FileSize:         file.Size,
		FileHash:         hash,
		ParseStatus:      "pending",
		EnableStatus:     "disabled",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		EmbeddingModelID: library.EmbeddingModelID,
		Metadata:         metadataJSON,
	}

	if processOverrides != nil {
		if err := entry.SetProcessOverrides(processOverrides); err != nil {
			logger.Errorf(ctx, "Failed to set process overrides: %v", err)
			return nil, err
		}
	}

	// Save the file to storage (use Library-level storage engine if configured)
	logger.Infof(ctx, "Saving file, entry ID: %s", entry.ID)
	fileSvc := s.resolveFileService(ctx, library)
	filePath, err := fileSvc.SaveFile(ctx, file, entry.SpaceID, entry.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to save file, entry ID: %s, error: %v", entry.ID, err)
		return nil, err
	}
	entry.FilePath = filePath
	{
		if err := entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
			Source: "file", Decision: "pending",
			SourceInfo: types.SourceInfo{Author: metadata["author"], Publisher: metadata["publisher"], OriginalURL: metadata["original_url"], PublishedAt: metadata["published_at"]},
		}); err != nil {
			_ = fileSvc.DeleteFile(ctx, filePath)
			return nil, err
		}
	}

	// Save entry record to database after the file is safely stored.
	logger.Info(ctx, "Saving entry record to database")
	duplicate, err := s.createMessageWithDeduplication(ctx, entry, "")
	if err != nil || duplicate != nil {
		if deleteErr := fileSvc.DeleteFile(ctx, filePath); deleteErr != nil {
			return nil, deleteErr
		}
		if duplicate != nil && err == nil {
			return duplicate, types.NewDuplicateFileError(duplicate)
		}
		logger.Errorf(ctx, "Failed to create entry record, ID: %s, error: %v", entry.ID, err)
		return nil, err
	}

	// Enqueue document processing task to PostgreSQL
	logger.Info(ctx, "Enqueuing document processing task to PostgreSQL")

	lang := types.LanguageFromContextOrDefault(ctx)
	sourceReader, err := file.Open()
	if err != nil {
		return nil, err
	}
	sourceBody, err := io.ReadAll(sourceReader)
	sourceReader.Close()
	if err != nil {
		return nil, err
	}
	taskPayload := types.DocumentProcessPayload{
		SpaceID:   spaceID,
		EntryID:   entry.ID,
		LibraryID: libraryID,
		FilePath:  filePath,
		Passages:  []string{string(sourceBody)},
		FileName:  safeFilename,
		FileType:  getFileType(safeFilename),
		Language:  lang,
	}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal document process task payload: %v", err)
		s.markEntryEnqueueFailed(ctx, entry)
		// 即使入队失败，也返回entry，因为文件已保存
		return entry, nil
	}

	task := taskqueue.NewTask(
		types.TypeDocumentProcess,
		payloadBytes,
		documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
	)
	info, err := s.task.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue document process task: %v", err)
		s.markEntryEnqueueFailed(ctx, entry)
		// 即使入队失败，也返回entry，因为文件已保存
		return entry, nil
	}
	logger.Infof(
		ctx,
		"Enqueued document process task: id=%s queue=%s entry_id=%s",
		info.ID,
		info.Queue,
		entry.ID,
	)

	logger.Infof(ctx, "Entry from file created successfully, ID: %s", entry.ID)
	return entry, nil
}

func (s *entryService) createLegacyManualFixture(ctx context.Context,
	libraryID string, payload *legacyManualFixturePayload, channel string,
) (*types.Entry, error) {
	if s.needsTaskTransaction() {
		return entryTaskTransaction(ctx, s, func(copy *entryService) (*types.Entry, error) {
			return copy.createLegacyManualFixture(ctx, libraryID, payload, channel)
		})
	}

	logger.Info(ctx, "Start creating manual entry entry")

	if payload == nil {
		return nil, werrors.NewBadRequestError("请求内容不能为空")
	}

	cleanContent := secutils.CleanMarkdown(payload.Content)
	if strings.TrimSpace(cleanContent) == "" {
		return nil, werrors.NewValidationError("内容不能为空")
	}
	if len([]rune(cleanContent)) > manualContentMaxLength {
		return nil, werrors.NewValidationError(fmt.Sprintf("内容长度超出限制（最多%d个字符）", manualContentMaxLength))
	}

	safeTitle, ok := secutils.ValidateInput(payload.Title)
	if !ok {
		return nil, werrors.NewValidationError("标题包含非法字符或超出长度限制")
	}

	status := strings.ToLower(strings.TrimSpace(payload.Status))
	if status == "" {
		status = types.ManualEntryStatusDraft
	}
	if status != types.ManualEntryStatusDraft && status != types.ManualEntryStatusPublish {
		return nil, werrors.NewValidationError("状态仅支持 draft 或 publish")
	}

	library, err := s.libraryService.GetLibraryByID(ctx, libraryID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get library: %v", err)
		return nil, err
	}

	spaceID := ctx.Value(types.SpaceIDContextKey).(uint64)
	now := time.Now()
	title := safeTitle
	if title == "" {
		title = fmt.Sprintf("Article-%s", now.Format("20060102-150405"))
	}

	if status == types.ManualEntryStatusPublish {
		incoming := &types.Entry{SpaceID: spaceID, LibraryID: libraryID}
		if len(payload.SourceMetadata) > 0 {
			metadata, err := json.Marshal(payload.SourceMetadata)
			if err != nil {
				return nil, err
			}
			incoming.Metadata = types.JSON(metadata)
		}
		if err := incoming.SetIntakeMetadata(&types.EntryIntakeMetadata{SourceInfo: payload.SourceInfo}); err != nil {
			return nil, err
		}
		existing, err := s.checkMessageDuplicate(ctx, incoming, cleanContent)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, &types.DuplicateEntryError{Message: "Message already exists", Entry: existing}
		}
	}

	fileName := ensureManualFileName(title)
	meta := types.NewManualEntryMetadata(cleanContent, status, 1)

	entry := &types.Entry{
		ID:               uuid.New().String(),
		SpaceID:          spaceID,
		LibraryID:        libraryID,
		Type:             types.EntryTypeManual,
		Channel:          defaultChannel(channel),
		Title:            title,
		Description:      "",
		Source:           types.EntryTypeManual,
		ParseStatus:      types.ManualEntryStatusDraft,
		EnableStatus:     "disabled",
		CreatedAt:        now,
		UpdatedAt:        now,
		EmbeddingModelID: library.EmbeddingModelID,
		FileName:         fileName,
		FileType:         types.EntryTypeManual,
	}
	if len(payload.SourceMetadata) > 0 {
		metadata, err := json.Marshal(payload.SourceMetadata)
		if err != nil {
			return nil, err
		}
		entry.Metadata = types.JSON(metadata)
	}
	if err := entry.SetManualMetadata(meta); err != nil {
		logger.Errorf(ctx, "Failed to set manual metadata: %v", err)
		return nil, err
	}
	// Keep the full message body
	// outside the row so a rejected message can be previewed/retried without
	// turning the metadata column into an unbounded original archive.
	{
		fileSvc := s.resolveFileService(ctx, library)
		originalPath, err := fileSvc.SaveBytes(ctx, []byte(cleanContent), spaceID, fileName, false)
		if err != nil {
			return nil, err
		}
		intake := &types.EntryIntakeMetadata{Source: "manual", OriginalPath: originalPath, Decision: "pending", SourceInfo: payload.SourceInfo}
		if err := entry.SetIntakeMetadata(intake); err != nil {
			_ = fileSvc.DeleteFile(ctx, originalPath)
			return nil, err
		}
		meta.Content = ""
		if err := entry.SetManualMetadata(meta); err != nil {
			_ = fileSvc.DeleteFile(ctx, originalPath)
			return nil, err
		}
	}
	entry.EnsureManualDefaults()

	if status == types.ManualEntryStatusPublish {
		entry.ParseStatus = "pending"
	}

	if status == types.ManualEntryStatusPublish {
		if err := entry.SetProcessOverrides(payload.ProcessConfig); err != nil {
			return nil, err
		}
	}

	var existing *types.Entry
	if status == types.ManualEntryStatusPublish {
		existing, err = s.createMessageWithDeduplication(ctx, entry, cleanContent)
	} else {
		err = s.repo.CreateEntry(ctx, entry)
	}
	if err != nil || existing != nil {
		if intake, _ := entry.IntakeMetadata(); intake != nil && intake.OriginalPath != "" {
			if cleanupErr := s.resolveFileService(ctx, library).DeleteFile(ctx, intake.OriginalPath); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		if err != nil {
			return nil, err
		}
		return existing, &types.DuplicateEntryError{Message: "Message already exists", Entry: existing}
	}

	// Claim referenced files before publishing so a saved draft retains them.
	s.bindContentResources(ctx, spaceID, entry.ID, cleanContent)

	if status == types.ManualEntryStatusPublish {
		logger.Infof(ctx, "Manual entry created, enqueuing async processing task, ID: %s", entry.ID)
		_, err := s.enqueueManualProcessing(ctx, entry, cleanContent, false)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue manual processing task for new entry: %v", err)
			// Non-fatal: mark as failed so user can retry
			entry.ParseStatus = "failed"
			entry.ErrorMessage = "Failed to enqueue processing task"
			s.repo.UpdateEntry(ctx, entry)
			s.failIntake(ctx, entry)
		}
	}

	return entry, nil
}

func calculateFileHash(file *multipart.FileHeader) (string, error) {
	f, err := file.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}

	// Reset file pointer for subsequent operations
	if _, err := f.Seek(0, 0); err != nil {
		return "", err
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

func ensureManualFileName(title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return "article.md"
	}
	if !strings.HasSuffix(strings.ToLower(title), ".md") {
		title += ".md"
	}
	return title
}

func resolveFileImportProcessConfig(
	ctx context.Context,
	library *types.Library,
	fileType string,
	processOverrides *types.EntryProcessOverrides,
	enableMultimodel *bool,
) (types.EffectiveProcessConfig, error) {

	eff := ResolveProcessConfig(library, processOverrides)

	return eff, nil
}

func isValidFileType(filename string) bool {
	return getFileType(filename) == "md"
}

func calculateStr(strList ...string) string {
	h := md5.New()
	input := strings.Join(strList, "")
	h.Write([]byte(input))
	return hex.EncodeToString(h.Sum(nil))
}

func defaultChannel(ch string) string {
	if ch == "" {
		return types.ChannelWeb
	}
	return ch
}
