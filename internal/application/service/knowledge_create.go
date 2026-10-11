package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	taskqueue "reliquary/internal/queue"

	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/logger"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

// CreateKnowledgeFromFile creates a knowledge entry from an uploaded file
func (s *knowledgeService) CreateKnowledgeFromFile(ctx context.Context,
	kbID string, file *multipart.FileHeader, metadata map[string]string, enableMultimodel *bool, customFileName string, channel string,
	processOverrides *types.KnowledgeProcessOverrides,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.CreateKnowledgeFromFile(ctx, kbID, file, metadata, enableMultimodel, customFileName, channel, processOverrides)
		})
	}

	logger.Info(ctx, "Start creating knowledge from file")

	fileName := path.Base(strings.ReplaceAll(file.Filename, "\\", "/"))
	if customFileName != "" {
		fileName = path.Base(strings.ReplaceAll(customFileName, "\\", "/"))
	}

	logger.Infof(ctx, "Knowledge base ID: %s, file: %s", kbID, fileName)

	if IsVideoType(getFileType(fileName)) {
		logger.Error(ctx, "Video file upload is not supported")
		return nil, werrors.NewBadRequestError("暂不支持上传视频文件")
	}

	// Get knowledge base configuration
	logger.Info(ctx, "Getting knowledge base configuration")
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return nil, err
	}

	// Early reject before the whole-file hash below. resolveFileImportProcessConfig
	// gates the same extension set and provides a clear unsupported-type error.
	if err := validateImportFileType(getFileType(fileName)); err != nil {
		return nil, err
	}

	// Calculate file hash for deduplication
	logger.Info(ctx, "Calculating file hash")
	hash, err := calculateFileHash(file)
	if err != nil {
		logger.Errorf(ctx, "Failed to calculate file hash: %v", err)
		return nil, err
	}

	// Check if file already exists
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	logger.Infof(ctx, "Checking if file exists, tenant ID: %d", tenantID)
	exists, existingKnowledge, err := s.repo.CheckKnowledgeExists(ctx, tenantID, kbID, &types.KnowledgeCheckParams{
		Type:     "file",
		FileName: fileName,
		FileType: getFileType(fileName),
		FileSize: file.Size,
		FileHash: hash,
	})
	if err != nil {
		logger.Errorf(ctx, "Failed to check knowledge existence: %v", err)
		return nil, err
	}
	if exists {
		if err := s.recordKnownMessageRepost(ctx, existingKnowledge, kbID, types.SourceInfo{Author: metadata["author"], Publisher: metadata["publisher"], OriginalURL: metadata["original_url"], PublishedAt: metadata["published_at"]}, "file_hash"); err != nil {
			return nil, err
		}
		logger.Infof(ctx, "File already exists: %s", fileName)
		// Update creation time for existing knowledge
		if err := s.repo.UpdateKnowledgeColumn(ctx, existingKnowledge.ID, "created_at", time.Now()); err != nil {
			logger.Errorf(ctx, "Failed to update existing knowledge: %v", err)
			return nil, err
		}
		return existingKnowledge, types.NewDuplicateFileError(existingKnowledge)
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

	_, err = resolveFileImportProcessConfig(ctx, kb, getFileType(safeFilename), processOverrides, enableMultimodel)
	if err != nil {
		return nil, err
	}

	// Prepare knowledge record
	logger.Info(ctx, "Preparing knowledge record")
	knowledge := &types.Knowledge{
		ID:               uuid.New().String(),
		TenantID:         tenantID,
		KnowledgeBaseID:  kbID,
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
		EmbeddingModelID: kb.EmbeddingModelID,
		Metadata:         metadataJSON,
	}

	if processOverrides != nil {
		if err := knowledge.SetProcessOverrides(processOverrides); err != nil {
			logger.Errorf(ctx, "Failed to set process overrides: %v", err)
			return nil, err
		}
	}

	// Save the file to storage (use KB-level storage engine if configured)
	logger.Infof(ctx, "Saving file, knowledge ID: %s", knowledge.ID)
	fileSvc := s.resolveFileService(ctx, kb)
	filePath, err := fileSvc.SaveFile(ctx, file, knowledge.TenantID, knowledge.ID)
	if err != nil {
		logger.Errorf(ctx, "Failed to save file, knowledge ID: %s, error: %v", knowledge.ID, err)
		return nil, err
	}
	knowledge.FilePath = filePath
	{
		if err := knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{
			Source: "file", Decision: "pending",
			SourceInfo: types.SourceInfo{Author: metadata["author"], Publisher: metadata["publisher"], OriginalURL: metadata["original_url"], PublishedAt: metadata["published_at"]},
		}); err != nil {
			_ = fileSvc.DeleteFile(ctx, filePath)
			return nil, err
		}
	}

	// Save knowledge record to database after the file is safely stored.
	logger.Info(ctx, "Saving knowledge record to database")
	duplicate, err := s.createMessageWithDeduplication(ctx, knowledge, "")
	if err != nil || duplicate != nil {
		if deleteErr := fileSvc.DeleteFile(ctx, filePath); deleteErr != nil {
			return nil, deleteErr
		}
		if duplicate != nil && err == nil {
			return duplicate, types.NewDuplicateFileError(duplicate)
		}
		logger.Errorf(ctx, "Failed to create knowledge record, ID: %s, error: %v", knowledge.ID, err)
		return nil, err
	}

	// Enqueue document processing task to PostgreSQL
	logger.Info(ctx, "Enqueuing document processing task to PostgreSQL")

	lang := types.LanguageFromContextOrDefault(ctx)
	taskPayload := types.DocumentProcessPayload{
		TenantID:        tenantID,
		KnowledgeID:     knowledge.ID,
		KnowledgeBaseID: kbID,
		FilePath:        filePath,
		FileName:        safeFilename,
		FileType:        getFileType(safeFilename),
		Language:        lang,
	}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal document process task payload: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		// 即使入队失败，也返回knowledge，因为文件已保存
		return knowledge, nil
	}

	task := taskqueue.NewTask(
		types.TypeDocumentProcess,
		payloadBytes,
		documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
	)
	info, err := s.task.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue document process task: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		// 即使入队失败，也返回knowledge，因为文件已保存
		return knowledge, nil
	}
	logger.Infof(
		ctx,
		"Enqueued document process task: id=%s queue=%s knowledge_id=%s",
		info.ID,
		info.Queue,
		knowledge.ID,
	)

	logger.Infof(ctx, "Knowledge from file created successfully, ID: %s", knowledge.ID)
	return knowledge, nil
}

