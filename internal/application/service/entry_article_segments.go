package service

import (
	"context"
	"encoding/json"
	"errors"

	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// SegmentContribution is the extension boundary for ikb-11. New extractors
// add keyed contributions here, then merge them without changing checkpoints.
type SegmentContribution struct {
	Segment int             `json:"segment"`
	Value   json.RawMessage `json:"value"`
}

type articleProcessingEntity struct {
	Name        string                 `json:"name"`
	Type        *string                `json:"type"`
	Aliases     []string               `json:"aliases,omitempty"`
	Description string                 `json:"description"`
	Relevance   string                 `json:"relevance"`
	EvidenceIDs []int                  `json:"evidence_ids"`
	Evidence    []types.SourceLocation `json:"evidence,omitempty"`
}

type articleProcessingProvenance struct {
	Summary          []int            `json:"summary"`
	ContentTypes     map[string][]int `json:"content_types"`
	Entities         map[string][]int `json:"entities"`
	Topics           map[string][]int `json:"topics"`
	ResolvedEntities map[string][]int `json:"resolved_entities,omitempty"`
}

func (s *entryService) prepareArticleProcessing(ctx context.Context, library *types.Library, k *types.Entry) (articleProcessingInput, string, *types.Model, error) {
	input := articleProcessingInput{Title: articleTitleForProcessing(k), ContentTypeHint: k.ContentTypeHint, ExistingTopics: []articleProcessingTopic{}}
	source, err := entrySourceInfo(k)
	if err != nil {
		return input, "", nil, err
	}
	input.Publisher, input.PublishedAt, input.Author = source.Publisher, source.PublishedAt, source.Author
	if s.db != nil {
		topics, err := NewTopicService(s.db).TopicsForLibrary(ctx, k.LibraryID)
		if err != nil {
			return input, "", nil, err
		}
		for _, topic := range topics {
			input.ExistingTopics = append(input.ExistingTopics, articleProcessingTopic{ID: topic.ID, Name: topic.Name, Description: topic.Description})
		}
	}
	prompt := libraryProcessingPrompt(library, k)
	prompt += "\n图片附件与输入 images 的顺序一一对应。仅依据已提供的可读图片理解文章，不猜测缺失或不可读图片的内容。"
	if input.ContentTypeHint != "" {
		prompt += "\n" + config.Prompt("content_type_hint")
	}
	id, err := ResolveModelTier(ctx, s.db, library, types.ModelTierGeneration)
	if err != nil {
		return input, prompt, nil, err
	}
	if id == "" {
		return input, prompt, nil, errors.New("article processing model is not configured")
	}
	if s.modelService == nil {
		return input, prompt, nil, errors.New("model service is unavailable")
	}
	model, err := s.modelService.GetModelByID(ctx, id)
	if err == nil && model == nil {
		err = errors.New("article processing model is unavailable")
	}
	return input, prompt, model, err
}

func (s *entryService) callArticleProcessing(ctx context.Context, library *types.Library, k *types.Entry, body string, images []docparser.StoredImage) (*articleProcessingResult, error) {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	ctx, calls := types.WithModelCallCollector(ctx)
	input, prompt, model, err := s.prepareArticleProcessing(ctx, library, k)
	if err != nil {
		return nil, err
	}
	_, imageLimit, _ := s.articleProcessingLimits(model)
	imageCount := 0
	if model.Parameters.SupportsVision {
		for _, ref := range articleImagesInOrder(body, images) {
			if !remoteIngestImage(ref) {
				imageCount++
			}
		}
		imageCount = min(imageLimit, imageCount)
	}
	budget, err := s.segmentBudget(model, prompt, input, imageCount)
	if err != nil {
		return nil, err
	}
	if model.Parameters.ContextWindow > 0 {
		// Numbering and JSON escaping consume input space too. Keep planning in
		// original coordinates, reducing the source budget until every part fits.
		limit, overlap, _ := s.segmentSettings()
		_, _, completion := s.articleProcessingLimits(model)
		metadata, _ := json.Marshal(input)
		inputBudget := model.Parameters.ContextWindow - completion - len([]rune(prompt)) - len([]rune(string(metadata))) - imageCount*1024 - segmentEnvelopeReserve - overlap
		_, imageLimit, _ := s.articleProcessingLimits(nil)
		runes := []rune(body)
		for {
			plan, err := types.PlanProcessingSegments(body, budget, overlap, limit)
			if err != nil {
				return nil, err
			}
			plan, err = imageBoundedSegments(body, plan, imageLimit, limit)
			if err != nil {
				return nil, err
			}
			largest := 0
			for _, part := range plan {
				numbered, _ := articleEvidenceBody(string(runes[part.Start:part.End]))
				encoded, _ := json.Marshal(numbered)
				largest = max(largest, len([]rune(string(encoded)))-2)
			}
			if largest <= inputBudget {
				break
			}
			if budget == 1 {
				return nil, errors.New("model context is too small for evidence numbering")
			}
			budget = max(1, min(budget-1, budget*inputBudget/largest))
		}
	}
	// Topic eligibility is live catalog state, not a document processing revision.
	// Completed segment results are retained when the catalog changes; new
	// segments use the current candidates and persistence revalidates all IDs.
	fingerprintInput := input
	fingerprintInput.ExistingTopics = []articleProcessingTopic{}
	run, err := s.openSegmentRun(ctx, k, "processing", body, budget, []any{
		fingerprintInput, prompt, segmentSummaryPrompt(), entityResolutionPrompt(), segmentModelIdentity(model), budget,
	})
	if err != nil {
		return nil, err
	}
	s.beginStage(ctx, k.ID, types.StageProcessing, types.JSONMap{"source_body_chars": len([]rune(body)), "run_id": run.ID})
	runes := []rune(body)
	parts, err := s.executeSegments(ctx, k, run, func(ctx context.Context, p types.ProcessingSegment, count int) (json.RawMessage, error) {
		stored := []docparser.StoredImage(nil)
		if count == 1 {
			stored = images
		}
		refs := segmentImageReferences(body, p)
		result, err := s.callArticleProcessingPart(ctx, library, k, string(runes[p.Start:p.End]), stored, segmentInput(p, count), refs)
		if err != nil {
			return nil, err
		}
		allowed := map[string]bool{}
		for _, topic := range input.ExistingTopics {
			allowed[topic.ID] = true
		}
		topics := []string{}
		for _, id := range result.Topics {
			if allowed[id] {
				topics = append(topics, id)
			}
		}
		result.Topics = topics
		qualifySegmentOutput(library, k, body, p, result)
		qualifyImageLocations(body, p, result, refs)
		return json.Marshal(result)
	})
	if err != nil {
		return nil, err
	}
	var result *articleProcessingResult
	err = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
		if len(current.Result) > 0 {
			return json.Unmarshal(current.Result, &result)
		}
		results := make([]*articleProcessingResult, len(parts))
		for i, part := range parts {
			var err error
			results[i], err = parseArticleProcessingResult(string(part.Result))
			if err != nil {
				return err
			}
		}
		result = mergeArticleSegments(results)
		result.RunID = current.ID
		if len(results) > 1 {
			summary, err := s.mergeSegmentSummaries(ctx, library, k, results, model)
			if err != nil {
				return err
			}
			result.Title, result.Summary = summary.Title, summary.Summary
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		current.Result, current.Status = types.JSON(data), "completed"
		return nil
	})
	if err != nil {
		if ctx.Err() == nil {
			_ = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error { current.Status = "failed"; return nil })
		}
		return nil, err
	}
	s.endStage(ctx, k.ID, types.StageProcessing, types.JSONMap{"segment_count": len(parts), "completed_segments": len(parts), "run_id": run.ID, "content_type": *result.ContentType, "content_type_rule": "majority_first_on_tie", "model_calls": calls.Snapshot()})
	return result, nil
}

