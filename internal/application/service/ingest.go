package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

var _ interfaces.IngestService = (*entryService)(nil)

// Ingest never retrieves source material from a URL. Supplied bytes are parsed
// locally, then reviewed and processed by the existing article pipeline.
func (s *entryService) Ingest(ctx context.Context, request *types.IngestRequest) (result *types.IngestResult, err error) {
	if err := validateIngestRequest(request); err != nil {
		return nil, err
	}
	if s.needsTaskTransaction() {
		changes := &ingestFileChanges{}
		ctx = context.WithValue(ctx, ingestFileChangesKey{}, changes)
		result, err = entryTaskTransaction(ctx, s, func(copy *entryService) (*types.IngestResult, error) {
			result, err := copy.Ingest(ctx, request)
			if err == nil && result != nil && (result.Status == "exists" || result.Status == "discarded") {
				// Cleanup participates in the transaction: a failed cleanup must
				// not commit a repost or an operation reported as invalid.
				err = changes.removeUnusedStoredFiles(ctx, copy.fileSvc)
			}
			return result, err
		})
		if err != nil {
			err = errors.Join(err, changes.rollback(s.vault))
		} else {
			err = errors.Join(err, changes.finish())
		}
		return result, err
	}
	ctx, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	kb, err := s.kbService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	if kb.SpaceID != spaceID {
		return nil, werrors.NewForbiddenError("knowledge base belongs to another space")
	}
	now := time.Now()
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = strings.TrimSpace(request.URL)
	}
	if title == "" {
		title = "待审文章"
	}
	if len([]rune(title)) > 255 {
		title = string([]rune(title)[:255])
	}
	tags := request.AgentTags
	if tags == nil {
		tags = []string{}
	}
	tagJSON, _ := json.Marshal(tags)
	article := &types.Entry{ID: uuid.NewString(), SpaceID: spaceID, KnowledgeBaseID: kb.ID,
		Type: types.EntryTypeManual, SourceKind: "article", Source: strings.TrimSpace(request.URL), Channel: types.ChannelAPI,
		Title: title, FileName: sanitizeManualDownloadFilename(title), FileType: types.EntryTypeManual,
		ParseStatus: types.ParseStatusPending, EnableStatus: "disabled", CreatedAt: now, UpdatedAt: now,
		EmbeddingModelID: kb.EmbeddingModelID, CollectReason: strings.TrimSpace(request.CollectReason),
		AgentSummary: request.AgentSummary, AgentTags: types.JSON(tagJSON), SourceName: request.SourceName,
		Acquisition: request.Acquisition, ContentTypeHint: request.ContentTypeHint,
		IngestAPIKeyID: keyID, IngestAPIKeyName: keyName, LastIngestAPIKeyID: keyID, LastIngestAPIKeyName: keyName}
	intake := &types.EntryIntakeMetadata{Source: "ingest", Decision: "pending", SourceInfo: types.SourceInfo{
		OriginalURL: article.Source, Author: request.Author, Publisher: request.Publisher,
		PublishedAt: normalizeIntakePublicationDate(request.PublishedAt)}}
	if err := article.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	setMessageFingerprint(article, "")
	// Hold the existing collection lock through lookup, byte storage and enqueue.
	// Concurrent deliveries and supplements therefore commit one mutation.
	if err := lockMessageDeduplication(s.db.WithContext(ctx), article); err != nil {
		return nil, err
	}
	if discarded, err := s.lookupIngestDiscard(ctx, article.NormalizedURL); err != nil {
		return nil, err
	} else if discarded != nil {
		return ingestResult(nil, "discarded", "该链接已被丢弃，未再次收录。", discarded.Reason, discarded.EntryID), nil
	}
	if existing, err := s.findIngestURL(ctx, article.NormalizedURL, true); err != nil {
		return nil, err
	} else if existing != nil {
		if existing.Incomplete || ingestMissingContentPending(existing) {
			return s.supplementIngest(ctx, kb, existing, request, keyID, keyName)
		}
		if _, err := recordIngestDuplicate(ctx, s.db, existing, article, "url", 0); err != nil {
			return nil, err
		}
		if s.vault != nil {
			s.vault.ScheduleRefresh(ctx, existing.ID)
		}
		return ingestResult(existing, "exists", "该文章已收录。", "", ""), nil
	}
	if strings.TrimSpace(utils.CleanMarkdown(request.Content)) == "" && (request.File == nil || (request.File.AssetID == "" && request.File.Data == "")) && !hasIngestMissing(request.Missing, "content", "") {
		return nil, werrors.NewBadRequestError("missing content requires a missing item explaining the reason")
	}
	body, canonicalBody, err := s.storeIngestMaterials(ctx, kb, article, request)
	if err != nil {
		return nil, err
	}
	intake, _ = article.IntakeMetadata()
	if err := extractIntakeOriginalLink(article, body, nil, article.FileType, nil); err != nil {
		return nil, err
	}
	intake, _ = article.IntakeMetadata()
	setMessageFingerprint(article, canonicalBody)
	// Source evidence extracted from a supplied file also participates in lookup.
	if discarded, err := s.lookupIngestDiscard(ctx, article.NormalizedURL); err != nil {
		return nil, err
	} else if discarded != nil {
		return ingestResult(nil, "discarded", "该链接已被丢弃，未再次收录。", discarded.Reason, discarded.EntryID), nil
	}
	duplicate, method, distance, err := findMessageDuplicate(s.db.WithContext(ctx), article)
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		if _, err := recordIngestDuplicate(ctx, s.db, duplicate, article, method, distance); err != nil {
			return nil, err
		}
		if s.vault != nil {
			s.vault.ScheduleRefresh(ctx, duplicate.ID)
		}
		return ingestResult(duplicate, "exists", "该文章已收录。", "", ""), nil
	}
	if err := article.SetManualMetadata(types.NewManualEntryMetadata("", types.ManualEntryStatusPublish, 1)); err != nil {
		return nil, err
	}
	missingContent := !hasIngestBody(request, body) || hasIngestMissing(article.IngestMissing(), "content", "")
	if missingContent {
		intake.Decision, intake.Reason = "rejected", "正文缺失"
		article.ParseStatus = types.ParseStatusRejected
		article.Incomplete = true
		if err := article.SetIntakeMetadata(intake); err != nil {
			return nil, err
		}
	}
	if err := s.db.WithContext(ctx).Create(article).Error; err != nil {
		return nil, err
	}
	if err := s.referenceIngestMaterials(ctx, article, request); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", article.ID, nil, map[string]any{
		"url": article.Source, "acquisition": article.Acquisition, "collect_reason": article.CollectReason, "missing": article.IngestMissing()}); err != nil {
		return nil, err
	}
	s.bindContentResources(ctx, spaceID, article.ID, body)
	if missingContent {
		return ingestResult(article, "pending_review", "正文缺失，已保存到收录待审。", intake.Reason, ""), nil
	}
	return s.finishIngestReview(ctx, kb, article, body)
}