// isFileURL reports whether the given URL should be treated as a direct file download.
// Priority: URL path has a known file extension first, then fall back to user-provided fileName/fileType hints.
func isFileURL(rawURL, fileName, fileType string) bool {
	u, err := url.Parse(rawURL)
	if err == nil {
		ext := strings.ToLower(strings.TrimPrefix(path.Ext(u.Path), "."))
		if ext != "" && (isSupportedImportExtension(ext) || IsAudioType(ext)) {
			return true
		}
	}
	// Fall back to user-provided hints
	return fileName != "" || fileType != ""
}

func (s *knowledgeService) CreateKnowledgeFromURL(ctx context.Context,
	kbID string, rawURL string, fileName string, fileType string, enableMultimodel *bool, title string, channel string,
	processOverrides *types.KnowledgeProcessOverrides, sourceInfos ...types.SourceInfo,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.CreateKnowledgeFromURL(ctx, kbID, rawURL, fileName, fileType, enableMultimodel, title, channel, processOverrides, sourceInfos...)
		})
	}

	if blockedIntakeURL(rawURL) {
		return nil, werrors.NewBadRequestError("X/Twitter sources require Grok ingestion")
	}
	logger.Info(ctx, "Start creating knowledge from URL")
	logger.Infof(ctx, "Knowledge base ID: %s, URL: %s", kbID, rawURL)

	// Route to file_url logic when the URL points to a downloadable file
	if isFileURL(rawURL, fileName, fileType) {
		return s.createKnowledgeFromFileURL(
			ctx, kbID, rawURL, fileName, fileType, enableMultimodel, title, channel, processOverrides,
			sourceInfos...,
		)
	}

	url := rawURL

	// Get knowledge base configuration
	logger.Info(ctx, "Getting knowledge base configuration")
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return nil, err
	}

	// Validate URL format and security
	logger.Info(ctx, "Validating URL")
	if !isValidURL(url) || !secutils.IsValidURL(url) {
		logger.Error(ctx, "Invalid or unsafe URL format")
		return nil, ErrInvalidURL
	}

	// SSRF protection: validate URL is safe to fetch (uses centralised entry-point with whitelist support)
	if err := secutils.ValidateURLForSSRF(url); err != nil {
		logger.Errorf(ctx, "URL rejected for SSRF protection: %s, err: %v", url, err)
		return nil, ErrInvalidURL
	}

	// Check if URL already exists in the knowledge base
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	logger.Infof(ctx, "Checking if URL exists, tenant ID: %d", tenantID)
	fileHash := calculateStr(url)

	incoming := &types.Knowledge{TenantID: tenantID, KnowledgeBaseID: kbID, Source: url}
	source := types.SourceInfo{OriginalURL: url}
	if len(sourceInfos) > 0 {
		source = sourceInfos[0]
		if source.OriginalURL == "" {
			source.OriginalURL = url
		}
	}
	if err := incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: source}); err != nil {
		return nil, err
	}
	existing, err := s.checkMessageDuplicate(ctx, incoming, "")
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, types.NewDuplicateURLError(existing)
	}
	exists, existingKnowledge, err := s.repo.CheckKnowledgeExists(ctx, tenantID, kbID, &types.KnowledgeCheckParams{
		Type:     "url",
		URL:      url,
		FileHash: fileHash,
	})
	if err != nil {
		logger.Errorf(ctx, "Failed to check knowledge existence: %v", err)
		return nil, err
	}
	if exists {
		if err := s.recordKnownMessageRepost(ctx, existingKnowledge, kbID, source, "url"); err != nil {
			return nil, err
		}
		logger.Infof(ctx, "URL already exists: %s", url)
		// Update creation time for existing knowledge
		existingKnowledge.CreatedAt = time.Now()
		existingKnowledge.UpdatedAt = time.Now()
		if err := s.repo.UpdateKnowledge(ctx, existingKnowledge); err != nil {
			logger.Errorf(ctx, "Failed to update existing knowledge: %v", err)
			return nil, err
		}
		return existingKnowledge, types.NewDuplicateURLError(existingKnowledge)
	}

	// Create knowledge record
	logger.Info(ctx, "Creating knowledge record")
	knowledge := &types.Knowledge{
		ID:               uuid.New().String(),
		TenantID:         tenantID,
		KnowledgeBaseID:  kbID,
		Type:             "url",
		Channel:          defaultChannel(channel),
		Title:            title,
		Source:           url,
		FileType:         "html",
		FileHash:         fileHash,
		ParseStatus:      "pending",
		EnableStatus:     "disabled",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		EmbeddingModelID: kb.EmbeddingModelID,
	}

	{
		source := types.SourceInfo{OriginalURL: rawURL}
		if len(sourceInfos) > 0 {
			source = sourceInfos[0]
			if source.OriginalURL == "" {
				source.OriginalURL = rawURL
			}
		}
		if err := knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: "url", Decision: "pending", SourceInfo: source}); err != nil {
			return nil, err
		}
	}

	// Save knowledge record
	logger.Infof(ctx, "Saving knowledge record to database, ID: %s", knowledge.ID)
	_, err = ApplyKnowledgeProcessOverrides(ctx, kb, knowledge, processOverrides, []string{"html"}, enableMultimodel)
	if err != nil {
		return nil, err
	}

	setMessageFingerprint(knowledge, "")

	existing, err = s.createMessageWithDeduplication(ctx, knowledge, "")
	if err == nil && existing != nil {
		return existing, types.NewDuplicateURLError(existing)
	}

	if err != nil {
		logger.Errorf(ctx, "Failed to create knowledge record: %v", err)
		return nil, err
	}

	// Enqueue URL processing task to PostgreSQL
	logger.Info(ctx, "Enqueuing URL processing task to PostgreSQL")

	lang := types.LanguageFromContextOrDefault(ctx)
	taskPayload := types.DocumentProcessPayload{
		TenantID:        tenantID,
		KnowledgeID:     knowledge.ID,
		KnowledgeBaseID: kbID,
		URL:             url,
		Language:        lang,
	}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal URL process task payload: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		return knowledge, nil
	}

	task := taskqueue.NewTask(
		types.TypeDocumentProcess,
		payloadBytes,
		documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
	)
	info, err := s.task.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue URL process task: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		return knowledge, nil
	}
	logger.Infof(ctx, "Enqueued URL process task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, knowledge.ID)

	logger.Infof(ctx, "Knowledge from URL created successfully, ID: %s", knowledge.ID)
	return knowledge, nil
}

