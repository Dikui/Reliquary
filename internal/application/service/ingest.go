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

var _ interfaces.IngestService = (*knowledgeService)(nil)

// Ingest stores deliberate collection with the same deduplication and asynchronous
// article processing used by other entry points. Source URLs are provenance only;
// fetching is limited to image URLs through the existing SSRF protected resolver.
func (s *knowledgeService) Ingest(ctx context.Context, request *types.IngestRequest) (*types.IngestResult, error) {
	if s.needsTaskTransaction() {
		return knowledgeTaskTransaction(ctx, s, func(copy *knowledgeService) (*types.IngestResult, error) {
			return copy.Ingest(ctx, request)
		})
	}

	if err := validateIngestRequest(request); err != nil {
		return nil, err
	}
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, werrors.NewUnauthorizedError("personal space is required")
	}
	if _, ok := types.PrincipalFromContext(ctx); !ok {
		return nil, werrors.NewUnauthorizedError("authenticated collector required")
	}
	if s.db == nil {
		return nil, werrors.NewInternalServerError("ingest database is unavailable")
	}
	kb, err := s.kbService.GetKnowledgeBase(ctx)
	if err != nil {
		return nil, err
	}
	if kb.TenantID != tenantID {
		return nil, werrors.NewForbiddenError("knowledge base belongs to another space")
	}
	now := time.Now()
	title := strings.TrimSpace(request.Title)
	if title == "" {
		title = strings.TrimSpace(request.URL)
	}
	// A source URL may exceed the title column while remaining valid provenance.
	if len([]rune(title)) > 255 {
		title = string([]rune(title)[:255])
	}
	annotationTags := request.AgentTags
	if annotationTags == nil {
		annotationTags = []string{}
	}
	tags, err := json.Marshal(annotationTags)
	if err != nil {
		return nil, err
	}
	article := &types.Knowledge{ID: uuid.NewString(), TenantID: tenantID, KnowledgeBaseID: kb.ID,
		Type: types.KnowledgeTypeManual, SourceKind: "article", Source: strings.TrimSpace(request.URL), Channel: types.ChannelAPI,
		Title: title, FileName: sanitizeManualDownloadFilename(title), FileType: types.KnowledgeTypeManual,
		ParseStatus: types.ParseStatusPending, EnableStatus: "disabled", CreatedAt: now, UpdatedAt: now,
		EmbeddingModelID: kb.EmbeddingModelID, CollectReason: strings.TrimSpace(request.CollectReason),
		AgentSummary: request.AgentSummary, AgentTags: types.JSON(tags), SourceName: request.SourceName}
	intake := &types.KnowledgeIntakeMetadata{Source: "ingest", Decision: "pending", SourceInfo: types.SourceInfo{
		OriginalURL: article.Source, Author: request.Author, Publisher: request.Publisher,
		PublishedAt: normalizeIntakePublicationDate(request.PublishedAt)}}
	if err := article.SetIntakeMetadata(intake); err != nil {
		return nil, err
	}
	// Stable source references make content fingerprints independent of generated
	// storage filenames. The worker receives only successfully stored references.
	canonicalRefs := make([]string, len(request.Images))
	for i, image := range request.Images {
		canonicalRefs[i] = ingestCanonicalImageReference(image)
	}
	canonicalBody := assembleIngestBody(request, canonicalRefs)
	setMessageFingerprint(article, canonicalBody)
	result := &types.IngestResult{ImageFailures: []types.IngestImageFailure{}}
	var duplicate *types.Knowledge
	var refresh bool
	// Avoid image downloads when the normalized URL or body is already collected.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMessageDeduplication(tx, article); err != nil {
			return err
		}
		var method string
		var distance int
		duplicate, method, distance, err = findMessageDuplicate(tx, article)
		if err != nil || duplicate == nil {
			return err
		}
		refresh, err = recordIngestDuplicate(ctx, tx, duplicate, article, method, distance)
		return err
	})
	if err != nil {
		return nil, err
	}
	if duplicate != nil {
		if refresh && s.vault != nil {
			s.vault.ScheduleRefresh(ctx, duplicate.ID)
		}
		result.ArticleID, result.Status, result.Message = duplicate.ID, "exists", "已存在"
		return result, nil
	}
	fileSvc := s.resolveFileService(ctx, kb)
	refs, stored, failures := s.resolveIngestImages(ctx, request.Images, fileSvc, tenantID)
	result.ImageFailures = failures
	body := assembleIngestBody(request, refs)
	// Failed images and their descriptions are absent from the immutable body,
	// so reserve the fingerprint of the successfully collected content as well.
	for i, ref := range refs {
		if ref == "" {
			canonicalRefs[i] = ""
		}
	}
	setMessageFingerprint(article, assembleIngestBody(request, canonicalRefs))
	cleanup := func() error {
		for _, image := range stored {
			if strings.HasPrefix(image.ServingURL, "http://") || strings.HasPrefix(image.ServingURL, "https://") {
				continue
			}
			if err := fileSvc.DeleteFile(ctx, image.ServingURL); err != nil {
				return err
			}
		}
		if intake.OriginalPath != "" {
			return fileSvc.DeleteFile(ctx, intake.OriginalPath)
		}
		return nil
	}
	intake.OriginalPath, err = fileSvc.SaveBytes(ctx, []byte(body), tenantID, article.ID+".md", false)
	if err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return nil, fmt.Errorf("save ingest body: %v; cleanup: %w", err, cleanupErr)
		}
		return nil, err
	}
	for _, image := range stored {
		intake.Images = append(intake.Images, image.ServingURL)
	}
	if err := article.SetIntakeMetadata(intake); err != nil {
		_ = cleanup()
		return nil, err
	}
	meta := types.NewManualKnowledgeMetadata("", types.ManualKnowledgeStatusPublish, 1)
	if err := article.SetManualMetadata(meta); err != nil {
		_ = cleanup()
		return nil, err
	}
	// Recheck under the same lock as creation. Concurrent deliveries cannot reserve
	// multiple articles, and the operation log commits with collection or reason fill.
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockMessageDeduplication(tx, article); err != nil {
			return err
		}
		var method string
		var distance int
		duplicate, method, distance, err = findMessageDuplicate(tx, article)
		if err != nil {
			return err
		}
		if duplicate != nil {
			refresh, err = recordIngestDuplicate(ctx, tx, duplicate, article, method, distance)
			return err
		}
		if err := tx.Create(article).Error; err != nil {
			return err
		}
		return NewOperationLogService(tx).RecordTx(ctx, tx, "ingest", article.ID, nil, map[string]any{
			"url": article.Source, "status": "pending", "collect_reason": article.CollectReason, "source_name": article.SourceName})
	})
	if err != nil || duplicate != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			return nil, cleanupErr
		}
		if err != nil {
			return nil, err
		}
		if refresh && s.vault != nil {
			s.vault.ScheduleRefresh(ctx, duplicate.ID)
		}
		result.ArticleID, result.Status, result.Message = duplicate.ID, "exists", "已存在"
		return result, nil
	}
	s.bindContentResources(ctx, tenantID, article.ID, body)
	rejected, err := s.reviewIntake(ctx, kb, article, body)
	var deferred *types.ChatGPTDeferredError
	var processOptions []taskqueue.Option
	if err != nil {
		s.markArticleProcessingFailed(ctx, article, err)
		if !errors.As(err, &deferred) {
			s.failIntake(ctx, article)
			return nil, err
		}
		// Collection has committed. Keep its unreviewed original and let the
		// worker repeat the same review after the generation pause.
		processOptions = append(processOptions, taskqueue.ProcessAt(deferred.RetryAt))
	}
	if rejected {
		intake, _ = article.IntakeMetadata()
		result.ArticleID, result.Status, result.Message, result.Reason = article.ID, "pending_review", "等待收录审核", intake.Reason
		return result, nil
	}
	if _, err := s.enqueueManualProcessing(ctx, article, body, false, processOptions...); err != nil {
		s.markKnowledgeEnqueueFailed(ctx, article)
		result.ArticleID, result.Status, result.Message = article.ID, types.ParseStatusFailed, "处理入队失败"
		return result, nil
	}
	result.ArticleID, result.Status, result.Message = article.ID, types.ParseStatusPending, "已收录，正在处理"
	if deferred != nil {
		result.Message = "已收录，等待模型恢复后处理"
	}
	return result, nil
}

