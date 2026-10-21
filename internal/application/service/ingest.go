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
	"unicode/utf8"

	"reliquary/internal/diagnostics"
	werrors "reliquary/internal/errors"
	"reliquary/internal/infrastructure/docparser"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var _ interfaces.IngestService = (*entryService)(nil)

// Ingest never retrieves source material from a URL. Supplied bytes are parsed
// locally, then reviewed and processed by the existing article pipeline.
func (s *entryService) Ingest(ctx context.Context, request *types.IngestRequest) (result *types.IngestResult, err error) {
	if request != nil {
		copy := *request
		request = &copy
	}
	if err := validateIngestRequest(request); err != nil {
		return nil, err
	}
	ctx, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	spaceID, _ := types.SpaceIDFromContext(ctx)
	diagnostics.SetStage(ctx, "library")
	library, err := s.resolveIngestLibrary(ctx, request.Library)
	if err != nil {
		return nil, err
	}
	diagnostics.SetLibrary(ctx, library)
	defer func() {
		if err == nil && result != nil && result.LibraryID == library.ID {
			result.LibraryName = library.Name
		}
		if err == nil && result != nil && result.EntryID != "" && request.RecoveryFailureID != "" && !s.needsTaskTransaction() {
			err = s.db.WithContext(ctx).Model(&types.Entry{}).Where("id=? AND space_id=?", result.EntryID, spaceID).Update("metadata", gorm.Expr("jsonb_set(COALESCE(metadata,'{}'::jsonb),'{recovery_failure_id}',to_jsonb(?::text),true)", request.RecoveryFailureID)).Error
		}
	}()
	if err := validateLibraryIngest(request, library); err != nil {
		return nil, err
	}
	if !intakeTemplateRules(library).Process {
		diagnostics.SetOutcome(ctx, "unknown", "")
		if s.vault == nil {
			return nil, werrors.NewBadRequestError("report vault is unavailable")
		}
		reports, err := NewVaultDocumentService(s.db, s.modelService, s.retrieveEngine, s.vault.root)
		if err != nil {
			return nil, err
		}
		report, err := reports.createReport(ctx, library, request.Title, request.Content, nil, request.Graph)
		if err != nil {
			return nil, err
		}
		status := "accepted"
		if report.Existing {
			status = "exists"
		}
		return ingestResult(report.Entry, status, report.Message, "", ""), nil
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
			if result != nil {
				diagnostics.SetOutcome(ctx, "unknown", result.EntryID)
			}
			err = errors.Join(err, changes.finish())
		}
		return result, err
	}
	kind := "article"
	if intakeTemplateRules(library).Identity.SourceRef {
		kind = "mine"
	}
	now := time.Now()
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = strings.TrimSpace(request.URL)
	}
	if title == "" {
		title = "待审文章"
		if intakeTemplateRules(library).Identity.SourceRef {
			title = "本人文档"
		}
	}
	if len([]rune(title)) > 255 {
		title = string([]rune(title)[:255])
	}
	tags := request.AgentTags
	if tags == nil {
		tags = []string{}
	}
	tagJSON, _ := json.Marshal(tags)
	article := &types.Entry{ID: uuid.NewString(), SpaceID: spaceID, LibraryID: library.ID,
		SourceRef: request.SourceRef, Identifier: request.Identifier, Graph: request.Graph,
		Type: types.EntryTypeManual, SourceKind: kind, Source: strings.TrimSpace(request.URL), Channel: types.ChannelAPI,
		Title: title, FileName: sanitizeManualDownloadFilename(title), FileType: types.EntryTypeManual,
		ParseStatus: types.ParseStatusPending, EnableStatus: "disabled", CreatedAt: now, UpdatedAt: now,
		EmbeddingModelID: library.EmbeddingModelID, CollectReason: strings.TrimSpace(request.CollectReason),
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
	// Concurrent deliveries therefore commit one mutation.
	if err := lockMessageDeduplication(s.db.WithContext(ctx), article); err != nil {
		return nil, err
	}
	article.CreatedAt = time.Now()
	if discarded, err := s.lookupIngestDiscard(ctx, library.ID, entryDiscardKeys(article, intakeTemplateRules(library))); err != nil {
		return nil, err
	} else if discarded != nil {
		return ingestResult(nil, "discarded", "该链接已被丢弃，未再次收录。", discarded.Reason, discarded.EntryID), nil
	}
	if existing, err := s.findIngestURL(ctx, article.NormalizedURL, true, library); err != nil {
		return nil, err
	} else if existing != nil {
		if _, err := recordIngestDuplicate(ctx, s.db, existing, article, "url", 0); err != nil {
			return nil, err
		}
		if s.vault != nil {
			s.vault.ScheduleRefresh(ctx, existing.ID)
		}
		return ingestResult(existing, "exists", "该文章已收录。", "", ""), nil
	}
	diagnostics.SetStage(ctx, "materials")
	body, canonicalBody, err := s.storeIngestMaterials(ctx, library, article, request)
	if err != nil {
		return nil, err
	}
	if intakeTemplateRules(library).Identity.SourceRef && request.SourceRef != "" {
		// The transaction holds the library lock, including when no prior row exists.
		article.SourceBodyHash = sourceBodyHash(canonicalBody)
		latest, err := types.FindLatestSourceEntry(s.db.WithContext(ctx), spaceID, library.ID, request.SourceRef)
		if err != nil {
			return nil, err
		}
		if latest != nil && latest.SourceBodyHash == article.SourceBodyHash {
			if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", latest.ID, nil, map[string]any{"status": "exists", "source_ref": request.SourceRef, "library_id": library.ID}); err != nil {
				return nil, err
			}
			return ingestResult(latest, "exists", "相同来源与正文已收录。", "", ""), nil
		}
	}
	intake, _ = article.IntakeMetadata()
	if err := extractIntakeOriginalLink(article, body, nil); err != nil {
		return nil, err
	}
	intake, _ = article.IntakeMetadata()
	setMessageFingerprint(article, canonicalBody)
	// Source evidence extracted from a supplied file also participates in lookup.
	if discarded, err := s.lookupIngestDiscard(ctx, library.ID, entryDiscardKeys(article, intakeTemplateRules(library))); err != nil {
		return nil, err
	} else if discarded != nil {
		return ingestResult(nil, "discarded", "该链接已被丢弃，未再次收录。", discarded.Reason, discarded.EntryID), nil
	}
	var duplicate *types.Entry
	var method string
	var distance int
	duplicate, method, distance, err = findMessageDuplicate(s.db.WithContext(ctx), article)
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
	if err := s.db.WithContext(ctx).Create(article).Error; err != nil {
		return nil, err
	}
	if err := s.referenceIngestMaterials(ctx, article, request); err != nil {
		return nil, err
	}
	if err := NewOperationLogService(s.db).RecordTx(ctx, s.db, "ingest", article.ID, nil, map[string]any{
		"library_id": library.ID, "template": library.Template, "url": article.Source, "acquisition": article.Acquisition, "collect_reason": article.CollectReason}); err != nil {
		return nil, err
	}
	s.bindContentResources(ctx, spaceID, article.ID, body)
	return s.finishIngestReview(ctx, library, article, body)
}