// maxFileURLSize is the maximum allowed file size for file URL import (10MB)
const maxFileURLSize = 10 * 1024 * 1024

// extractFileNameFromURL extracts the filename from a URL path
func extractFileNameFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	base := path.Base(u.Path)
	if base == "." || base == "/" {
		return ""
	}
	return base
}

// extractFileNameFromContentDisposition extracts filename from Content-Disposition header
func extractFileNameFromContentDisposition(header string) string {
	// e.g. attachment; filename="document.pdf" or filename*=UTF-8''document.pdf
	for _, part := range strings.Split(header, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToLower(part), "filename=") {
			name := strings.TrimPrefix(part, "filename=")
			name = strings.TrimPrefix(part[len("filename="):], "")
			name = strings.Trim(name, `"'`)
			if name != "" {
				return name
			}
		}
	}
	return ""
}

// createKnowledgeFromFileURL is the internal implementation for file URL knowledge creation.
// Called by CreateKnowledgeFromURL when the URL is detected as a direct file download.
func (s *knowledgeService) createKnowledgeFromFileURL(
	ctx context.Context,
	kbID string,
	fileURL string,
	fileName string,
	fileType string,
	enableMultimodel *bool,
	title string,
	channel string,
	processOverrides *types.KnowledgeProcessOverrides, sourceInfos ...types.SourceInfo,
) (*types.Knowledge, error) {
	logger.Info(ctx, "Start creating knowledge from file URL")
	logger.Infof(ctx, "Knowledge base ID: %s, file URL: %s", kbID, fileURL)

	// Get knowledge base configuration
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return nil, err
	}

	// Validate URL format and security (static check only, no HEAD request)
	if !isValidURL(fileURL) || !secutils.IsValidURL(fileURL) {
		logger.Error(ctx, "Invalid or unsafe file URL format")
		return nil, ErrInvalidURL
	}
	if err := secutils.ValidateURLForSSRF(fileURL); err != nil {
		logger.Errorf(ctx, "File URL rejected for SSRF protection: %s, err: %v", fileURL, err)
		return nil, ErrInvalidURL
	}

	// Resolve fileName: user-provided > extracted from URL path
	if fileName == "" {
		fileName = extractFileNameFromURL(fileURL)
	}
	if fileName != "" {
		safeFilename, ok := secutils.ValidateInput(fileName)
		if !ok {
			logger.Errorf(ctx, "Invalid filename: %s", fileName)
			return nil, werrors.NewValidationError("文件名包含非法字符")
		}
		fileName = safeFilename
	}

	// Resolve fileType: user-provided > inferred from fileName (which already
	// falls back to the URL path above). getFileType never returns empty, so an
	// undeterminable type surfaces as "unknown" and is rejected below.
	if fileType == "" {
		fileType = getFileType(fileName)
	}
	fileType = normalizeFileExtension(fileType)

	// Use title as display name if fileName is still empty
	displayName := fileName
	if displayName == "" {
		displayName = title
	}
	if displayName == "" {
		// Fallback: use last segment of URL
		displayName = extractFileNameFromURL(fileURL)
	}
	if displayName == "" {
		displayName = fileURL
	}

	// Check for duplicate (by URL hash)
	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	fileHash := calculateStr(fileURL)

	incoming := &types.Knowledge{TenantID: tenantID, KnowledgeBaseID: kbID, Source: fileURL}
	source := types.SourceInfo{OriginalURL: fileURL}
	if len(sourceInfos) > 0 {
		source = sourceInfos[0]
		if source.OriginalURL == "" {
			source.OriginalURL = fileURL
		}
	}
	if err := incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: source}); err != nil {
		return nil, err
	}
	existing, err := s.checkMessageDuplicate(ctx, incoming, "")
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return existing, types.NewDuplicateURLError(existing)
	}
	exists, existingKnowledge, err := s.repo.CheckKnowledgeExists(ctx, tenantID, kbID, &types.KnowledgeCheckParams{
		Type:     "file_url",
		URL:      fileURL,
		FileHash: fileHash,
	})
	if err != nil {
		logger.Errorf(ctx, "Failed to check knowledge existence: %v", err)
		return nil, err
	}
	if exists {
		if err := s.recordKnownMessageRepost(ctx, existingKnowledge, kbID, source, "url"); err != nil {
			return nil, err
		}
		logger.Infof(ctx, "File URL already exists: %s", fileURL)
		existingKnowledge.CreatedAt = time.Now()
		existingKnowledge.UpdatedAt = time.Now()
		if err := s.repo.UpdateKnowledge(ctx, existingKnowledge); err != nil {
			logger.Errorf(ctx, "Failed to update existing knowledge: %v", err)
			return nil, err
		}
		return existingKnowledge, types.NewDuplicateURLError(existingKnowledge)
	}

	// Create knowledge record
	knowledge := &types.Knowledge{
		ID:               uuid.New().String(),
		TenantID:         tenantID,
		KnowledgeBaseID:  kbID,
		Type:             "file_url",
		Channel:          defaultChannel(channel),
		Title:            title,
		FileName:         displayName,
		FileType:         fileType,
		Source:           fileURL,
		FileHash:         fileHash,
		ParseStatus:      "pending",
		EnableStatus:     "disabled",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		EmbeddingModelID: kb.EmbeddingModelID,
	}

	{
		source := types.SourceInfo{OriginalURL: fileURL}
		if len(sourceInfos) > 0 {
			source = sourceInfos[0]
			if source.OriginalURL == "" {
				source.OriginalURL = fileURL
			}
		}
		if err := knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: "url", Decision: "pending", SourceInfo: source}); err != nil {
			return nil, err
		}
	}
	if knowledge.Title == "" {
		knowledge.Title = displayName
	}

	_, err = resolveFileImportProcessConfig(ctx, kb, fileType, processOverrides, enableMultimodel)
	if err != nil {
		return nil, err
	}
	if processOverrides != nil {
		if err := knowledge.SetProcessOverrides(processOverrides); err != nil {
			logger.Errorf(ctx, "Failed to set process overrides: %v", err)
			return nil, err
		}
	}

	setMessageFingerprint(knowledge, "")

	existing, err = s.createMessageWithDeduplication(ctx, knowledge, "")
	if err == nil && existing != nil {
		return existing, types.NewDuplicateURLError(existing)
	}

	if err != nil {
		logger.Errorf(ctx, "Failed to create knowledge record: %v", err)
		return nil, err
	}

	// Build async task payload

	lang := types.LanguageFromContextOrDefault(ctx)
	taskPayload := types.DocumentProcessPayload{
		TenantID:        tenantID,
		KnowledgeID:     knowledge.ID,
		KnowledgeBaseID: kbID,
		FileURL:         fileURL,
		FileName:        fileName,
		FileType:        fileType,
		Language:        lang,
	}

	payloadBytes, err := json.Marshal(taskPayload)
	if err != nil {
		logger.Errorf(ctx, "Failed to marshal file URL process task payload: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		return knowledge, nil
	}

	task := taskqueue.NewTask(
		types.TypeDocumentProcess,
		payloadBytes,
		documentProcessTaskOptions(s.config)...,
	)
	info, err := s.task.Enqueue(task)
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue file URL process task: %v", err)
		s.markKnowledgeEnqueueFailed(ctx, knowledge)
		return knowledge, nil
	}
	logger.Infof(ctx, "Enqueued file URL process task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, knowledge.ID)

	logger.Infof(ctx, "Knowledge from file URL created successfully, ID: %s", knowledge.ID)
	return knowledge, nil
}

