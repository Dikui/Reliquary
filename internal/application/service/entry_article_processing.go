package service

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/chunker"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/chat"
	"reliquary/internal/searchutil"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"slices"
	"sort"
	"strings"
	"time"
)

func articleProcessingPrompt(libraryIDs ...string) string {
	return config.Prompt("article_processing", libraryIDs...)
}

const (
	defaultArticleBodyChars        = 24000
	defaultArticleImages           = 8
	defaultArticleCompletionTokens = 4096
)

// articleProcessingResult keeps required fields nullable while decoding so an
// omitted field is distinguishable from an intentionally empty array.
type articleProcessingResult struct {
	Fact             *understandingFact               `json:"fact,omitempty"`
	Title            string                           `json:"title,omitempty"`
	Images           []types.ProcessingImage          `json:"images_result,omitempty"`
	Summary          *types.EntrySummary              `json:"summary"`
	DomainIDs        []string                         `json:"domain_ids"`
	EntityIDs        []string                         `json:"entity_ids"`
	EntityMentions   []understandingEntityMention     `json:"entity_mentions"`
	Entities         []articleProcessingEntity        `json:"entities"`
	Provenance       *articleProcessingProvenance     `json:"provenance,omitempty"`
	Extensions       map[string][]SegmentContribution `json:"extensions,omitempty"`
	RunID            string                           `json:"processing_run_id,omitempty"`
	ResolvedEntities map[string]string                `json:"resolved_entities,omitempty"`
}

type articleProcessingInput struct {
	LibraryName   string                      `json:"library_name"`
	FactAllowed   bool                        `json:"fact_allowed"`
	DomainOptions []understandingDomainOption `json:"domain_options"`
	EntityOptions []understandingEntityOption `json:"entity_options"`
	Images        []processingImageInput      `json:"images"`
	Author        string                      `json:"author"`
	Segment       any                         `json:"segment,omitempty"`
	Title         string                      `json:"title"`
	Publisher     string                      `json:"publisher"`
	PublishedAt   string                      `json:"published_at"`
	Body          string                      `json:"body"`
	BodyTruncated bool                        `json:"body_truncated"`
}

func articleTitleForProcessing(k *types.Entry) string {
	title := strings.TrimSpace(k.Title)
	source := []rune(strings.TrimSpace(k.Source))
	if title == "待审文章" || title == "本人文档" || title == "未命名资料" || title == string(source[:min(255, len(source))]) {
		return ""
	}
	return title
}

func parseArticleProcessingResult(raw string) (*articleProcessingResult, error) {
	var fields map[string]json.RawMessage
	if err := utils.ParseLLMJsonResponse(raw, &fields); err != nil {
		return nil, fmt.Errorf("entry understanding output is not valid JSON: %w", err)
	}
	domainIDs, entityIDs := understandingIDs(fields["domain_ids"]), understandingIDs(fields["entity_ids"])
	mentions := understandingEntityMentions(fields["entity_mentions"])
	fact := parseUnderstandingFact(fields["fact"])
	delete(fields, "fact")
	delete(fields, "domain_ids")
	delete(fields, "entity_ids")
	delete(fields, "entity_mentions")
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	var result articleProcessingResult
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil, fmt.Errorf("article processing output is not valid JSON: %w", err)
	}
	result.Title = strings.TrimSpace(result.Title)
	result.DomainIDs, result.EntityIDs, result.Fact = domainIDs, entityIDs, fact
	result.EntityMentions = mentions
	if len([]rune(result.Title)) > 255 || strings.ContainsAny(result.Title, "\r\n\x00") {
		return nil, errors.New("article processing title must be a single line of at most 255 characters without NUL")
	}
	if result.Summary == nil ||
		strings.TrimSpace(result.Summary.Overview) == "" {
		return nil, errors.New("article processing output is missing required fields")
	}
	result.Summary.Overview = strings.TrimSpace(result.Summary.Overview)
	return &result, nil
}

// Invalid items are discarded independently, including values of the wrong JSON type.
func understandingIDs(raw json.RawMessage) []string {
	var values []json.RawMessage
	_ = json.Unmarshal(raw, &values)
	ids := []string{}
	seen := map[string]bool{}
	for _, value := range values {
		var id string
		if json.Unmarshal(value, &id) == nil && id != "" && !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}
	return ids
}

