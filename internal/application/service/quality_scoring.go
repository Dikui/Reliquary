package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

const (
	qualityScoringMaxTokens = 128
	// Two independent calls only reduce variance when sampling is not greedy.
	// Reasoning-model adapters may force temperature to zero; the calls still
	// act as an independent gate in that case.
	qualityScoringTemperature = 0.7
)

type qualityScoringInput struct {
	LibraryName string `json:"library_name"`
	Title       string `json:"title"`
	SourceName  string `json:"source_name,omitempty"`
	Author      string `json:"author,omitempty"`
	PublishedAt string `json:"published_at,omitempty"`
	Body        string `json:"body"`
}

// qualityScoringApplies reports whether the library's materials are filtered
// by quality. Own, generated and report material keeps its existing behavior.
func qualityScoringApplies(library *types.Library, k *types.Entry) bool {
	if library == nil || k == nil || k.SourceKind == "report" {
		return false
	}
	return library.Template == types.TemplateExternalTimely || library.Template == types.TemplateExternalDurable
}

func parseQualityScoringResult(raw string) (int, error) {
	var fields map[string]json.RawMessage
	if err := utils.ParseLLMJsonResponse(raw, &fields); err != nil {
		return 0, fmt.Errorf("quality scoring output is not valid JSON: %w", err)
	}
	var score int
	if err := json.Unmarshal(fields["attentionScore"], &score); err != nil {
		return 0, errors.New("quality scoring output is missing attentionScore")
	}
	if score < 0 || score > 100 {
		return 0, fmt.Errorf("quality scoring output %d is outside 0-100", score)
	}
	return score, nil
}

func (s *entryService) qualityThreshold() int {
	if s.config != nil {
		return s.config.QualityThreshold()
	}
	return config.DefaultQualityThreshold
}

func (s *entryService) qualityBodyChars() int {
	if s.config != nil {
		return s.config.QualityMaxBodyChars()
	}
	return config.DefaultQualityMaxBodyChars
}

func qualityScoringBody(body string, limit int) string {
	runes := []rune(body)
	if limit > 0 && len(runes) > limit {
		return string(runes[:limit])
	}
	return body
}

func (s *entryService) callQualityScore(ctx context.Context, llm chat.Chat, prompt string, input qualityScoringInput) (int, error) {
	encoded, err := json.Marshal(input)
	if err != nil {
		return 0, err
	}
	ctx = types.WithLLMCallMetadata(ctx, string(types.CallPurposeQualityScoring), "")
	response, err := llm.Chat(ctx, []chat.Message{
		{Role: "system", Content: prompt},
		{Role: "user", Content: string(encoded)},
	}, &chat.ChatOptions{
		Temperature:         qualityScoringTemperature,
		MaxTokens:           qualityScoringMaxTokens,
		MaxCompletionTokens: qualityScoringMaxTokens,
	})
	if err != nil {
		return 0, err
	}
	if response == nil {
		return 0, errors.New("quality scoring returned an empty response")
	}
	return parseQualityScoringResult(response.Content)
}

// scoreQuality runs the two independent scoring calls and returns both scores.
func (s *entryService) scoreQuality(ctx context.Context, library *types.Library, k *types.Entry, body string) (first, second int, err error) {
	ctx = types.WithModelCallEntry(ctx, k.ID)
	prompt := config.Prompt(string(types.CallPurposeQualityScoring), library.ID)
	llm, err := GetTierChatModel(ctx, s.db, s.modelService, library, types.ModelTierGeneration)
	if err != nil {
		return 0, 0, err
	}
	input := qualityScoringInput{
		LibraryName: library.Name,
		Title:       articleTitleForProcessing(k),
		SourceName:  k.SourceName,
		Body:        qualityScoringBody(body, s.qualityBodyChars()),
	}
	if source, sourceErr := entrySourceInfo(k); sourceErr == nil {
		input.Author, input.PublishedAt = source.Author, source.PublishedAt
	}
	if first, err = s.callQualityScore(ctx, llm, prompt, input); err != nil {
		return 0, 0, err
	}
	if second, err = s.callQualityScore(ctx, llm, prompt, input); err != nil {
		return 0, 0, err
	}
	return first, second, nil
}

func (s *entryService) qualityPassed(first, second int) bool {
	return first+second >= 2*s.qualityThreshold()
}

func (s *entryService) writeQualityScores(k *types.Entry, first, second int) {
	now := time.Now().UTC()
	k.QualityScoreFirst, k.QualityScoreSecond = first, second
	k.QualityScore = first + second
	k.QualityScoredAt = &now
	if s.qualityPassed(first, second) {
		k.QualityStatus = types.QualityStatusPassed
		return
	}
	k.QualityStatus = types.QualityStatusLow
}

// applyQualityGate scores the material before understanding. It reports
// whether understanding should continue; low-quality material is marked and
// skipped without producing entities or facts.
func (s *entryService) applyQualityGate(ctx context.Context, library *types.Library, k *types.Entry, body string) (bool, error) {
	if !qualityScoringApplies(library, k) {
		return true, nil
	}
	first, second, err := s.scoreQuality(ctx, library, k, body)
	if err != nil {
		return false, err
	}
	s.writeQualityScores(k, first, second)
	if k.QualityStatus == types.QualityStatusPassed {
		return true, s.repo.UpdateEntry(ctx, k)
	}
	now := time.Now().UTC()
	k.SummaryStatus = types.SummaryStatusSkipped
	k.ProcessedAt = &now
	k.ErrorMessage = ""
	return false, s.repo.UpdateEntry(ctx, k)
}

// scoreEntryQuality scores an already stored material for the batch action.
// It never changes the summary status: old material may already be understood.
func (s *entryService) scoreEntryQuality(ctx context.Context, library *types.Library, k *types.Entry) error {
	if !qualityScoringApplies(library, k) {
		return nil
	}
	body, available, err := s.reprocessStoredBody(ctx, k)
	if err != nil {
		return err
	}
	if !available {
		return errors.New("资料正文不可用")
	}
	first, second, err := s.scoreQuality(ctx, library, k, body)
	if err != nil {
		return err
	}
	s.writeQualityScores(k, first, second)
	return s.repo.UpdateEntry(ctx, k)
}