// CreateKnowledgeFromPassage creates a knowledge entry from text passages
func (s *knowledgeService) CreateKnowledgeFromPassage(ctx context.Context,
	kbID string, passage []string, channel string,
) (*types.Knowledge, error) {
	return s.createKnowledgeFromPassageInternal(ctx, kbID, passage, false, channel)
}

// CreateKnowledgeFromPassageSync creates a knowledge entry from text passages and waits for indexing to complete.
func (s *knowledgeService) CreateKnowledgeFromPassageSync(ctx context.Context,
	kbID string, passage []string, channel string,
) (*types.Knowledge, error) {
	return s.createKnowledgeFromPassageInternal(ctx, kbID, passage, true, channel)
}

// CreateKnowledgeFromManual creates or saves manual Markdown knowledge content.
func (s *knowledgeService) CreateKnowledgeFromManual(ctx context.Context,
	kbID string, payload *types.ManualKnowledgePayload, channel string,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.CreateKnowledgeFromManual(ctx, kbID, payload, channel)
		})
	}

	logger.Info(ctx, "Start creating manual knowledge entry")

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
		status = types.ManualKnowledgeStatusDraft
	}
	if status != types.ManualKnowledgeStatusDraft && status != types.ManualKnowledgeStatusPublish {
		return nil, werrors.NewValidationError("状态仅支持 draft 或 publish")
	}

	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return nil, err
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	now := time.Now()
	title := safeTitle
	if title == "" {
		title = fmt.Sprintf("Knowledge-%s", now.Format("20060102-150405"))
	}

	if status == types.ManualKnowledgeStatusPublish {
		incoming := &types.Knowledge{TenantID: tenantID, KnowledgeBaseID: kbID}
		if len(payload.SourceMetadata) > 0 {
			metadata, err := json.Marshal(payload.SourceMetadata)
			if err != nil {
				return nil, err
			}
			incoming.Metadata = types.JSON(metadata)
		}
		if err := incoming.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{SourceInfo: payload.SourceInfo}); err != nil {
			return nil, err
		}
		existing, err := s.checkMessageDuplicate(ctx, incoming, cleanContent)
		if err != nil {
			return nil, err
		}
		if existing != nil {
			return existing, &types.DuplicateKnowledgeError{Message: "Message already exists", Knowledge: existing}
		}
	}

	fileName := ensureManualFileName(title)
	meta := types.NewManualKnowledgeMetadata(cleanContent, status, 1)

	knowledge := &types.Knowledge{
		ID:               uuid.New().String(),
		TenantID:         tenantID,
		KnowledgeBaseID:  kbID,
		Type:             types.KnowledgeTypeManual,
		Channel:          defaultChannel(channel),
		Title:            title,
		Description:      "",
		Source:           types.KnowledgeTypeManual,
		ParseStatus:      types.ManualKnowledgeStatusDraft,
		EnableStatus:     "disabled",
		CreatedAt:        now,
		UpdatedAt:        now,
		EmbeddingModelID: kb.EmbeddingModelID,
		FileName:         fileName,
		FileType:         types.KnowledgeTypeManual,
	}
	if len(payload.SourceMetadata) > 0 {
		metadata, err := json.Marshal(payload.SourceMetadata)
		if err != nil {
			return nil, err
		}
		knowledge.Metadata = types.JSON(metadata)
	}
	if err := knowledge.SetManualMetadata(meta); err != nil {
		logger.Errorf(ctx, "Failed to set manual metadata: %v", err)
		return nil, err
	}
	// Keep the full message body
	// outside the row so a rejected message can be previewed/retried without
	// turning the metadata column into an unbounded original archive.
	{
		fileSvc := s.resolveFileService(ctx, kb)
		originalPath, err := fileSvc.SaveBytes(ctx, []byte(cleanContent), tenantID, fileName, false)
		if err != nil {
			return nil, err
		}
		intake := &types.KnowledgeIntakeMetadata{Source: "manual", OriginalPath: originalPath, Decision: "pending", SourceInfo: payload.SourceInfo}
		if err := knowledge.SetIntakeMetadata(intake); err != nil {
			_ = fileSvc.DeleteFile(ctx, originalPath)
			return nil, err
		}
		meta.Content = ""
		if err := knowledge.SetManualMetadata(meta); err != nil {
			_ = fileSvc.DeleteFile(ctx, originalPath)
			return nil, err
		}
	}
	knowledge.EnsureManualDefaults()

	if status == types.ManualKnowledgeStatusPublish {
		knowledge.ParseStatus = "pending"
	}

	if status == types.ManualKnowledgeStatusPublish {
		if _, err := ApplyKnowledgeProcessOverrides(ctx, kb, knowledge, payload.ProcessConfig, nil, nil); err != nil {
			return nil, err
		}
	}

	var existing *types.Knowledge
	if status == types.ManualKnowledgeStatusPublish {
		existing, err = s.createMessageWithDeduplication(ctx, knowledge, cleanContent)
	} else {
		err = s.repo.CreateKnowledge(ctx, knowledge)
	}
	if err != nil || existing != nil {
		if intake, _ := knowledge.IntakeMetadata(); intake != nil && intake.OriginalPath != "" {
			if cleanupErr := s.resolveFileService(ctx, kb).DeleteFile(ctx, intake.OriginalPath); cleanupErr != nil {
				return nil, cleanupErr
			}
		}
		if err != nil {
			return nil, err
		}
		return existing, &types.DuplicateKnowledgeError{Message: "Message already exists", Knowledge: existing}
	}

	// Claim referenced files before publishing so a saved draft retains them.
	s.bindContentResources(ctx, tenantID, knowledge.ID, cleanContent)

	if status == types.ManualKnowledgeStatusPublish {
		logger.Infof(ctx, "Manual knowledge created, enqueuing async processing task, ID: %s", knowledge.ID)
		_, err := s.enqueueManualProcessing(ctx, knowledge, cleanContent, false)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue manual processing task for new knowledge: %v", err)
			// Non-fatal: mark as failed so user can retry
			knowledge.ParseStatus = "failed"
			knowledge.ErrorMessage = "Failed to enqueue processing task"
			s.repo.UpdateKnowledge(ctx, knowledge)
			s.failIntake(ctx, knowledge)
		}
	}

	return knowledge, nil
}