func qualifyUnderstandingChoices(result *articleProcessingResult, input articleProcessingInput) {
	domains := map[string]bool{}
	for _, option := range input.DomainOptions {
		domains[option.ID] = true
	}
	result.DomainIDs = acceptedUnderstandingIDs(result.DomainIDs, domains)
	result.EntityIDs = []string{}
	for i := range result.EntityMentions {
		mention := &result.EntityMentions[i]
		mention.EntityID = ""
		if id := resolveFactEntity(mention.Name, "", input.EntityOptions); id != nil {
			mention.EntityID = *id
			if !slices.Contains(result.EntityIDs, *id) {
				result.EntityIDs = append(result.EntityIDs, *id)
			}
		}
	}
	qualifyFactEntityIDs(result.Fact, input.EntityOptions)
}

func acceptedUnderstandingIDs(ids []string, options map[string]bool) []string {
	accepted := []string{}
	for _, id := range ids {
		if options[id] {
			accepted = append(accepted, id)
			delete(options, id)
			if len(accepted) == 3 {
				break
			}
		}
	}
	return accepted
}

const modelOutputCorrectionReserve = 512

func outputCorrectionBudget(libraryIDs ...string) int {
	return len([]rune(strings.ReplaceAll(config.Prompt("output_correction", libraryIDs...), "{{validation_error}}", strings.Repeat("x", modelOutputCorrectionReserve)))) + 1
}

// Only invalid output gets one corrective call; transport failures use the task retry policy.
func chatWithOutputValidation(ctx context.Context, model chat.Chat, messages []chat.Message, options *chat.ChatOptions, validate func(string) error, libraryIDs ...string) error {
	var validationErr error
	for attempt := 0; attempt < 2; attempt++ {
		request := messages
		if attempt > 0 {
			request = append([]chat.Message(nil), messages...)
			feedback := []rune(validationErr.Error())
			request[0].Content += "\n" + strings.ReplaceAll(config.Prompt("output_correction", libraryIDs...), "{{validation_error}}", string(feedback[:min(len(feedback), modelOutputCorrectionReserve)]))
		}
		response, err := model.Chat(ctx, request, options)
		if err != nil {
			return err
		}
		if response == nil {
			return errors.New("model returned an empty response")
		}
		validationErr = validate(response.Content)
		if validationErr == nil {
			return nil
		}
	}
	return fmt.Errorf("model output remains invalid after correction: %w", validationErr)
}

func (s *entryService) articleProcessingLimits(model *types.Model) (bodyChars, images, completion int) {
	bodyChars, images, completion = defaultArticleBodyChars, defaultArticleImages, defaultArticleCompletionTokens
	if model != nil && model.Parameters.ContextWindow > 0 {
		// One rune per token is a conservative estimate for mixed Chinese and
		// English. Keep 40 percent for instructions, images and output.
		_, _, reserve := s.segmentSettings()
		bodyChars = max(1, model.Parameters.ContextWindow*(100-reserve)/100)
		completion = min(completion, max(1, model.Parameters.ContextWindow/4))
	}
	if s.config != nil && s.config.ArticleProcessing != nil {
		cfg := s.config.ArticleProcessing
		if cfg.MaxBodyChars > 0 {
			bodyChars = min(bodyChars, cfg.MaxBodyChars)
		}
		if cfg.MaxImages > 0 {
			images = cfg.MaxImages
		}
		if cfg.MaxCompletionTokens > 0 {
			completion = cfg.MaxCompletionTokens
		}
	}
	if model != nil && model.Parameters.MaxOutputTokens > 0 {
		completion = min(completion, model.Parameters.MaxOutputTokens)
	}
	return
}

