package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

type segmentReviewDecision struct {
	Approved *bool  `json:"approved"`
	Reason   string `json:"reason"`
}

type deferSegmentReviewKey struct{}

var errSegmentReviewQueued = errors.New("segmented intake review requires background execution")

func segmentReviewPrompt(library *types.Library) string {
	return intakeReviewPrompt(library) + "\nThe body is one segment of the document. Apply the criteria to this segment; context_before is only preceding context."
}

func (s *entryService) rejectSegmentLimit(ctx context.Context, entry *types.Entry, err error) (bool, error) {
	var limit *types.SegmentLimitError
	if !errors.As(err, &limit) {
		return false, err
	}
	intake, e := entry.IntakeMetadata()
	if e != nil {
		return false, e
	}
	if intake == nil {
		intake = &types.EntryIntakeMetadata{}
	}
	intake.Decision, intake.Reason = "rejected", limit.Error()
	entry.ParseStatus, entry.EnableStatus = types.ParseStatusRejected, "disabled"
	if e = entry.SetIntakeMetadata(intake); e != nil {
		return false, e
	}
	return true, s.repo.UpdateEntry(ctx, entry)
}

func (s *entryService) hasSegmentCheckpoint(ctx context.Context, entry *types.Entry) bool {
	if s.db == nil {
		return false
	}
	var count int64
	err := s.db.WithContext(ctx).Model(&entrySegmentRun{}).Where("entry_id=? AND space_id=? AND status IN ?", entry.ID, entry.SpaceID, []string{"pending", "running", "failed"}).Count(&count).Error
	// A database outage must not turn a resumable failure into source deletion.
	return err != nil || count > 0
}

func (s *entryService) singleReviewSegment(ctx context.Context, library *types.Library, entry *types.Entry, body string) bool {
	id, err := ResolveModelTier(ctx, s.db, library, types.ModelTierLight)
	if err != nil || id == "" || s.modelService == nil {
		return false
	}
	model, err := s.modelService.GetModelByID(ctx, id)
	if err != nil {
		return false
	}
	intake, err := entry.IntakeMetadata()
	if err != nil || intake == nil {
		return false
	}
	source := intake.SourceInfo
	source.Confirmed = false
	metadata := struct {
		SourceInfo types.SourceInfo `json:"source_info"`
	}{source}
	budget, err := s.segmentBudget(model, segmentReviewPrompt(library), metadata, 0)
	if err != nil {
		return false
	}
	return len([]rune(body)) <= budget
}