// createKnowledgeFromPassageInternal consolidates the common logic for creating knowledge from passages.
// When syncMode is true, chunk processing is performed synchronously; otherwise, it's processed asynchronously.
func (s *knowledgeService) createKnowledgeFromPassageInternal(ctx context.Context,
	kbID string, passage []string, syncMode bool, channel string,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.createKnowledgeFromPassageInternal(ctx, kbID, passage, syncMode, channel)
		})
	}

	if syncMode {
		logger.Info(ctx, "Start creating knowledge from passage (sync)")
	} else {
		logger.Info(ctx, "Start creating knowledge from passage")
	}
	logger.Infof(ctx, "Knowledge base ID: %s, passage count: %d", kbID, len(passage))

	// 验证段落内容安全性
	safePassages := make([]string, 0, len(passage))
	for i, p := range passage {
		safePassage, isValid := secutils.ValidateInput(p)
		if !isValid {
			logger.Errorf(ctx, "Invalid passage content at index %d", i)
			return nil, werrors.NewValidationError(fmt.Sprintf("段落 %d 包含非法内容", i+1))
		}
		safePassages = append(safePassages, safePassage)
	}

	// Get knowledge base configuration
	logger.Info(ctx, "Getting knowledge base configuration")
	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base: %v", err)
		return nil, err
	}

	// Create knowledge record
	if syncMode {
		logger.Info(ctx, "Creating knowledge record (sync)")
	} else {
		logger.Info(ctx, "Creating knowledge record")
	}
	knowledge := &types.Knowledge{
		ID:               uuid.New().String(),
		TenantID:         ctx.Value(types.TenantIDContextKey).(uint64),
		KnowledgeBaseID:  kbID,
		Type:             "passage",
		Channel:          defaultChannel(channel),
		ParseStatus:      "pending",
		EnableStatus:     "disabled",
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
		EmbeddingModelID: kb.EmbeddingModelID,
	}

	// Save knowledge record
	logger.Infof(ctx, "Saving knowledge record to database, ID: %s", knowledge.ID)
	if err := s.repo.CreateKnowledge(ctx, knowledge); err != nil {
		logger.Errorf(ctx, "Failed to create knowledge record: %v", err)
		return nil, err
	}
	// Process passages
	if syncMode {
		logger.Info(ctx, "Processing passage synchronously")
		s.processDocumentFromPassage(ctx, kb, knowledge, safePassages)
		logger.Infof(ctx, "Knowledge from passage created successfully (sync), ID: %s", knowledge.ID)
	} else {
		// Enqueue passage processing task to PostgreSQL
		logger.Info(ctx, "Enqueuing passage processing task to PostgreSQL")
		tenantID := ctx.Value(types.TenantIDContextKey).(uint64)

		// Check question generation config
		lang := types.LanguageFromContextOrDefault(ctx)
		taskPayload := types.DocumentProcessPayload{
			TenantID:        tenantID,
			KnowledgeID:     knowledge.ID,
			KnowledgeBaseID: kbID,
			Passages:        safePassages,
			Language:        lang,
		}

		payloadBytes, err := json.Marshal(taskPayload)
		if err != nil {
			logger.Errorf(ctx, "Failed to marshal passage process task payload: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, knowledge)
			// 即使入队失败，也返回knowledge
			return knowledge, nil
		}

		task := taskqueue.NewTask(
			types.TypeDocumentProcess,
			payloadBytes,
			documentProcessTaskOptions(s.config, taskqueue.MaxRetry(3))...,
		)
		info, err := s.task.Enqueue(task)
		if err != nil {
			logger.Errorf(ctx, "Failed to enqueue passage process task: %v", err)
			s.markKnowledgeEnqueueFailed(ctx, knowledge)
			return knowledge, nil
		}
		logger.Infof(ctx, "Enqueued passage process task: id=%s queue=%s knowledge_id=%s", info.ID, info.Queue, knowledge.ID)
		logger.Infof(ctx, "Knowledge from passage created successfully, ID: %s", knowledge.ID)
	}
	return knowledge, nil
}