func (s *entryService) IngestBatch(ctx context.Context, request *types.IngestBatchRequest) (*types.IngestBatchResult, error) {
	if request == nil || len(request.Items) < 1 || len(request.Items) > 50 {
		return nil, werrors.NewBadRequestError("items must contain between 1 and 50 entries")
	}
	if _, _, _, err := s.ingestIdentity(ctx); err != nil {
		return nil, err
	}
	var total int64
	for _, item := range request.Items {
		total += ingestInlineSize(item.File, item.Images)
	}
	if total > types.MaxIngestInlineRequestBytes {
		return nil, werrors.NewBadRequestError("inline files exceed the 20 MiB request limit")
	}
	result := &types.IngestBatchResult{Items: make([]types.IngestResult, 0, len(request.Items))}
	for i := range request.Items {
		item, err := s.Ingest(ctx, &request.Items[i])
		if err != nil {
			var identityErr *ingestIdentityError
			if errors.As(err, &identityErr) {
				return nil, err
			}
			var app *werrors.AppError
			reason := "内部处理失败"
			message := "该条目收录失败，请稍后重试。"
			if errors.As(err, &app) && app.HTTPCode == http.StatusBadRequest {
				reason = app.Message
				message = "该条目无效，请检查获取方式、材料和缺失说明后重试。"
			}
			item = ingestResult(nil, "invalid", message, reason, "")
		}
		item.Index = i
		result.Items = append(result.Items, *item)
	}
	return result, nil
}