func (s *entryService) IngestBatch(ctx context.Context, request *types.IngestBatchRequest) (*types.IngestBatchResult, error) {
	if request == nil || len(request.Items) < 1 || len(request.Items) > 50 {
		return nil, werrors.NewBadRequestError("items must contain between 1 and 50 entries")
	}
	verified, keyID, keyName, err := s.ingestIdentity(ctx)
	if err != nil {
		return nil, err
	}
	spaceID, _ := types.SpaceIDFromContext(verified)
	principal, _ := types.PrincipalFromContext(verified)
	ctx = context.WithValue(verified, verifiedIngestIdentityKey{}, verifiedIngestIdentity{spaceID, principal, keyID, keyName})
	var total int64
	for _, item := range request.Items {
		total += ingestInlineSize(item.Images)
	}
	if total > types.MaxIngestInlineRequestBytes {
		return nil, werrors.NewBadRequestError("inline images exceed the 20 MiB request limit")
	}
	result := &types.IngestBatchResult{Items: make([]types.IngestResult, 0, len(request.Items))}
	for i := range request.Items {
		item, err := s.ingestWithDiagnostics(ctx, &request.Items[i], i)
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
				message = "该条目无效，请检查获取方式、正文和图片后重试。"
			}
			diagnostic := item
			item = ingestResult(nil, "invalid", message, reason, "")
			if diagnostic != nil {
				item.Diagnostics = diagnostic.Diagnostics
			}
		}
		item.Index = i
		result.Items = append(result.Items, *item)
	}
	return result, nil
}

func (s *entryService) finishIngestReview(ctx context.Context, library *types.Library, article *types.Entry, body string) (*types.IngestResult, error) {
	diagnostics.SetStage(ctx, "review")
	rejected, err := s.reviewIntake(context.WithValue(ctx, deferSegmentReviewKey{}, true), library, article, body)
	if errors.Is(err, errSegmentReviewQueued) {
		diagnostics.SetStage(ctx, "enqueue")
		if _, err := s.enqueueManualProcessing(ctx, article, body, false); err != nil {
			return nil, err
		}
		return ingestResult(article, "accepted", "已接收，正在分段审核。", "", ""), nil
	}
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
	diagnostics.SetStage(ctx, "enqueue")
	if _, err := s.enqueueManualProcessing(ctx, article, body, false, options...); err != nil {
		return nil, err
	}
	return ingestResult(article, "accepted", "已收录，正在处理。", "", ""), nil
}