// UpdateManualKnowledge updates manual Markdown knowledge content.
// For publish status, the heavy operations (cleanup old indexes, re-chunking,
// re-embedding) are offloaded to a PostgreSQL task so the HTTP response returns quickly.
func (s *knowledgeService) UpdateManualKnowledge(ctx context.Context,
	knowledgeID string, payload *types.ManualKnowledgePayload,
) (*types.Knowledge, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.Knowledge, error) {
			return copy.UpdateManualKnowledge(ctx, knowledgeID, payload)
		})
	}

	logger.Info(ctx, "Start updating manual knowledge entry")
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
		status = types.ManualKnowledgeStatusDraft
	}
	if status != types.ManualKnowledgeStatusDraft && status != types.ManualKnowledgeStatusPublish {
		return nil, werrors.NewValidationError("状态仅支持 draft 或 publish")
	}

	tenantID := ctx.Value(types.TenantIDContextKey).(uint64)
	existing, err := s.repo.GetKnowledgeByID(ctx, tenantID, knowledgeID)
	if err != nil {
		logger.Errorf(ctx, "Failed to load knowledge: %v", err)
		return nil, err
	}
	if existing.SourceKind != "" && existing.SourceKind != "article" {
		return nil, werrors.NewBadRequestError("vault documents are edited directly in their files")
	}
	if existing.VaultPath != "" {
		return nil, werrors.NewBadRequestError("an approved article body cannot be changed")
	}
	if !existing.IsManual() {
		return nil, werrors.NewBadRequestError("仅支持手工知识的在线编辑")
	}
	if existing.ParseStatus == types.ParseStatusPending || existing.ParseStatus == types.ParseStatusProcessing || existing.ParseStatus == types.ParseStatusFinalizing {
		return nil, werrors.NewBadRequestError("来源仍在处理中，请等待处理完成后再编辑")
	}

	kb, err := s.kbService.GetKnowledgeBaseByID(ctx, existing.KnowledgeBaseID)
	if err != nil {
		logger.Errorf(ctx, "Failed to get knowledge base for manual update: %v", err)
		return nil, err
	}
	if status == types.ManualKnowledgeStatusPublish {
	}
	intake, err := existing.IntakeMetadata()
	if err != nil {
		return nil, err
	}
	reviewedOriginal := intake != nil && intake.Decision == "approved"
	if intake == nil && existing.ParseStatus == types.ParseStatusCompleted {
		reviewedOriginal = true
	}
	if reviewedOriginal {
		return nil, werrors.NewBadRequestError("an approved article body cannot be changed")
	}

	if intake != nil && intake.Decision == "rejected" && !types.IsAuthenticatedFromContext(ctx) {
		return nil, werrors.NewForbiddenError("an authenticated user is required to review intake")
	}
	if status == types.ManualKnowledgeStatusPublish {
		if _, err := ApplyKnowledgeProcessOverrides(ctx, kb, existing, payload.ProcessConfig, nil, nil); err != nil {
			return nil, err
		}
	}
	processingQueued := false
	if _, bound := s.task.(*transactionTaskEnqueuer); bound && status == types.ManualKnowledgeStatusPublish {
		// Workers only see this task after commit. Enqueue before replacing
		// the stored source so a queue failure leaves the old body intact.
		if _, err := s.enqueueManualProcessing(ctx, existing, cleanContent, true); err != nil {
			return existing, err
		}
		processingQueued = true
	}
	if intake != nil {
		intake.SourceInfo = mergeSourceInfo(intake.SourceInfo, payload.SourceInfo)
		intake.Decision, intake.Reason = "pending", ""
		if err := existing.SetIntakeMetadata(intake); err != nil {
			return nil, err
		}
		if err := s.saveIntakeOriginal(ctx, kb, existing, cleanContent, nil); err != nil {
			return nil, err
		}
	}

	var version int
	if meta, err := existing.ManualMetadata(); err == nil && meta != nil {
		version = meta.Version + 1
	} else {
		version = 1
	}

	meta := types.NewManualKnowledgeMetadata(cleanContent, status, version)
	if intake, _ := existing.IntakeMetadata(); intake != nil {
		meta.Content = ""
	}
	if err := existing.SetManualMetadata(meta); err != nil {
		logger.Errorf(ctx, "Failed to set manual metadata during update: %v", err)
		return nil, err
	}

	if safeTitle != "" {
		existing.Title = safeTitle
	} else if existing.Title == "" {
		existing.Title = fmt.Sprintf("手工知识-%s", time.Now().Format("20060102-150405"))
	}
	existing.FileName = ensureManualFileName(existing.Title)
	existing.FileType = types.KnowledgeTypeManual
	existing.Type = types.KnowledgeTypeManual
	existing.Source = types.KnowledgeTypeManual
	existing.EnableStatus = "disabled"
	existing.UpdatedAt = time.Now()
	existing.EmbeddingModelID = kb.EmbeddingModelID

	if status == types.ManualKnowledgeStatusDraft {
		existing.ParseStatus = types.ManualKnowledgeStatusDraft
		existing.Description = ""
		existing.ProcessedAt = nil

		if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
			logger.Errorf(ctx, "Failed to persist manual draft: %v", err)
			return nil, err
		}
		s.bindContentResources(ctx, tenantID, existing.ID, cleanContent)
		return existing, nil
	}

	// Publish: persist pending status and enqueue async task for cleanup + re-indexing
	existing.ParseStatus = "pending"
	existing.Description = ""
	existing.ProcessedAt = nil

	if err := s.repo.UpdateKnowledge(ctx, existing); err != nil {
		logger.Errorf(ctx, "Failed to persist manual knowledge before indexing: %v", err)
		return nil, err
	}

	logger.Infof(ctx, "Manual knowledge updated, enqueuing async processing task, ID: %s", existing.ID)
	if !processingQueued {
		_, err = s.enqueueManualProcessing(ctx, existing, cleanContent, true)
	}
	if err != nil {
		logger.Errorf(ctx, "Failed to enqueue manual processing task: %v", err)
		// Non-fatal: mark as failed so user can retry
		existing.ParseStatus = "failed"
		existing.ErrorMessage = "Failed to enqueue processing task"
		s.repo.UpdateKnowledge(ctx, existing)
		return nil, werrors.NewInternalServerError("Failed to submit processing task")
	}
	return existing, nil
}