func (s *entryService) reviewSegments(ctx context.Context, library *types.Library, entry *types.Entry, body string, source types.SourceInfo) (bool, string, error) {
	ctx = types.WithModelCallEntry(ctx, entry.ID)
	id, err := ResolveModelTier(ctx, s.db, library, types.ModelTierLight)
	if err != nil {
		return false, "", err
	}
	if id == "" {
		return false, "", errors.New("light model is not configured")
	}
	if s.modelService == nil {
		return false, "", errors.New("model service is unavailable")
	}
	model, err := s.modelService.GetModelByID(ctx, id)
	if err != nil {
		return false, "", err
	}
	if model == nil {
		return false, "", errors.New("light model is unavailable")
	}
	prompt := segmentReviewPrompt(library)
	metadata := struct {
		SourceInfo types.SourceInfo `json:"source_info"`
	}{source}
	budget, err := s.segmentBudget(model, prompt, metadata, 0)
	if err != nil {
		return false, "", err
	}
	// Enforce the processing cap before spending any review calls as well.
	if generationID, e := ResolveModelTier(ctx, s.db, library, types.ModelTierGeneration); e == nil && generationID != "" {
		input, processingPrompt, generation, e := s.prepareArticleProcessing(ctx, library, entry)
		if e != nil {
			return false, "", e
		}
		_, images, _ := s.articleProcessingLimits(generation)
		imageCount := 0
		if generation.Parameters.SupportsVision {
			imageCount = min(images, len(articleImagesInOrder(body, nil)))
		}
		processingBudget, e := s.segmentBudget(generation, processingPrompt, input, imageCount)
		if e != nil {
			return false, "", e
		}
		limit, overlap, _ := s.segmentSettings()
		if _, e = types.PlanProcessingSegments(body, processingBudget, overlap, limit); e != nil {
			return false, "", e
		}
	}
	run, err := s.openSegmentRun(ctx, entry, "review", body, budget, []any{prompt, metadata, segmentModelIdentity(model)})
	if err != nil {
		return false, "", err
	}
	if deferReview, _ := ctx.Value(deferSegmentReviewKey{}).(bool); deferReview {
		var plan []segmentCheckpoint
		if err := json.Unmarshal(run.Plan, &plan); err != nil {
			return false, "", err
		}
		if len(plan) > 1 {
			return false, "", errSegmentReviewQueued
		}
	}
	llm, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierLight)
	if err != nil {
		return false, "", err
	}
	runes := []rune(body)
	parts, err := s.executeSegments(ctx, entry, run, func(ctx context.Context, p types.ProcessingSegment, count int) (json.RawMessage, error) {
		input := map[string]any{"source_info": source, "body": string(runes[p.Start:p.End]), "segment": segmentInput(p, count)}
		encoded, e := json.Marshal(input)
		if e != nil {
			return nil, e
		}
		_, _, completion := s.articleProcessingLimits(model)
		var decision segmentReviewDecision
		e = chatWithOutputValidation(types.WithLLMCallMetadata(ctx, "intake_review", ""), llm, []chat.Message{{Role: "system", Content: prompt}, {Role: "user", Content: string(encoded)}}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion}, func(raw string) error {
			decision = segmentReviewDecision{}
			if err := utils.ParseLLMJsonResponse(raw, &decision); err != nil {
				return fmt.Errorf("invalid intake review response: %w", err)
			}
			if decision.Approved == nil {
				return errors.New("invalid intake review response: approved must be a boolean")
			}
			if strings.TrimSpace(decision.Reason) == "" {
				return errors.New("invalid intake review response: reason must be non-empty")
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
		return json.Marshal(decision)
	})
	if err != nil {
		// A known rejection is decisive even if a later segment fails. Keep
		// the incomplete review checkpoint, and expose the rejection for review.
		var saved []segmentCheckpoint
		if json.Unmarshal(run.Plan, &saved) == nil {
			if reason := segmentRejectionReason(saved); reason != "" {
				return false, reason, nil
			}
		}
		return false, "", err
	}
	reasons := []string{}
	for i, part := range parts {
		var decision segmentReviewDecision
		if err = json.Unmarshal(part.Result, &decision); err != nil {
			return false, "", err
		}
		if !*decision.Approved {
			reasons = append(reasons, fmt.Sprintf("第 %d/%d 段: %s", i+1, len(parts), decision.Reason))
		}
	}
	approved := len(reasons) == 0
	reason := strings.Join(reasons, "; ")
	err = s.mutateSegmentRun(ctx, run, func(current *entrySegmentRun) error {
		raw, e := json.Marshal(segmentReviewDecision{Approved: &approved, Reason: reason})
		current.Result, current.Status = types.JSON(raw), "completed"
		return e
	})
	return approved, reason, err
}

func segmentRejectionReason(parts []segmentCheckpoint) string {
	reasons := []string{}
	for i, part := range parts {
		if part.Status != "completed" {
			continue
		}
		var decision segmentReviewDecision
		if json.Unmarshal(part.Result, &decision) == nil && decision.Approved != nil && !*decision.Approved {
			reasons = append(reasons, fmt.Sprintf("第 %d/%d 段: %s", i+1, len(parts), decision.Reason))
		}
	}
	return strings.Join(reasons, "; ")
}
