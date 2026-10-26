package service

import (
	"context"
	"encoding/json"
	"errors"
	"reliquary/internal/config"
	"reliquary/internal/infrastructure/docparser"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
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
	Entities         map[string][]int `json:"entities"`
	ResolvedEntities map[string][]int `json:"resolved_entities,omitempty"`
}

func (s *entryService) prepareArticleProcessing(ctx context.Context, library *types.Library, k *types.Entry) (articleProcessingInput, string, *types.Model, error) {
	input := articleProcessingInput{
		LibraryName: library.Name,
		Title:       articleTitleForProcessing(k),
		FactAllowed: materialCanProduceFact(library, k),
	}
	if err := loadUnderstandingOptions(ctx, s.db, k.SpaceID, &input); err != nil {
		return input, "", nil, err
	}
	source, err := entrySourceInfo(k)
	if err != nil {
		return input, "", nil, err
	}
	input.Publisher, input.PublishedAt, input.Author = source.Publisher, source.PublishedAt, source.Author
	prompt := libraryProcessingPrompt(library, k)
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
	budget, err := s.segmentBudget(model, prompt, input, imageCount, library.ID)
	if err != nil {
		return nil, err
	}
	if model.Parameters.ContextWindow > 0 {
		// Numbering and JSON escaping consume input space too. Keep planning in
		// original coordinates, reducing the source budget until every part fits.
		limit, overlap, _ := s.segmentSettings()
		_, _, completion := s.articleProcessingLimits(model)
		metadata, _ := json.Marshal(input)
		inputBudget := model.Parameters.ContextWindow - completion - len([]rune(prompt)) - len([]rune(string(metadata))) - imageCount*1024 - segmentEnvelopeReserve - overlap - outputCorrectionBudget(library.ID)
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
	run, err := s.openSegmentRun(ctx, k, "processing", body, budget, []any{
		input, prompt, segmentSummaryPrompt(library.ID), segmentModelIdentity(model), budget,
		ctx.Value(domainReclassificationKey{}), config.Prompt("output_correction", library.ID),
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
			result.DomainIDs, result.EntityIDs = summary.DomainIDs, summary.EntityIDs
			result.EntityMentions = summary.EntityMentions
			result.Fact = summary.Fact
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
	s.endStage(
		ctx,
		k.ID,
		types.StageProcessing,
		types.JSONMap{
			"segment_count":      len(parts),
			"completed_segments": len(parts),
			"run_id":             run.ID,
			"model_calls":        calls.Snapshot(),
		},
	)
	return result, nil
}

func appendSegmentNumber(values []int, n int) []int {
	for _, v := range values {
		if v == n {
			return values
		}
	}
	return append(values, n)
}

// Merge summaries, images and extension contributions from accepted segments.
func mergeArticleSegments(parts []*articleProcessingResult) *articleProcessingResult {
	p := &articleProcessingProvenance{Entities: map[string][]int{}}
	merged := &articleProcessingResult{
		Title:          parts[0].Title,
		Fact:           parts[0].Fact,
		Summary:        parts[0].Summary,
		DomainIDs:      parts[0].DomainIDs,
		EntityIDs:      parts[0].EntityIDs,
		EntityMentions: parts[0].EntityMentions,
		Entities:       []articleProcessingEntity{},
		Provenance:     p,
		Extensions:     map[string][]SegmentContribution{},
	}
	for i, part := range parts {
		n := i + 1
		merged.Images = append(merged.Images, part.Images...)
		p.Summary = append(p.Summary, n)
		for key, values := range part.Extensions {
			for _, value := range values {
				value.Segment = n
				merged.Extensions[key] = append(merged.Extensions[key], value)
			}
		}
	}
	return merged
}

func segmentSummaryPrompt(libraryIDs ...string) string {
	return config.Prompt("summary_merge", libraryIDs...)
}

func (s *entryService) mergeSegmentSummaries(ctx context.Context, library *types.Library, k *types.Entry, parts []*articleProcessingResult, model *types.Model) (*articleProcessingResult, error) {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	summaries := make([]any, len(parts))
	for i, part := range parts {
		mentions := append([]understandingEntityMention{}, part.EntityMentions...)
		for j := range mentions {
			mentions[j].SourceID = entityMentionReference(i, j)
		}
		summaries[i] = map[string]any{
			"segment": i + 1, "title": part.Title, "summary": part.Summary,
			"domain_ids": part.DomainIDs, "entity_mentions": mentions, "fact": part.Fact,
		}
	}
	title := articleTitleForProcessing(k)
	var options articleProcessingInput
	if err := loadUnderstandingOptions(ctx, s.db, k.SpaceID, &options); err != nil {
		return nil, err
	}
	input, _ := json.Marshal(map[string]any{
		"title": title, "segments": summaries, "fact_allowed": materialCanProduceFact(library, k),
		"domain_options": options.DomainOptions, "entity_options": options.EntityOptions,
	})
	_, _, completion := s.articleProcessingLimits(model)
	if model.Parameters.ContextWindow > 0 && len([]rune(string(input)))+len([]rune(segmentSummaryPrompt(library.ID)))+completion+outputCorrectionBudget(library.ID) > model.Parameters.ContextWindow {
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
	err = chatWithOutputValidation(types.WithLLMCallMetadata(ctx, "article_summary_merge", ""), llm, []chat.Message{{Role: "system", Content: segmentSummaryPrompt(library.ID)}, {Role: "user", Content: string(input)}}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion}, func(raw string) error {
		checked, err := parseArticleProcessingResult(raw)
		if err == nil && checked.Title == "" {
			return errors.New("summary merge output is missing a generated title")
		}
		if err == nil {
			domains := map[string]bool{}
			for _, part := range parts {
				for _, id := range part.DomainIDs {
					domains[id] = true
				}
			}
			checked.DomainIDs = acceptedUnderstandingIDs(checked.DomainIDs, domains)
			qualifyMergedEntityMentions(checked, parts)
			qualifyUnderstandingChoices(checked, options)
			summary = checked
		}
		return err
	}, library.ID)
	if err != nil {
		s.tracker().FailSpan(ctx, span, "SUMMARY_MERGE_FAILED", err.Error(), err)
		return nil, err
	}
	s.tracker().EndSpan(ctx, span, types.JSONMap{"model_calls": calls.Snapshot(), "segment_count": len(parts)})
	return summary, nil
}
