package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"time"

	"github.com/google/uuid"
)

const (
	groupingSameOccurrence = "same_occurrence"
	groupingSameStory      = "same_story"
	groupingUnrelated      = "unrelated"
	groupingMention        = "mention"
)

type groupingAnswer struct {
	Result     string   `json:"result"`
	Confidence *float64 `json:"confidence"`
	Reason     string   `json:"reason"`
}

type factGroupingPlan struct {
	candidate *types.Fact
	result    string
	decisions []types.GroupingDecision
}

func (s *entryService) planFactGrouping(
	ctx context.Context,
	library *types.Library,
	entry *types.Entry,
	result *articleProcessingResult,
	fact *understandingFact,
) (*factGroupingPlan, error) {
	source, err := entrySourceInfo(entry)
	if err != nil {
		return nil, err
	}
	date := groupingDate(source.PublishedAt)
	text := entry.Title + "\n" + result.Summary.Overview
	if fact != nil {
		if occurred := groupingDate(fact.OccurredAt); occurred != nil {
			date = occurred
		}
		text = factRecallText(
			types.Fact{
				Title:      fact.Title,
				Subject:    fact.Subject,
				Action:     fact.Action,
				Object:     fact.Object,
				Conditions: fact.Conditions,
			},
		)
	}
	candidates, err := s.recallFacts(ctx, library, entry, text, date)
	if err != nil {
		return nil, err
	}
	plan := &factGroupingPlan{result: groupingUnrelated}
	if len(candidates) == 0 {
		return plan, nil
	}
	model, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierGeneration)
	if err != nil {
		return nil, err
	}
	ctx = types.WithLLMCallMetadata(types.WithModelCallEntry(ctx, entry.ID), "fact_grouping", "")
	_, reviewThreshold := s.config.GroupingThresholds()
	prompt := config.Prompt("fact_grouping", library.ID)
	for _, candidate := range candidates {
		incoming := map[string]any{"text": text, "fact": fact}
		payload := map[string]any{
			"left":          incoming,
			"right":         candidate.Fact,
			"incoming_side": "left",
			"mention_only":  fact == nil,
		}
		answer, err := callFactGrouping(ctx, model, payload, prompt)
		if err != nil {
			return nil, err
		}
		decision := types.GroupingDecision{
			ID: uuid.NewString(), SpaceID: entry.SpaceID, EntryID: entry.ID,
			CandidateFactID: candidate.ID, Similarity: candidate.Similarity, Result: answer.Result,
			Confidence: *answer.Confidence, AppliedResult: answer.Result, Reason: answer.Reason,
			ModelID: model.GetModelID(), CreatedAt: time.Now(),
		}
		confidence := *answer.Confidence
		if confidence >= reviewThreshold {
			payload["left"], payload["right"] = payload["right"], payload["left"]
			payload["incoming_side"] = "right"
			reviewed, err := callFactGrouping(ctx, model, payload, prompt)
			if err != nil {
				return nil, err
			}
			decision.ReviewResult = reviewed.Result
			decision.ReviewConfidence = reviewed.Confidence
			confidence = math.Min(confidence, *reviewed.Confidence)
			if reviewed.Result != answer.Result {
				decision.AppliedResult = groupingUnrelated
				decision.Reason += "; review_disagreed"
			}
		}
		// A material without a qualified occurrence can only mention a fact.
		if fact == nil && decision.AppliedResult != groupingUnrelated {
			decision.AppliedResult = groupingMention
		}
		if date == nil && fact != nil &&
			(decision.AppliedResult != groupingSameOccurrence || confidence < reviewThreshold) {
			decision.AppliedResult = groupingUnrelated
			decision.Reason += "; undated_requires_high_confidence_same_occurrence"
		}
		plan.decisions = append(plan.decisions, decision)
		// Prefer a duplicate occurrence over a continuation, then a mention. Recall order breaks ties.
		rank := func(v string) int {
			switch v {
			case groupingSameOccurrence:
				return 3
			case groupingSameStory:
				return 2
			case groupingMention:
				return 1
			}
			return 0
		}
		if rank(decision.AppliedResult) > rank(plan.result) {
			f := candidate.Fact
			plan.candidate = &f
			plan.result = decision.AppliedResult
		}
	}
	return plan, nil
}

func callFactGrouping(ctx context.Context, model chat.Chat, payload map[string]any, prompts ...string) (*groupingAnswer, error) {
	prompt := config.Prompt("fact_grouping")
	if len(prompts) > 0 {
		prompt = prompts[0]
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	response, err := model.Chat(ctx, []chat.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: string(raw)},
	}, &chat.ChatOptions{Temperature: 0, MaxCompletionTokens: 1024})
	if err != nil {
		return nil, err
	}
	if response == nil {
		return nil, fmt.Errorf("fact grouping returned no response")
	}
	var answer groupingAnswer
	if err = utils.ParseLLMJsonResponse(response.Content, &answer); err != nil {
		return nil, fmt.Errorf("invalid fact grouping response: %w", err)
	}
	switch answer.Result {
	case groupingSameOccurrence, groupingSameStory, groupingUnrelated, groupingMention:
	default:
		return nil, fmt.Errorf("unknown fact grouping result %q", answer.Result)
	}
	if answer.Confidence == nil || math.IsNaN(*answer.Confidence) || math.IsInf(*answer.Confidence, 0) ||
		*answer.Confidence < 0 ||
		*answer.Confidence > 1 {
		return nil, fmt.Errorf("invalid fact grouping confidence")
	}
	return &answer, nil
}