// articleImagesInOrder uses body positions rather than resolver batch order;
// data URI and remote images are resolved in separate passes.
func articleImagesInOrder(body string, stored []docparser.StoredImage) []string {
	type reference struct {
		position int
		url      string
	}
	refs := []reference{}
	for _, ref := range docparser.MarkdownImageReferences(body) {
		refs = append(refs, reference{ref.Position, ref.URL})
	}
	for _, match := range searchutil.HTMLImageSrcRegex.FindAllStringSubmatchIndex(body, -1) {
		group := searchutil.HTMLImageSrcURLGroup * 2
		if len(match) > group+1 && match[group] >= 0 {
			refs = append(refs, reference{match[0], strings.TrimSpace(body[match[group]:match[group+1]])})
		}
	}
	sort.SliceStable(refs, func(i, j int) bool { return refs[i].position < refs[j].position })
	urls := make([]string, 0, len(refs))
	for _, ref := range refs {
		urls = append(urls, ref.url)
	}
	if len(urls) == 0 {
		for _, image := range stored {
			if image.ServingURL != "" {
				urls = append(urls, image.ServingURL)
			}
		}
	}
	return urls
}

func (s *entryService) processingImageReader(ctx context.Context, library *types.Library, k *types.Entry, ref string) (io.ReadCloser, error) {
	if strings.HasPrefix(ref, "data:image/") {
		header, payload, ok := strings.Cut(ref, ",")
		if !ok || !strings.HasSuffix(header, ";base64") {
			return nil, errors.New("图片 data URI 格式不正确")
		}
		reader := bufio.NewReader(base64.NewDecoder(base64.StdEncoding, strings.NewReader(payload)))
		prefix, _ := reader.Peek(512)
		if !strings.HasPrefix(http.DetectContentType(prefix), "image/") {
			return nil, errors.New("图片 data URI 内容不可读")
		}
		return io.NopCloser(reader), nil
	}
	if remoteIngestImage(ref) {
		return nil, errors.New("未提供图片文件")
	}
	if s.vault != nil && k.VaultPath != "" && !strings.Contains(ref, "://") {
		return s.vault.ReadImage(k, ref)
	}
	return s.resolveFileServiceForPath(ctx, library, ref).GetFile(ctx, ref)
}

func (s *entryService) rejectProcessingGIFs(ctx context.Context, library *types.Library, k *types.Entry, body string) error {
	return rejectGIFImages(body, func(ref string) (io.ReadCloser, error) {
		return s.processingImageReader(ctx, library, k, ref)
	})
}

func (s *entryService) processingImageDataURI(ctx context.Context, library *types.Library, k *types.Entry, ref string) (string, error) {
	reader, err := s.processingImageReader(ctx, library, k, ref)
	if err != nil {
		return "", err
	}
	defer reader.Close()
	prepared, err := utils.PrepareModelImage(ctx, reader)
	if err != nil {
		label := ref
		if strings.HasPrefix(ref, "data:") {
			label = "内联图片"
		}
		return "", fmt.Errorf("prepare processing image %s: %w", label, err)
	}
	return "data:" + http.DetectContentType(prepared.Data) + ";base64," + base64.StdEncoding.EncodeToString(prepared.Data), nil
}