func (s *entryService) finishIngestReview(ctx context.Context, kb *types.KnowledgeBase, article *types.Entry, body string) (*types.IngestResult, error) {
	rejected, err := s.reviewIntake(ctx, kb, article, body)
	var deferred *types.ChatGPTDeferredError
	var options []taskqueue.Option
	if err != nil {
		if !errors.As(err, &deferred) {
			return nil, err
		}
		s.markArticleProcessingFailed(ctx, article, err)
		options = append(options, taskqueue.ProcessAt(deferred.RetryAt))
	}
	if rejected {
		intake, _ := article.IntakeMetadata()
		return ingestResult(article, "pending_review", "已保存，等待收录审核。", intake.Reason, ""), nil
	}
	if _, err := s.enqueueManualProcessing(ctx, article, body, false, options...); err != nil {
		return nil, err
	}
	return ingestResult(article, "accepted", "已收录，正在处理。", "", ""), nil
}

func ingestResult(article *types.Entry, status, message, reason, id string) *types.IngestResult {
	result := &types.IngestResult{EntryID: id, Status: status, Message: message, Reason: reason, Completeness: []types.IngestMissing{}, ImageFailures: []types.IngestImageFailure{}}
	if article != nil {
		result.EntryID = article.ID
		if article.Incomplete {
			result.Completeness = article.IngestMissing()
		}
	}
	if result.Completeness == nil {
		result.Completeness = []types.IngestMissing{}
	}
	return result
}