func ingestResult(article *types.Entry, status, message, reason, id string) *types.IngestResult {
	result := &types.IngestResult{EntryID: id, Status: status, Message: message, Reason: reason, ImageFailures: []types.IngestImageFailure{}}
	if article != nil {
		result.LibraryName = article.LibraryName
		result.EntryID = article.ID
		result.SourceUnverified = article.SourceUnverified
		result.LibraryID, result.SourceRef, result.Identifier, result.Graph = article.LibraryID, article.SourceRef, article.Identifier, article.Graph
		result.HasUpdate, result.LatestEntryID = article.HasUpdate, article.LatestEntryID
		if article.SourceRef != "" && result.LatestEntryID == "" {
			result.LatestEntryID = article.ID
		}
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
	if strings.TrimSpace(request.Library) == "" {
		return bad("library is required; call list_libraries for available libraries")
	}
	if utf8.RuneCountInString(request.Content) > types.MaxIngestContentRunes {
		return bad("content exceeds 5000000 Unicode code points")
	}
	if strings.TrimSpace(utils.CleanMarkdown(request.Content)) == "" || validIngestURL(strings.TrimSpace(request.Content)) {
		return bad("content is required; convert the source to Markdown before ingestion")
	}
	if request.Acquisition != "verbatim" && request.Acquisition != "extracted" && request.Acquisition != "model_written" {
		return bad("acquisition must be verbatim, extracted or model_written")
	}
	if request.URL != "" && (!validIngestURL(strings.TrimSpace(request.URL)) || len(request.URL) > 2048) {
		return bad("url must be an absolute HTTP or HTTPS URL of at most 2048 bytes")
	}
	bodyRunes := utf8.RuneCountInString(request.Content)
	for _, item := range request.Thread {
		bodyRunes += utf8.RuneCountInString(item.Content)
	}
	for _, item := range request.Quotes {
		bodyRunes += utf8.RuneCountInString(item.Content)
	}
	if bodyRunes > types.MaxIngestContentRunes {
		return bad("content including thread and quotes exceeds 5000000 Unicode code points")
	}
	if len([]rune(strings.TrimSpace(request.Title))) > 255 {
		return bad("title exceeds 255 characters")
	}
	if len([]rune(request.ContentTypeHint)) > 100 {
		return bad("invalid content_type_hint")
	}
	if len(request.Images) > 100 {
		return bad("images supports at most 100 items")
	}
	for i, image := range request.Images {
		if image.AssetID != "" && image.Data != "" {
			return bad(fmt.Sprintf("images[%d] requires one of asset_id or data", i))
		}
		if image.AssetID == "" && image.Data == "" {
			source := ingestImageSource(image)
			if source == "" || !validIngestURL(source) {
				return bad(fmt.Sprintf("images[%d] requires image data, an asset_id or an HTTP/HTTPS source_url", i))
			}
		}
		if image.Data != "" {
			if _, err := decodeIngestInline(image.Data); err != nil {
				return bad(err.Error())
			}
			if _, err := ingestImageDataURI(image.Data); err != nil {
				return bad(err.Error())
			}
		}
		if image.SourceURL != "" && !validIngestURL(image.SourceURL) {
			return bad(fmt.Sprintf("images[%d].source_url is invalid", i))
		}
	}
	for i, ref := range bodyImageReferences(request.Content) {
		if strings.HasPrefix(ref.URL, "data:image/") {
			if _, err := decodeIngestInline(ref.URL); err != nil {
				return bad(fmt.Sprintf("正文图片 %d：%s", i+1, err))
			}
			if _, err := ingestImageDataURI(ref.URL); err != nil {
				return bad(fmt.Sprintf("正文图片 %d：%s", i+1, err))
			}
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
	for i, image := range input {
		if image.Data == "" {
			refs[i] = ingestImageSource(image)
			continue
		}
		ref, err := ingestImageDataURI(image.Data)
		if err == nil {
			_, payload, _ := strings.Cut(ref, ",")
			data, _ := base64.StdEncoding.DecodeString(payload)
			mimeType := http.DetectContentType(data)
			source := ingestImageSource(image)
			if image.AssetID != "" {
				source = "asset_id:" + image.AssetID
			}
			diagnostics.CaptureImage(ctx, fmt.Sprintf("image-%d", i), source, mimeType, data)
			// Inputs are already validated; preserve upload limits and original bytes.
			name := uuid.NewString() + vaultImageExtension("", mimeType)
			refs[i], err = files.SaveBytes(ctx, data, spaceID, name, false)
			if err == nil {
				stored = append(stored, docparser.StoredImage{OriginalRef: ref, ServingURL: refs[i], MimeType: mimeType})
			}
		}
		if err != nil {
			failures = append(failures, types.IngestImageFailure{Index: i, URL: ingestImageSource(image), Reason: err.Error()})
		}
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
	if err := utils.ValidateIngestImage(decoded); err != nil {
		return "", err
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