func mergeSourceInfo(old, updated types.SourceInfo) types.SourceInfo {
	if updated.Author == "" {
		updated.Author = old.Author
	}
	if updated.Publisher == "" {
		updated.Publisher = old.Publisher
	}
	if updated.OriginalURL == "" {
		updated.OriginalURL = old.OriginalURL
		updated.OriginalURLSource = old.OriginalURLSource
	}
	if updated.PublishedAt == "" {
		updated.PublishedAt = old.PublishedAt
	}
	return updated
}

// enqueueManualProcessing enqueues a manual:process PostgreSQL task for async cleanup + re-indexing.
func (s *knowledgeService) enqueueManualProcessing(ctx context.Context,
	knowledge *types.Knowledge, content string, needCleanup bool, extraOptions ...taskqueue.Option,
) (string, error) {
	requestID, _ := types.RequestIDFromContext(ctx)
	payload := types.ManualProcessPayload{
		RequestId:       requestID,
		TenantID:        knowledge.TenantID,
		KnowledgeID:     knowledge.ID,
		KnowledgeBaseID: knowledge.KnowledgeBaseID,
		Content:         content,
		NeedCleanup:     needCleanup,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal manual process payload: %w", err)
	}

	options := append([]taskqueue.Option{taskqueue.Queue(types.QueueDefault), taskqueue.MaxRetry(3), taskqueue.Timeout(30 * time.Minute)}, extraOptions...)
	task := taskqueue.NewTask(types.TypeManualProcess, payloadBytes, options...)
	info, err := s.task.Enqueue(task)
	if err != nil {
		return "", fmt.Errorf("failed to enqueue manual process task: %w", err)
	}
	logger.Infof(ctx, "Enqueued manual process task: knowledge_id=%s, task_id=%s", knowledge.ID, info.ID)
	return info.ID, nil
}

// markKnowledgeEnqueueFailed prevents a durable knowledge row from remaining
// indefinitely "pending" when its background processing task was never
// created. The API may still return the row so callers can retry it.
func (s *knowledgeService) markKnowledgeEnqueueFailed(ctx context.Context, knowledge *types.Knowledge) {
	if knowledge == nil {
		return
	}
	defer s.failIntake(ctx, knowledge)
	knowledge.ParseStatus = "failed"
	knowledge.ErrorMessage = "Failed to enqueue processing task"
	if err := s.repo.UpdateKnowledge(ctx, knowledge); err != nil {
		logger.Errorf(ctx, "Failed to mark knowledge as failed after enqueue error: %v", err)
	}
}

func ensureManualFileName(title string) string {
	if title == "" {
		return fmt.Sprintf("manual-%s%s", time.Now().Format("20060102-150405"), manualFileExtension)
	}
	trimmed := strings.TrimSpace(title)
	if strings.HasSuffix(strings.ToLower(trimmed), manualFileExtension) {
		return trimmed
	}
	return trimmed + manualFileExtension
}

// sanitizeManualDownloadFilename converts a knowledge title into a safe .md
// download filename. Characters that are illegal or dangerous in HTTP header
// values and file-system paths are removed or replaced; a blank result falls
// back to "untitled".
func sanitizeManualDownloadFilename(title string) string {
	safeName := strings.NewReplacer(
		"\n", "", "\r", "", "\t", "", "/", "-", "\\", "-", "\"", "'",
	).Replace(title)
	if strings.TrimSpace(safeName) == "" {
		safeName = "untitled"
	}
	if !strings.HasSuffix(strings.ToLower(safeName), manualFileExtension) {
		safeName += manualFileExtension
	}
	return safeName
}

// bindContentResources claims every stored file the body references on behalf
// of a knowledge entry.
//
// A manual document may refer to previously stored resources. Claims keep
// those files available while the document uses them. References from another
// workspace are skipped.
//
// Best-effort by design: the document is already saved, and a missed claim
// degrades to the old behaviour rather than failing the save.
func (s *knowledgeService) bindContentResources(
	ctx context.Context, tenantID uint64, knowledgeID, content string,
) {
	if s.resourceCatalog == nil || knowledgeID == "" {
		return
	}
	for _, ref := range types.ScanResourceReferences(content) {
		resource, err := s.resourceCatalog.Resolve(ctx, ref)
		if err != nil || resource == nil {
			logger.Warnf(ctx, "Skip binding unknown resource %s to knowledge %s: %v", ref, knowledgeID, err)
			continue
		}
		if resource.TenantID != tenantID {
			logger.Warnf(ctx, "Skip binding cross-workspace resource %s to knowledge %s", ref, knowledgeID)
			continue
		}
		if err := s.resourceCatalog.Bind(
			ctx, ref, types.ResourceOwnerKnowledge, knowledgeID, types.ResourceRelationAttachment,
		); err != nil {
			logger.Warnf(ctx, "Failed to bind resource %s to knowledge %s: %v", ref, knowledgeID, err)
		}
	}
}