func recordIngestDuplicate(ctx context.Context, tx *gorm.DB, existing, incoming *types.Entry, method string, distance int) (bool, error) {
	if err := recordMessageRepost(tx, existing, incoming, method, distance); err != nil {
		return false, err
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("space_id=? AND id=?", existing.SpaceID, existing.ID).First(existing).Error; err != nil {
		return false, err
	}
	before := existing.CollectReason
	filled := strings.TrimSpace(before) == "" && incoming.CollectReason != ""
	updates := map[string]any{"last_ingest_api_key_id": incoming.LastIngestAPIKeyID, "last_ingest_api_key_name": incoming.LastIngestAPIKeyName, "updated_at": time.Now()}
	if filled {
		updates["collect_reason"] = incoming.CollectReason
		existing.CollectReason = incoming.CollectReason
	}
	if err := tx.Model(existing).Updates(updates).Error; err != nil {
		return false, err
	}
	err := NewOperationLogService(tx).RecordTx(ctx, tx, "ingest", existing.ID,
		map[string]any{"collect_reason": before}, map[string]any{"url": incoming.Source, "status": "exists", "collect_reason": existing.CollectReason})
	return filled, err
}

func validateIngestRequest(request *types.IngestRequest) error {
	bad := func(message string) error { return werrors.NewBadRequestError(message) }
	if request == nil {
		return bad("item is required")
	}
	if request.InputError != "" {
		return bad(request.InputError)
	}
	if request.Acquisition != "verbatim" && request.Acquisition != "extracted" && request.Acquisition != "model_written" {
		return bad("acquisition must be verbatim, extracted or model_written")
	}
	if request.URL != "" && (!validIngestURL(strings.TrimSpace(request.URL)) || len(request.URL) > 2048) {
		return bad("url must be an absolute HTTP or HTTPS URL of at most 2048 bytes")
	}
	if len([]rune(request.Content)) > manualContentMaxLength {
		return bad("content exceeds 200000 characters")
	}
	if len([]rune(strings.TrimSpace(request.Title))) > 255 {
		return bad("title exceeds 255 characters")
	}
	if request.ContentTypeHint != "" && types.NormalizeContentType(request.ContentTypeHint) == "" {
		return bad("invalid content_type_hint")
	}
	if len(request.Images) > 100 {
		return bad("images supports at most 100 items")
	}
	for i, missing := range request.Missing {
		if missing.Type != "content" && missing.Type != "file" && missing.Type != "image" {
			return bad(fmt.Sprintf("missing[%d] has an invalid type", i))
		}
		if strings.TrimSpace(missing.Reason) == "" {
			return bad(fmt.Sprintf("missing[%d].reason is required", i))
		}
		if missing.SourceURL != "" && !validIngestURL(missing.SourceURL) {
			return bad(fmt.Sprintf("missing[%d].source_url must be an HTTP or HTTPS URL", i))
		}
	}
	if validIngestURL(strings.TrimSpace(request.Content)) && !hasIngestMissing(request.Missing, "content", "") {
		return bad("content link requires a corresponding missing explanation")
	}

	if request.File != nil {
		file := request.File
		if file.AssetID != "" && file.Data != "" {
			return bad("file requires one of asset_id or data")
		}
		if file.AssetID == "" && file.Data == "" && !hasIngestMissing(request.Missing, "file", file.SourceURL) {
			return bad("file link requires a corresponding missing explanation")
		}
		if file.Data != "" {
			if _, err := decodeIngestInline(file.Data); err != nil {
				return bad(err.Error())
			}
		}
	}
	for i, image := range request.Images {
		if image.AssetID != "" && image.Data != "" {
			return bad(fmt.Sprintf("images[%d] requires one of asset_id or data", i))
		}
		if image.AssetID == "" && image.Data == "" {
			source := ingestImageSource(image)
			if source == "" || !validIngestURL(source) || !hasIngestMissing(request.Missing, "image", source) {
				return bad(fmt.Sprintf("images[%d] link requires a corresponding missing explanation", i))
			}
		}
		if image.Data != "" {
			if _, err := decodeIngestInline(image.Data); err != nil {
				return bad(err.Error())
			}
		}
		if image.SourceURL != "" && !validIngestURL(image.SourceURL) {
			return bad(fmt.Sprintf("images[%d].source_url is invalid", i))
		}
	}
	for i, item := range request.Thread {
		if strings.TrimSpace(item.Content) == "" {
			return bad(fmt.Sprintf("thread[%d].content is required", i))
		}
	}
	for i, quote := range request.Quotes {
		if strings.TrimSpace(quote.Content) == "" {
			return bad(fmt.Sprintf("quotes[%d].content is required", i))
		}
		if quote.URL != "" && !validIngestURL(quote.URL) {
			return bad(fmt.Sprintf("quotes[%d].url must be an HTTP or HTTPS URL", i))
		}
	}
	return nil
}

func validIngestURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.ContainsAny(raw, "\r\n\t")
}

func ingestImageSource(image types.IngestImage) string {
	if image.SourceURL != "" {
		return image.SourceURL
	}
	return image.URL
}

// A remote image is retained as provenance and never handed to a downloader.
func (s *entryService) resolveIngestImages(ctx context.Context, input []types.IngestImage, files interfaces.FileService, spaceID uint64) ([]string, []docparser.StoredImage, []types.IngestImageFailure) {
	refs := make([]string, len(input))
	stored := []docparser.StoredImage{}
	failures := []types.IngestImageFailure{}
	resolver := docparser.NewImageResolver()
	for i, image := range input {
		if image.Data == "" {
			refs[i] = ingestImageSource(image)
			continue
		}
		ref, err := ingestImageDataURI(image.Data)
		var images []docparser.StoredImage
		if err == nil {
			_, images, err = resolver.ResolveDataURIImages(ctx, "![image]("+ref+")", files, spaceID)
		}
		if err == nil && len(images) != 1 {
			err = fmt.Errorf("image could not be stored")
		}
		if err != nil {
			failures = append(failures, types.IngestImageFailure{Index: i, URL: ingestImageSource(image), Reason: err.Error()})
			continue
		}
		refs[i] = images[0].ServingURL
		stored = append(stored, images[0])
	}
	return refs, stored, failures
}