func recordIngestDuplicate(ctx context.Context, tx *gorm.DB, existing, incoming *types.Knowledge, method string, distance int) (bool, error) {
	if err := recordMessageRepost(tx, existing, incoming, method, distance); err != nil {
		return false, err
	}
	// The deduplication lock serializes collection; the row lock also protects
	// concurrent user edits and processing updates to this article.
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id=? AND id=?", existing.TenantID, existing.ID).First(existing).Error; err != nil {
		return false, err
	}
	before := existing.CollectReason
	filled := strings.TrimSpace(before) == "" && incoming.CollectReason != ""
	if filled {
		if err := tx.Model(existing).Updates(map[string]any{"collect_reason": incoming.CollectReason, "updated_at": time.Now()}).Error; err != nil {
			return false, err
		}
	}
	err := NewOperationLogService(tx).RecordTx(ctx, tx, "ingest", existing.ID,
		map[string]any{"collect_reason": before}, map[string]any{"url": incoming.Source, "status": "exists", "collect_reason": existing.CollectReason, "source_name": incoming.SourceName})
	return filled, err
}

func validateIngestRequest(request *types.IngestRequest) error {
	if request == nil || strings.TrimSpace(request.URL) == "" || strings.TrimSpace(utils.CleanMarkdown(request.Content)) == "" {
		return werrors.NewBadRequestError("url and content are required")
	}
	if !validIngestURL(strings.TrimSpace(request.URL)) || len(request.URL) > 2048 {
		return werrors.NewBadRequestError("url must be an absolute HTTP or HTTPS URL of at most 2048 bytes")
	}
	if len([]rune(request.Content)) > manualContentMaxLength {
		return werrors.NewBadRequestError("content exceeds 200000 characters")
	}
	if len([]rune(strings.TrimSpace(request.Title))) > 255 {
		return werrors.NewBadRequestError("title exceeds 255 characters")
	}
	if len(request.Images) > 100 {
		return werrors.NewBadRequestError("images supports at most 100 items")
	}
	for i, image := range request.Images {
		if (strings.TrimSpace(image.URL) == "") == (strings.TrimSpace(image.Data) == "") {
			return werrors.NewBadRequestError(fmt.Sprintf("images[%d] requires exactly one of url or data", i))
		}
		if image.URL != "" && !validIngestURL(image.URL) {
			return werrors.NewBadRequestError(fmt.Sprintf("images[%d].url must be an absolute HTTP or HTTPS URL", i))
		}
	}
	for i, item := range request.Thread {
		if strings.TrimSpace(item.Content) == "" {
			return werrors.NewBadRequestError(fmt.Sprintf("thread[%d].content is required", i))
		}
	}
	for i, quote := range request.Quotes {
		if strings.TrimSpace(quote.Content) == "" {
			return werrors.NewBadRequestError(fmt.Sprintf("quotes[%d].content is required", i))
		}
		if quote.URL != "" && !validIngestURL(quote.URL) {
			return werrors.NewBadRequestError(fmt.Sprintf("quotes[%d].url must be an absolute HTTP or HTTPS URL", i))
		}
	}
	return nil
}

func validIngestURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Hostname() != "" && u.User == nil && (u.Scheme == "http" || u.Scheme == "https") && !strings.ContainsAny(raw, "\r\n\t")
}

func (s *knowledgeService) resolveIngestImages(ctx context.Context, input []types.IngestImage, files interfaces.FileService, tenantID uint64) ([]string, []docparser.StoredImage, []types.IngestImageFailure) {
	refs := make([]string, len(input))
	stored := []docparser.StoredImage{}
	failures := []types.IngestImageFailure{}
	resolver := s.imageResolver
	if resolver == nil {
		resolver = docparser.NewImageResolver()
	}
	for i, image := range input {
		ref := image.URL
		var images []docparser.StoredImage
		var err error
		if image.Data != "" {
			ref, err = ingestImageDataURI(image.Data)
			if err == nil {
				_, images, err = resolver.ResolveDataURIImages(ctx, "![image]("+ref+")", files, tenantID)
			}
		} else {
			// URL.String escapes Markdown delimiters without changing the target.
			u, _ := url.Parse(ref)
			_, images, err = resolver.ResolveRemoteImages(ctx, "![image](<"+u.String()+">)", files, tenantID)
		}
		if err == nil && len(images) != 1 {
			err = fmt.Errorf("image could not be stored")
		}
		if err != nil {
			failures = append(failures, types.IngestImageFailure{Index: i, URL: image.URL, Reason: err.Error()})
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
		return image.URL
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