func (s *knowledgeService) triggerManualProcessing(ctx context.Context,
	kb *types.KnowledgeBase, knowledge *types.Knowledge, content string, _ bool,
) error {
	defer func() {
		if knowledge.ParseStatus == types.ParseStatusFailed {
			s.failIntake(ctx, knowledge)
		}
	}()
	clean := strings.TrimSpace(content)
	if clean == "" {
		return errors.New("manual article body is empty")
	}

	// Resolve embedded data:base64 images and remote http(s) images → storage, replace URLs.
	// Runs before chunking so chunks contain stable provider:// URLs.
	var resolvedImages []docparser.StoredImage
	if s.imageResolver != nil && !(s.vault != nil && knowledge.VaultPath != "" && knowledge.ProcessingMode == types.ProcessingModeFull) {
		fileSvc := s.resolveFileService(ctx, kb)
		afterDataURI, fromDataURI, dataErr := s.imageResolver.ResolveAndStore(ctx, &types.ReadResult{MarkdownContent: clean}, fileSvc, knowledge.TenantID)
		if err := s.recordIntakeImages(ctx, knowledge, fromDataURI); err != nil {
			knowledge.ParseStatus = types.ParseStatusFailed
			return errors.New("manual article processing failed")
		}
		if dataErr != nil {
			if intake, _ := knowledge.IntakeMetadata(); intake != nil {
				knowledge.ParseStatus, knowledge.ErrorMessage, knowledge.UpdatedAt = types.ParseStatusFailed, "failed to resolve intake images", time.Now()
				_ = s.repo.UpdateKnowledge(ctx, knowledge)
				return errors.New("manual article processing failed")
			}
		}
		if len(fromDataURI) > 0 {
			logger.Infof(ctx, "Resolved %d data-URI images for manual knowledge %s", len(fromDataURI), knowledge.ID)
			clean = afterDataURI
			resolvedImages = append(resolvedImages, fromDataURI...)
		}
		updatedContent, storedImages, resolveErr := s.imageResolver.ResolveRemoteImages(ctx, clean, fileSvc, knowledge.TenantID)
		if err := s.recordIntakeImages(ctx, knowledge, storedImages); err != nil {
			knowledge.ParseStatus = types.ParseStatusFailed
			return errors.New("manual article processing failed")
		}
		if resolveErr != nil {
			logger.Warnf(ctx, "Remote image resolution partially failed: %v", resolveErr)
			if intake, _ := knowledge.IntakeMetadata(); intake != nil {
				knowledge.ParseStatus, knowledge.ErrorMessage, knowledge.UpdatedAt = types.ParseStatusFailed, "failed to resolve intake images", time.Now()
				_ = s.repo.UpdateKnowledge(ctx, knowledge)
				return errors.New("manual article processing failed")
			}
		}
		if updatedContent != "" {
			clean = updatedContent
		}
		if len(storedImages) > 0 {
			logger.Infof(ctx, "Resolved %d remote images for manual knowledge %s", len(storedImages), knowledge.ID)
			resolvedImages = append(resolvedImages, storedImages...)
		}
		if intake, _ := knowledge.IntakeMetadata(); intake != nil && s.hasUnstoredArticleImages(knowledge, clean) {
			knowledge.ParseStatus, knowledge.ErrorMessage, knowledge.UpdatedAt = types.ParseStatusFailed, "intake has unstored content images", time.Now()
			_ = s.repo.UpdateKnowledge(ctx, knowledge)
			return errors.New("manual article processing failed")
		}
	}
	if err := extractIntakeOriginalLink(knowledge, clean, nil, knowledge.FileType, nil); err != nil {
		knowledge.ParseStatus = types.ParseStatusFailed
		return err
	}
	if intake, _ := knowledge.IntakeMetadata(); intake != nil && intake.Decision != "approved" {
		existing, err := s.checkMessageDuplicate(ctx, knowledge, clean)
		if err != nil {
			knowledge.ParseStatus = types.ParseStatusFailed
			return errors.New("manual article processing failed")
		}
		if existing != nil {
			if err := s.stopDuplicateProcessing(ctx, knowledge); err != nil {
				knowledge.ParseStatus = types.ParseStatusFailed
				return err
			}
			return nil
		}
	}

	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		if s.hasUnstoredArticleImages(knowledge, clean) {
			knowledge.ParseStatus = types.ParseStatusFailed
			return errors.New("manual article processing failed")
		}
		if err := s.saveIntakeOriginal(ctx, kb, knowledge, clean, nil); err != nil {
			knowledge.ParseStatus = types.ParseStatusFailed
			return errors.New("manual article processing failed")
		}
	}

	// Re-claim the body's stored files. This runs after cleanupKnowledgeResources
	// released the previous run's claims, so a republished document keeps the
	// files its new body still references.
	s.bindContentResources(ctx, knowledge.TenantID, knowledge.ID, clean)

	// Keep manually entered CRLF text aligned with the LF values sent by the
	// chunking preview endpoint.
	clean = chunker.NormalizeLineEndings(clean)

	processOverrides, _ := knowledge.ProcessOverrides()
	eff := ResolveProcessConfig(kb, processOverrides)

	if intake, _ := knowledge.IntakeMetadata(); intake != nil {
		rejected, err := s.reviewIntake(ctx, kb, knowledge, clean)
		if err != nil {
			s.markArticleProcessingFailed(ctx, knowledge, err)
			return err
		}
		if rejected {
			return nil
		}
	}
	if err := s.processArticleBody(ctx, kb, knowledge, clean, resolvedImages, eff); err != nil {
		s.markArticleProcessingFailed(ctx, knowledge, err)
		return err
	}
	return nil
}