func ingestImageDataURI(raw string) (string, error) {
	payload := strings.TrimSpace(raw)
	if strings.HasPrefix(payload, "data:") {
		parts := strings.SplitN(payload, ",", 2)
		if len(parts) != 2 || !strings.HasSuffix(parts[0], ";base64") {
			return "", fmt.Errorf("image data must be base64")
		}
		payload = parts[1]
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return "", fmt.Errorf("decode image data: %w", err)
	}
	mimeType := http.DetectContentType(decoded)
	if !strings.HasPrefix(mimeType, "image/") {
		return "", fmt.Errorf("decoded data is not an image")
	}
	return "data:" + mimeType + ";base64," + base64.StdEncoding.EncodeToString(decoded), nil
}

func ingestCanonicalImageReference(image types.IngestImage) string {
	if image.Data == "" {
		return ingestImageSource(image)
	}
	payload := strings.TrimSpace(image.Data)
	if strings.HasPrefix(payload, "data:") {
		if parts := strings.SplitN(payload, ",", 2); len(parts) == 2 {
			payload = parts[1]
		}
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data = []byte(payload)
	}
	// The reference identifies image bytes while keeping base64 size from
	// dominating the existing body SimHash and its text length comparison.
	return fmt.Sprintf("data:sha256,%x", sha256.Sum256(data))
}

// assembleIngestBody fixes the order and Markdown structure before immutable
// article storage. Empty references skip failed images and their descriptions.
func assembleIngestBody(request *types.IngestRequest, imageRefs []string) string {
	parts := []string{strings.TrimSpace(utils.CleanMarkdown(request.Content))}
	field := func(name, value string) {
		if value != "" {
			parts = append(parts, name+"："+utils.CleanMarkdown(value))
		}
	}
	blockquote := func(content string) {
		lines := strings.Split(strings.ReplaceAll(utils.CleanMarkdown(content), "\r\n", "\n"), "\n")
		for i := range lines {
			lines[i] = "> " + lines[i]
		}
		parts = append(parts, strings.Join(lines, "\n"))
	}
	if len(request.Thread) > 0 {
		parts = append(parts, "## 推文串")
		for i, item := range request.Thread {
			parts = append(parts, fmt.Sprintf("### 推文 %d", i+1))
			field("作者", item.Author)
			field("时间", item.PublishedAt)
			parts = append(parts, utils.CleanMarkdown(item.Content))
		}
	}
	if len(request.Quotes) > 0 {
		parts = append(parts, "## 引用")
		for i, quote := range request.Quotes {
			parts = append(parts, fmt.Sprintf("### 引用 %d", i+1))
			field("作者", quote.Author)
			field("来源", quote.URL)
			blockquote(quote.Content)
		}
	}
	hasImages := false
	for i, ref := range imageRefs {
		if ref == "" {
			continue
		}
		if !hasImages {
			parts = append(parts, "## 图片")
			hasImages = true
		}
		image := request.Images[i]
		caption := image.Caption
		if caption == "" {
			caption = fmt.Sprintf("图片 %d", i+1)
		}
		label := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "\n", " ", "\r", " ").Replace(utils.CleanMarkdown(caption))
		parts = append(parts, fmt.Sprintf("### 图片 %d", i+1), fmt.Sprintf("![%s](<%s>)", label, ref))
		field("说明", image.Caption)
		if image.Text != "" {
			parts = append(parts, "图片文字：")
			blockquote(image.Text)
		}
	}
	return strings.Join(parts, "\n\n") + "\n"
}
