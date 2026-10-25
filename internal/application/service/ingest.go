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
	"reliquary/internal/diagnostics"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
	"strings"
	"time"
	"unicode/utf8"

	werrors "reliquary/internal/errors"

	"github.com/google/uuid"
)

var _ interfaces.IngestService = (*entryService)(nil)

// Ingest never retrieves source material from a URL. Supplied bytes are parsed
// locally and saved before background indexing and enrichment.
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
	if err := s.validateFactMentions(ctx, library, request); err != nil {
		return nil, err
	}
	if err := validateLibraryIngest(request, library); err != nil {
		return nil, err
	}
	if s.vault == nil {
		return nil, werrors.NewServiceUnavailableError("article storage is unavailable")
	}
	if s.needsTaskTransaction() {
		changes := &ingestFileChanges{}
		ctx = context.WithValue(ctx, ingestFileChangesKey{}, changes)
		result, err = entryTaskTransaction(ctx, s, func(copy *entryService) (*types.IngestResult, error) {
			result, err := copy.Ingest(ctx, request)
			if err == nil && result != nil && (result.Status == "exists" || result.Status == "discarded") {
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
	// Serialize explicit mention writes with manual fact merges before taking any entry row locks.
	if request.MentionedFactIDs != nil {
		if err := lockEntitySpace(s.db.WithContext(ctx), spaceID); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	kind := "article"
	if library.Template == types.TemplateOwn {
		kind = "mine"
	} else if library.Template == types.TemplateGenerated {
		kind = "report"
	}
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = strings.TrimSpace(request.URL)
	}
	if title == "" {
		title = "未命名资料"
	}
	if len([]rune(title)) > 255 {
		title = string([]rune(title)[:255])
	}
	agentTags := request.AgentTags
	if agentTags == nil {
		agentTags = []string{}
	}
	tags, _ := json.Marshal(agentTags)
	entry := &types.Entry{
		ID: uuid.NewString(), SpaceID: spaceID, LibraryID: library.ID,
		SourceID: request.SourceID, SourceRevision: request.SourceRevision,
		SourceRef: request.SourceRef, Identifier: request.Identifier,
		Type: types.EntryTypeManual, SourceKind: kind, Source: strings.TrimSpace(request.URL), Channel: types.ChannelAPI,
		Title: title, FileName: sanitizeManualDownloadFilename(title), FileType: types.EntryTypeManual,
		ParseStatus: types.ParseStatusPending, SummaryStatus: types.SummaryStatusPending, EnableStatus: "disabled",
		CreatedAt: now, UpdatedAt: now, EmbeddingModelID: library.EmbeddingModelID,
		CollectReason: strings.TrimSpace(request.CollectReason), AgentSummary: request.AgentSummary, AgentTags: types.JSON(tags),
		SourceName: request.SourceName, Acquisition: request.Acquisition,
		IngestAPIKeyID: keyID, IngestAPIKeyName: keyName, LastIngestAPIKeyID: keyID, LastIngestAPIKeyName: keyName,
	}
	intake := &types.EntryIntakeMetadata{Source: "ingest", Decision: "approved", SourceInfo: types.SourceInfo{
		OriginalURL: entry.Source, Author: request.Author, Publisher: request.Publisher,
		PublishedAt: normalizeIntakePublicationDate(request.PublishedAt),
	}}
	if err := entry.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	setMessageFingerprint(entry, "")
	if err := lockMessageDeduplication(s.db.WithContext(ctx), entry); err != nil {
		return nil, err
	}
	existing, err := s.findExactIngestSource(ctx, entry)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		err = s.withTaskObject(ctx, existing.ID, func(held context.Context) error {
			result, err = s.saveIngestEntry(held, library, entry, existing, request)
			return err
		})
		return result, err
	}
	return s.saveIngestEntry(ctx, library, entry, nil, request)
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
			if errors.As(err, &app) && (app.HTTPCode == http.StatusBadRequest || app.HTTPCode == http.StatusConflict) {
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

func ingestResult(article *types.Entry, status, message, reason, id string) *types.IngestResult {
	result := &types.IngestResult{EntryID: id, Status: status, Message: message, Reason: reason, ImageFailures: []types.IngestImageFailure{}}
	if article != nil {
		result.LibraryName = article.LibraryName
		result.EntryID = article.ID
		result.SourceUnverified = article.SourceUnverified
		result.LibraryID, result.SourceRef, result.Identifier = article.LibraryID, article.SourceRef, article.Identifier
		result.SourceID, result.SourceRevision = article.SourceID, article.SourceRevision
		result.HasUpdate, result.LatestEntryID = article.HasUpdate, article.LatestEntryID
		if article.SourceRef != "" && result.LatestEntryID == "" {
			result.LatestEntryID = article.ID
		}
	}
	return result
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
	textContent := searchutil.MarkdownImageRegex.ReplaceAllString(utils.CleanMarkdown(request.Content), "")
	textContent = searchutil.HTMLImageSrcRegex.ReplaceAllString(textContent, "")
	if strings.TrimSpace(textContent) == "" {
		return bad("readable Markdown text is required; image-only content is not accepted")
	}
	if request.Acquisition != "" && request.Acquisition != "verbatim" && request.Acquisition != "extracted" && request.Acquisition != "model_written" {
		return bad("acquisition must be verbatim, extracted or model_written")
	}
	if request.URL != "" && (!validIngestURL(strings.TrimSpace(request.URL)) || len(request.URL) > 2048) {
		return bad("url must be an absolute HTTP or HTTPS URL of at most 2048 bytes")
	}
	if request.PublishedAt != "" && normalizeIntakePublicationDate(request.PublishedAt) == "" {
		return bad("published_at must be a valid date in YYYY-MM-DD or RFC3339 format")
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