func (s *entryService) resolveProcessingEntities(ctx context.Context, k *types.Entry, candidates []types.EntityCandidate, result *articleProcessingResult) ([]types.Entity, error) {
	resolved := []types.Entity{}
	identities := map[string]string{}
	for offset := 0; offset < len(candidates); offset += 500 {
		batch, mapping, err := NewEntityService(s.db, s.modelService).resolveEntityCandidates(ctx, k.ID, candidates[offset:min(offset+500, len(candidates))])
		if err != nil {
			return nil, err
		}
		resolved = append(resolved, batch...)
		for key, id := range mapping {
			identities[key] = id
		}
	}
	result.ResolvedEntities = identities
	if result.Provenance != nil {
		result.Provenance.ResolvedEntities = map[string][]int{}
		for key, id := range identities {
			for _, n := range result.Provenance.Entities[key] {
				result.Provenance.ResolvedEntities[id] = appendSegmentNumber(result.Provenance.ResolvedEntities[id], n)
			}
		}
		if result.RunID != "" {
			data, err := json.Marshal(result)
			if err != nil {
				return nil, err
			}
			if err = s.db.WithContext(ctx).Model(&entrySegmentRun{}).Where("id=? AND entry_id=? AND space_id=?", result.RunID, k.ID, k.SpaceID).Update("result", types.JSON(data)).Error; err != nil {
				return nil, err
			}
		}
	}
	return resolved, nil
}

func appendSegmentNumber(values []int, n int) []int {
	for _, v := range values {
		if v == n {
			return values
		}
	}
	return append(values, n)
}

