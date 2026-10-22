package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reliquary/internal/config"
	"reliquary/internal/models/chat"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

func articleGraphVerificationPrompt() string {
	return config.Prompt("graph_verification") + `
只返回 JSON：{"verdicts":[{"id":"e0","status":"supported","description_status":"unsupported","reason":"身份有依据，简介添加了行业标签"},{"id":"r0","status":"insufficient_evidence","reason":"缺少关系动作的证据"}]}。
每个候选 id 必须恰好返回一次，不能省略或出现额外 id。实体必须返回 description_status，关系不需要。reason 必须非空、简短说明判定依据。`
}

type graphVerificationCandidate struct {
	ID       string                   `json:"id"`
	Entity   *articleProcessingEntity `json:"entity,omitempty"`
	Relation *extractedRelation       `json:"relation,omitempty"`
}

type graphVerificationVerdict struct {
	ID                string `json:"id"`
	Status            string `json:"status"`
	DescriptionStatus string `json:"description_status,omitempty"`
	Reason            string `json:"reason"`
}

type graphVerificationInput struct {
	Source     articleProcessingInput       `json:"source"`
	Candidates []graphVerificationCandidate `json:"candidates"`
}

func validGraphVerificationStatus(status string) bool {
	return status == "supported" || status == "unsupported" || status == "insufficient_evidence"
}

func verifyArticleGraph(ctx context.Context, model chat.Chat, metadata *types.Model, source articleProcessingInput, images []string, completion int, result *articleProcessingResult) error {
	candidates := []graphVerificationCandidate{}
	for i := range result.Entities {
		candidates = append(candidates, graphVerificationCandidate{ID: fmt.Sprintf("e%d", i), Entity: &result.Entities[i]})
	}
	for i := range result.Relations {
		candidates = append(candidates, graphVerificationCandidate{ID: fmt.Sprintf("r%d", i), Relation: &result.Relations[i]})
	}
	if len(candidates) == 0 {
		return nil
	}
	ctx = types.WithLLMCallMetadata(ctx, "article_graph_verification", "")
	source.ExistingTopics = nil
	verdicts := map[string]graphVerificationVerdict{}
	// Batch all candidates to leave output space for each verdict; never truncate.
	batchSize := max(1, (completion-32)/192)
	for start := 0; start < len(candidates); {
		end := min(start+batchSize, len(candidates))
		var encoded []byte
		for {
			var err error
			encoded, err = json.Marshal(graphVerificationInput{Source: source, Candidates: candidates[start:end]})
			if err != nil {
				return err
			}
			if metadata.Parameters.ContextWindow <= 0 || len([]rune(string(encoded)))+len([]rune(articleGraphVerificationPrompt()))+completion+len(images)*1024+modelOutputCorrectionReserve <= metadata.Parameters.ContextWindow {
				break
			}
			if end-start == 1 {
				return fmt.Errorf("graph verification candidate %s exceeds model context budget", candidates[start].ID)
			}
			end = start + (end-start)/2
		}
		expected := map[string]graphVerificationCandidate{}
		for _, candidate := range candidates[start:end] {
			expected[candidate.ID] = candidate
		}
		var accepted []graphVerificationVerdict
		thinking := false
		err := chatWithOutputValidation(ctx, model, []chat.Message{
			{Role: "system", Content: articleGraphVerificationPrompt()},
			{Role: "user", Content: string(encoded), Images: images},
		}, &chat.ChatOptions{Temperature: 0, MaxTokens: completion, MaxCompletionTokens: completion, Thinking: &thinking}, func(raw string) error {
			var output struct {
				Verdicts []graphVerificationVerdict `json:"verdicts"`
			}
			if err := utils.ParseLLMJsonResponse(raw, &output); err != nil {
				return err
			}
			if len(output.Verdicts) != len(expected) {
				return fmt.Errorf("graph verification must return exactly %d verdicts", len(expected))
			}
			seen := map[string]bool{}
			for _, verdict := range output.Verdicts {
				candidate, ok := expected[verdict.ID]
				if !ok || seen[verdict.ID] || !validGraphVerificationStatus(verdict.Status) || strings.TrimSpace(verdict.Reason) == "" || (candidate.Entity != nil && !validGraphVerificationStatus(verdict.DescriptionStatus)) {
					return fmt.Errorf("invalid or duplicate graph verification verdict %q", verdict.ID)
				}
				seen[verdict.ID] = true
			}
			accepted = output.Verdicts
			return nil
		})
		if err != nil {
			return fmt.Errorf("graph verification call failed: %w", err)
		}
		for _, verdict := range accepted {
			verdicts[verdict.ID] = verdict
		}
		start = end
	}
	// Save the original candidates before clearing descriptions or filtering facts.
	audit, err := json.Marshal(struct {
		Candidates []graphVerificationCandidate        `json:"candidates"`
		Verdicts   map[string]graphVerificationVerdict `json:"verdicts"`
	}{candidates, verdicts})
	if err != nil {
		return err
	}
	entities := []articleProcessingEntity{}
	retained := map[string]bool{}
	for i, entity := range result.Entities {
		verdict := verdicts[fmt.Sprintf("e%d", i)]
		if verdict.Status != "supported" {
			continue
		}
		if verdict.DescriptionStatus != "supported" {
			entity.Description = ""
		}
		entities = append(entities, entity)
		retained[normalizeEntityName(entity.Name)] = true
	}
	relations := []extractedRelation{}
	for i, relation := range result.Relations {
		if verdicts[fmt.Sprintf("r%d", i)].Status == "supported" && retained[normalizeEntityName(relation.Subject)] && retained[normalizeEntityName(relation.Object)] {
			relations = append(relations, relation)
		}
	}
	result.Entities, result.Relations = entities, relations
	if result.Extensions == nil {
		result.Extensions = map[string][]SegmentContribution{}
	}
	result.Extensions["graph_verification"] = []SegmentContribution{{Value: audit}}
	return nil
}