func (s *entryService) callArticleProcessingPart(ctx context.Context, library *types.Library, k *types.Entry, body string, stored []docparser.StoredImage, segment any, imageRefs ...[]docparser.MarkdownImageReference) (*articleProcessingResult, error) {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	ctx = types.WithLLMCallMetadata(ctx, "entry_understanding", "")
	input, prompt, model, err := s.prepareArticleProcessing(ctx, library, k)
	if err != nil {
		return nil, err
	}
	modelChat, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierGeneration)
	if err != nil {
		return nil, err
	}
	input.Segment = segment
	bodyLimit, _, completion := s.articleProcessingLimits(model)
	images, imageInputs, imageRecords, err := s.attachProcessingImages(ctx, library, k, body, model.Parameters.SupportsVision, imageRefs...)
	if err != nil {
		return nil, err
	}
	input.Images = imageInputs
	imageReferences := []types.ModelCallImageReference{}
	for i, uri := range images {
		_, payload, _ := strings.Cut(uri, ",")
		data, _ := base64.StdEncoding.DecodeString(payload)
		index := imageInputs[i].Index - 1
		imageReferences = append(imageReferences, types.ModelCallImageReference{Path: imageRecords[index].URL, SHA256: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	ctx = types.WithModelCallImages(ctx, imageReferences)
	if len([]rune(body)) > bodyLimit {
		return nil, errors.New("segment exceeds configured body budget")
	}
	input.Body, _ = articleEvidenceBody(body)
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	if model.Parameters.ContextWindow > 0 && len([]rune(string(encoded)))+len([]rune(prompt))+completion+len(images)*1024+outputCorrectionBudget(library.ID) > model.Parameters.ContextWindow {
		return nil, errors.New("segment exceeds model context budget")
	}
	user := chat.Message{Role: "user", Content: string(encoded)}
	if len(images) > 0 {
		user.Images = images
	}
	thinking := false
	var result *articleProcessingResult
	err = chatWithOutputValidation(ctx, modelChat, []chat.Message{{Role: "system", Content: prompt}, user}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion, Thinking: &thinking}, func(raw string) error {
		var err error
		result, err = parseArticleProcessingResult(raw)
		if err != nil {
			return err
		}
		if result.Title == "" {
			return errors.New("article processing output is missing a generated title")
		}
		result.Images = imageRecords
		qualifyUnderstandingEntityMentions(result, body)
		qualifyUnderstandingChoices(result, input)
		return nil
	}, library.ID)
	if err != nil {
		return nil, fmt.Errorf("article processing call failed: %w", err)
	}
	// Verify only structurally eligible candidates. The caller restores whole-document coordinates.
	qualifySegmentOutput(library, k, body, types.ProcessingSegment{End: len([]rune(body))}, result)
	return result, nil
}

func (s *entryService) markArticleProcessingFailed(ctx context.Context, k *types.Entry, err error) {
	if errors.Is(err, repository.ErrEntrySuperseded) {
		return
	}
	if ctx.Err() != nil {
		return
	}
	if k.ParseStatus == types.ParseStatusCompleted {
		k.SummaryStatus, k.ErrorMessage, k.UpdatedAt = types.SummaryStatusFailed, err.Error(), time.Now()
		_ = s.repo.UpdateEntry(ctx, k)
		s.failStage(ctx, k.ID, types.StageProcessing, "ARTICLE_PROCESSING_FAILED", err.Error(), err)
		return
	}
	if types.IsChatGPTDeferred(err) {
		k.ParseStatus, k.SummaryStatus, k.ErrorMessage, k.UpdatedAt = types.ParseStatusPending, types.SummaryStatusPending, err.Error(), time.Now()
		_ = s.repo.UpdateEntry(ctx, k)
		return
	}
	s.recordTerminalMutationFailure()
	k.ParseStatus, k.SummaryStatus, k.ErrorMessage, k.UpdatedAt = types.ParseStatusFailed, types.SummaryStatusFailed, err.Error(), time.Now()
	_ = s.repo.UpdateEntry(ctx, k)
	s.failStage(ctx, k.ID, types.StageProcessing, "ARTICLE_PROCESSING_FAILED", err.Error(), err)
}

func (s *entryService) processArticleBody(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage, eff types.EffectiveProcessConfig) error {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	writeFiles := ctx.Value(libraryRebuildContextKey{}) != true
	if aborted, _ := s.isEntryAborted(ctx, k.SpaceID, k.ID); aborted {
		return nil
	}
	// Publish the body index before calling any enrichment model.
	if k.ParseStatus != types.ParseStatusCompleted {
		chunks, options := splitArticleBody(body, eff.ChunkingConfig)
		s.processChunks(ctx, library, k, chunks, options)
		if k.ParseStatus != types.ParseStatusCompleted {
			return errors.New(k.ErrorMessage)
		}
	}
	k.SummaryStatus = types.SummaryStatusProcessing
	if err := s.repo.UpdateEntry(ctx, k); err != nil {
		return err
	}
	if err := s.rejectProcessingGIFs(ctx, library, k, body); err != nil {
		return err
	}
	result, err := s.callArticleProcessing(ctx, library, k, body, images)
	if err != nil {
		return err
	}
	return s.persistArticleProcessingResult(ctx, library, k, body, images, eff, result, writeFiles)
}

// persistArticleProcessingResult is shared by ingestion and a user conversion.
// A conversion prepares the model result before opening its database transaction
// and owns the file write so it can restore the previous file on rollback.
func (s *entryService) persistArticleProcessingResult(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage, eff types.EffectiveProcessConfig, result *articleProcessingResult, writeFiles bool) error {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	if aborted, _ := s.isEntryAborted(ctx, k.SpaceID, k.ID); aborted {
		return nil
	}
	if s.db != nil {
		if isIngestArticle(k) {
			var latest types.Entry
			if err := s.db.WithContext(ctx).Select("acquisition",
				"ingest_api_key_id",
				"ingest_api_key_name",
				"last_ingest_api_key_id",
				"last_ingest_api_key_name",
				"related_entry_id").Where("id=? AND space_id=?",
				k.ID,
				k.SpaceID).Take(&latest).Error; err != nil {
				return err
			}
			k.Acquisition = latest.Acquisition
			k.IngestAPIKeyID, k.IngestAPIKeyName = latest.IngestAPIKeyID, latest.IngestAPIKeyName
			k.LastIngestAPIKeyID, k.LastIngestAPIKeyName = latest.LastIngestAPIKeyID, latest.LastIngestAPIKeyName
			k.RelatedEntryID = latest.RelatedEntryID
		}
		var current types.Entry
		if err := s.db.WithContext(ctx).Model(&types.Entry{}).Select("title",
			"file_name",
			"source_unverified").Where("id=? AND space_id=?",
			k.ID,
			k.SpaceID).Take(&current).Error; err != nil {
			return err
		}
		k.Title, k.FileName = current.Title, current.FileName
		k.SourceUnverified = current.SourceUnverified
	}
	if result.Title != "" {
		k.Title = result.Title
		k.FileName = sanitizeManualDownloadFilename(k.Title)
	}
	k.StructuredSummary = result.Summary
	k.Description = result.Summary.RenderText()
	k.SummaryStatus = types.SummaryStatusProcessing
	if err := s.repo.UpdateEntry(ctx, k); err != nil {
		return err
	}
	if s.db != nil {
		if err := s.saveUnderstandingChoices(ctx, k, result, false); err != nil {
			return err
		}
		result.Entities = nil
		result.ResolvedEntities = nil
		if err := s.saveUnderstandingFact(ctx, library, k, body, result); err != nil {
			return err
		}
		if err := s.saveProcessingOutput(ctx, k, body, result); err != nil {
			return err
		}
	}
	if writeFiles && s.vault == nil {
		if err := s.storeProcessedBody(ctx, library, k, body, images); err != nil {
			return err
		}
	}
	if k.ParseStatus != types.ParseStatusCompleted {
		chunks, options := splitArticleBody(body, eff.ChunkingConfig)
		s.processChunks(ctx, library, k, chunks, options)
		if k.ParseStatus == types.ParseStatusFailed {
			return errors.New(k.ErrorMessage)
		}
	}
	if err := indexArticleEmbedding(ctx, s.db, s.modelService, library, k); err != nil {
		if err := s.deferEntryVectors(ctx, k, err); err != nil {
			return err
		}
	}
	if writeFiles && s.vault != nil {
		read := func(ctx context.Context, ref string) (io.ReadCloser, error) {
			return s.resolveFileServiceForPath(ctx, library, ref).GetFile(ctx, ref)
		}
		if err := s.vault.WriteArticle(ctx, k, body, read); err != nil {
			s.vault.recordArticleError(ctx, k.ID, err)
			return fmt.Errorf("write article to vault: %w", err)
		}
		if err := recalculateBodyLocations(ctx, s.db, k, body, k.VaultBody, nil); err != nil {
			return err
		}
		if err := s.removeParsedBodyCopies(ctx, library, k, body, images); err != nil {
			return err
		}
	}
	if err := indexArticleImages(ctx, s.db, s.modelService, library, s.vault, k); err != nil {
		if err := s.deferEntryVectors(ctx, k, err); err != nil {
			return err
		}
	}
	k.SummaryStatus, k.ErrorMessage = types.SummaryStatusCompleted, ""
	if err := s.repo.UpdateEntry(ctx, k); err != nil {
		return err
	}
	s.tracker().FinalizeAttempt(ctx, k.ID, attemptFromCtx(ctx), "done", types.JSONMap{}, "", "")
	return nil
}

// removeParsedBodyCopies drops internal Markdown originals only after the
// approved article is available in the vault. Image
// providers remain available for indexed images.
func (s *entryService) removeParsedBodyCopies(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage) error {
	metadata := map[string]json.RawMessage{}
	if len(k.Metadata) > 0 {
		if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
			return err
		}
	}
	var copies []string
	var processedPath string
	_ = json.Unmarshal(metadata["processed_body_path"], &processedPath)
	if processedPath != "" {
		copies = append(copies, processedPath)
		delete(metadata, "processed_body_path")
	}
	if intake, err := k.IntakeMetadata(); err != nil {
		return err
	} else if intake != nil && intake.OriginalPath != "" {
		copies = append(copies, intake.OriginalPath)
		intake.OriginalPath = ""
		metadata["intake"], _ = json.Marshal(intake)
	}
	metadata["processed_images"], _ = json.Marshal(articleImagesInOrder(body, images))
	if manual, err := k.ManualMetadata(); err != nil {
		return err
	} else if manual != nil && k.IsManual() {
		// Manual processing payloads still carry their content, while subsequent
		// reprocessing reads the vault body instead of this metadata copy.
		delete(metadata, "content")
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return err
	}
	k.Metadata = data
	if err = s.repo.UpdateEntryColumn(ctx, k.ID, "metadata", data); err != nil {
		return err
	}
	for _, ref := range copies {
		if ref == k.FilePath {
			continue
		}
		if err = s.resolveFileServiceForPath(ctx, library, ref).DeleteFile(ctx, ref); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("delete replaced parsed body copy %s: %w", ref, err)
		}
	}
	return nil
}