// Entity identity remains the existing resolver's responsibility. This merge
// collapses exact normalized name/type candidates and retains all aliases.
func mergeArticleSegments(parts []*articleProcessingResult) *articleProcessingResult {
	p := &articleProcessingProvenance{ContentTypes: map[string][]int{}, Entities: map[string][]int{}, Topics: map[string][]int{}}
	merged := &articleProcessingResult{Title: parts[0].Title, Summary: parts[0].Summary, Entities: []articleProcessingEntity{}, Topics: []string{}, Provenance: p, Extensions: map[string][]SegmentContribution{}}
	entityIndex := map[string]int{}
	winningType := ""
	winningCount := 0
	for i, part := range parts {
		n := i + 1
		merged.Images = append(merged.Images, part.Images...)
		p.Summary = append(p.Summary, n)
		p.ContentTypes[*part.ContentType] = append(p.ContentTypes[*part.ContentType], n)
		for _, entity := range part.Entities {
			key := entityNameKey(*entity.Type, entity.Name)
			if index, ok := entityIndex[key]; ok {
				if merged.Entities[index].Description == "" {
					merged.Entities[index].Description = entity.Description
					merged.Entities[index].Evidence = entity.Evidence
					merged.Entities[index].EvidenceIDs = entity.EvidenceIDs
				}
				for _, alias := range entity.Aliases {
					merged.Entities[index].Aliases = appendUniqueEntityString(merged.Entities[index].Aliases, alias)
				}
			} else {
				entityIndex[key] = len(merged.Entities)
				merged.Entities = append(merged.Entities, entity)
			}
			p.Entities[key] = appendSegmentNumber(p.Entities[key], n)
		}
		for _, topic := range part.Topics {
			if len(p.Topics[topic]) == 0 {
				merged.Topics = append(merged.Topics, topic)
			}
			p.Topics[topic] = appendSegmentNumber(p.Topics[topic], n)
		}
		for key, values := range part.Extensions {
			for _, value := range values {
				value.Segment = n
				merged.Extensions[key] = append(merged.Extensions[key], value)
			}
		}
	}
	// Iterate source order, so a final tie always picks the earliest segment.
	for _, part := range parts {
		v := *part.ContentType
		if len(p.ContentTypes[v]) > winningCount {
			winningType, winningCount = v, len(p.ContentTypes[v])
		}
	}
	merged.ContentType = &winningType
	return merged
}

func segmentSummaryPrompt() string {
	return config.Prompt("summary_merge") + `只输出 JSON {"title":"资料标题","summary":{"overview":"一句话概述","points":["原文支持的要点"],"key_terms":["关键名词"]}}。`
}

func (s *entryService) mergeSegmentSummaries(ctx context.Context, library *types.Library, k *types.Entry, parts []*articleProcessingResult, model *types.Model) (*articleProcessingResult, error) {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	summaries := make([]any, len(parts))
	for i, part := range parts {
		summaries[i] = map[string]any{"segment": i + 1, "summary": part.Summary}
	}
	title := articleTitleForProcessing(k)
	input, _ := json.Marshal(map[string]any{"title": title, "segments": summaries})
	_, _, completion := s.articleProcessingLimits(model)
	if model.Parameters.ContextWindow > 0 && len([]rune(string(input)))+len([]rune(segmentSummaryPrompt()))+completion+modelOutputCorrectionReserve > model.Parameters.ContextWindow {
		return nil, errors.New("各段摘要超过合并模型上下文，请提高模型上下文或减少分段数")
	}
	llm, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierGeneration)
	if err != nil {
		return nil, err
	}
	ctx, calls := types.WithModelCallCollector(ctx)
	parent := s.tracker().LookupStage(ctx, k.ID, attemptFromCtx(ctx), types.StageProcessing)
	span := s.tracker().BeginSubSpan(ctx, parent, "processing.summary_merge", types.SpanKindGeneration, types.JSONMap{"segment_count": len(parts)})
	if span != nil {
		ctx = types.WithModelCallTrace(ctx, span.Attempt, span.SpanID)
	}
	var summary *articleProcessingResult
	err = chatWithOutputValidation(types.WithLLMCallMetadata(ctx, "article_summary_merge", ""), llm, []chat.Message{{Role: "system", Content: segmentSummaryPrompt()}, {Role: "user", Content: string(input)}}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion}, func(raw string) error {
		var output struct {
			Title   string              `json:"title"`
			Summary *types.EntrySummary `json:"summary"`
		}
		if err := utils.ParseLLMJsonResponse(raw, &output); err != nil {
			return err
		}
		encoded, _ := json.Marshal(map[string]any{
			"title": output.Title, "summary": output.Summary, "entities": []any{}, "topics": []string{},
		})
		checked, err := parseArticleProcessingResult(string(encoded))
		if err == nil && title == "" && checked.Title == "" {
			return errors.New("summary merge output is missing a generated title")
		}
		if err == nil {
			summary = checked
		}
		return err
	})
	if err != nil {
		s.tracker().FailSpan(ctx, span, "SUMMARY_MERGE_FAILED", err.Error(), err)
		return nil, err
	}
	s.tracker().EndSpan(ctx, span, types.JSONMap{"model_calls": calls.Snapshot(), "segment_count": len(parts)})
	return summary, nil
}