func splitArticleBody(body string, cfg types.ChunkingConfig) ([]types.ParsedChunk, ProcessChunksOptions) {
	base := buildSplitterConfigFromChunking(cfg)
	options := ProcessChunksOptions{Body: body}
	if cfg.EnableParentChild {
		parent, child := buildParentChildConfigs(cfg, base)
		result := chunker.SplitParentChild(body, parent, child)
		chunks := make([]types.ParsedChunk, len(result.Children))
		for i, c := range result.Children {
			chunks[i] = types.ParsedChunk{Content: c.Content, ContextHeader: c.ContextHeader, Seq: c.Seq, Start: c.Start, End: c.End, ParentIndex: c.ParentIndex, Images: articleChunkImages(c.Content)}
		}
		for _, p := range result.Parents {
			options.ParentChunks = append(options.ParentChunks, types.ParsedParentChunk{Content: p.Content, Seq: p.Seq, Start: p.Start, End: p.End})
		}
		return chunks, options
	}
	result := chunker.Split(body, base)
	chunks := make([]types.ParsedChunk, len(result))
	for i, c := range result {
		chunks[i] = types.ParsedChunk{Content: c.Content, ContextHeader: c.ContextHeader, Seq: c.Seq, Start: c.Start, End: c.End, Images: articleChunkImages(c.Content)}
	}
	return chunks, options
}

func articleChunkImages(content string) []types.ParsedImage {
	refs := articleImagesInOrder(content, nil)
	images := make([]types.ParsedImage, 0, len(refs))
	for _, ref := range refs {
		images = append(images, types.ParsedImage{URL: ref})
	}
	return images
}

// Relative image destinations in a persisted article already point to managed
// vault attachments. Validate their presence before the existing intake check.
func (s *entryService) hasUnstoredArticleImages(k *types.Entry, body string) bool {
	remote := map[string]string{}
	for _, ref := range articleImagesInOrder(body, nil) {
		if remoteIngestImage(ref) {
			remote[ref] = "local://remote/image"
		}
	}
	body = replaceIngestImageTargets(body, remote)
	if s.vault == nil || k.VaultPath == "" {
		return docparser.HasUnstoredImages(body)
	}
	for _, ref := range articleImagesInOrder(body, nil) {
		if strings.Contains(ref, "://") || strings.HasPrefix(ref, "/files/") || strings.HasPrefix(ref, "data:") {
			continue
		}
		file, err := s.vault.ReadImage(k, ref)
		if err != nil {
			return true
		}
		file.Close()
		body = strings.ReplaceAll(body, ref, "local://vault/attachment")
	}
	return docparser.HasUnstoredImages(body)
}

func (s *entryService) storeProcessedBody(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage) error {
	metadata := map[string]json.RawMessage{}
	if len(k.Metadata) > 0 {
		if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
			return err
		}
	}
	delete(metadata, "processed_body_path")
	metadata["processed_images"], _ = json.Marshal(articleImagesInOrder(body, images))
	var err error
	k.Metadata, err = json.Marshal(metadata)
	if err != nil {
		return err
	}
	k.VaultBody = body
	if err = s.repo.UpdateEntry(ctx, k); err != nil {
		return err
	}
	s.bindContentResources(ctx, k.SpaceID, k.ID, body)

	return nil
}

func (s *entryService) removeArticleMaterials(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage) error {
	// Delete body vectors without resolving an embedding model. New
	// deletion must work even when no embedding model is configured.
	if s.db != nil {
		if err := s.db.WithContext(ctx).Table("embeddings").Where("library_id=? AND entry_id=?", k.LibraryID, k.ID).Delete(nil).Error; err != nil {
			return err
		}
	}
	if err := s.chunkService.DeleteChunksByEntryID(ctx, k.ID); err != nil {
		return err
	}
	if k.StorageSize > 0 {
		if err := s.spaceRepo.AdjustStorageUsed(ctx, k.SpaceID, -k.StorageSize); err != nil {
			return err
		}
	}

	metadata := map[string]json.RawMessage{}
	if len(k.Metadata) > 0 {
		if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
			return err
		}
	}
	path := ""
	_ = json.Unmarshal(metadata["processed_body_path"], &path)
	oldImages := []string{}
	_ = json.Unmarshal(metadata["processed_images"], &oldImages)
	refs := append([]string{k.FilePath, path}, oldImages...)
	refs = append(refs, articleImagesInOrder(body, images)...)

	if intake, _ := k.IntakeMetadata(); intake != nil {
		refs = append(refs, intake.OriginalPath)
		refs = append(refs, intake.Images...)
		intake.OriginalPath = ""
		intake.Images = nil
		if err := k.SetIntakeMetadata(intake); err != nil {
			return err
		}
		if err := json.Unmarshal(k.Metadata, &metadata); err != nil {
			return err
		}
	}
	releaser := entryResourceOwners(s.resourceCatalog, k.ID)
	seen := map[string]bool{}
	for _, ref := range refs {
		if ref == "" || seen[ref] || strings.HasPrefix(ref, "http://") || strings.HasPrefix(ref, "https://") || strings.HasPrefix(ref, "data:") {
			continue
		}
		seen[ref] = true
		if releaser.deletable(ctx, ref) {
			if err := s.resolveFileServiceForPath(ctx, library, ref).DeleteFile(ctx, ref); err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("delete article material %s: %w", ref, err)
			}
		}
	}
	delete(metadata, "processed_body_path")
	delete(metadata, "processed_images")
	delete(metadata, "content")
	delete(metadata, "chunks")
	k.Metadata, _ = json.Marshal(metadata)
	if manual, _ := k.ManualMetadata(); manual != nil {
		manual.Content = ""
		if err := k.SetManualMetadata(manual); err != nil {
			return err
		}
	}
	k.FilePath = ""
	k.StorageSize = 0
	k.EmbeddingModelID = ""

	return s.repo.UpdateEntry(ctx, k)
}

func entrySourceInfo(k *types.Entry) (types.SourceInfo, error) {
	if intake, err := k.IntakeMetadata(); err != nil {
		return types.SourceInfo{}, err
	} else if intake != nil {
		return intake.SourceInfo, nil
	}

	meta := k.GetMetadata()
	return types.SourceInfo{Publisher: meta["publisher"], Author: meta["author"], PublishedAt: meta["published_at"], OriginalURL: meta["original_url"]}, nil
}
