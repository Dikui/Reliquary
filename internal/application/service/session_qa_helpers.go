package service

import (
	"context"
	"fmt"
	"reliquary/internal/agent/tools"
	"strings"

	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// ---------------------------------------------------------------------------
// Shared QA helpers: KB resolution, model resolution, retrieval tenant
// ---------------------------------------------------------------------------

// resolveKnowledgeBases resolves the effective knowledge base IDs and knowledge IDs
// for a QA request. Priority:
//  1. Explicit @mentions (request-specified kbIDs / knowledgeIDs)
//  2. RetrieveKBOnlyWhenMentioned -> disable KB if no mention
//  3. Agent's configured knowledge bases (via KBSelectionMode)
func (s *sessionService) resolveKnowledgeBases(
	ctx context.Context,
	req *types.QARequest,
) (kbIDs []string, knowledgeIDs []string, err error) {
	kbIDs = req.KnowledgeBaseIDs
	knowledgeIDs = req.KnowledgeIDs
	customAgent := req.CustomAgent

	hasExplicitMention := len(kbIDs) > 0 || len(knowledgeIDs) > 0 || len(req.TagScopes) > 0
	if customAgent != nil {
		logger.Infof(ctx, "KB resolution: hasExplicitMention=%v, RetrieveKBOnlyWhenMentioned=%v, KBSelectionMode=%s",
			hasExplicitMention, customAgent.Config.RetrieveKBOnlyWhenMentioned, customAgent.Config.KBSelectionMode)
	}

	if hasExplicitMention {
		logger.Infof(ctx, "Using request-specified targets: kbs=%v, docs=%v", kbIDs, knowledgeIDs)

	} else if customAgent != nil && customAgent.Config.RetrieveKBOnlyWhenMentioned {
		kbIDs = nil
		knowledgeIDs = nil
		logger.Infof(ctx, "RetrieveKBOnlyWhenMentioned is enabled and no @ mention found, KB retrieval disabled for this request")
	} else if customAgent != nil {
		kbIDs = s.resolveKnowledgeBasesFromAgent(ctx, customAgent, req.Session.TenantID)
	}

	return kbIDs, knowledgeIDs, nil
}

// resolveChatModelID resolves the effective chat model ID for a QA request.
//
// When a user-configured agent is selected, its model configuration must be
// complete and valid. A request-level override may choose another valid model,
// while the stored agent configuration must remain valid.
//
// Without an agent, the legacy KB / session / system fallback remains
// available for non-agent callers.
func (s *sessionService) resolveChatModelID(
	ctx context.Context,
	req *types.QARequest,
	knowledgeBaseIDs []string,
	knowledgeIDs []string,
) (string, error) {
	summaryModelID := req.SummaryModelID
	customAgent := req.CustomAgent
	session := req.Session
	configuredAgentModelID := ""

	if customAgent != nil {
		configuredAgentModelID = strings.TrimSpace(customAgent.Config.ModelID)
		if configuredAgentModelID == "" {
			return "", fmt.Errorf("chat model is not configured: please set model_id on agent %s", customAgent.ID)
		}
		if configuredAgentModelID != "" {
			model, err := s.modelService.GetModelByID(ctx, configuredAgentModelID)
			if err != nil || model == nil || model.Type != types.ModelTypeKnowledgeQA {
				return "", fmt.Errorf("configured chat model %s is unavailable for agent %s", configuredAgentModelID, customAgent.ID)
			}
		}

	}

	summaryModelID = strings.TrimSpace(summaryModelID)
	if summaryModelID != "" {
		if model, err := s.modelService.GetModelByID(ctx, summaryModelID); err == nil && model != nil &&
			model.Type == types.ModelTypeKnowledgeQA {
			logger.Infof(ctx, "Using request's summary model override: %s", summaryModelID)
			return summaryModelID, nil
		}
		logger.Warnf(ctx, "Request provided invalid summary model ID %s, falling back", summaryModelID)
	}
	if configuredAgentModelID != "" {
		logger.Infof(ctx, "Using custom agent's model_id: %s", configuredAgentModelID)
		return configuredAgentModelID, nil
	}
	return s.selectChatModelID(ctx, session, knowledgeBaseIDs, knowledgeIDs)
}

// resolveRetrievalTenantID always selects the single team for QA dependencies.
func (s *sessionService) resolveRetrievalTenantID(ctx context.Context, req *types.QARequest) uint64 {
	return types.SingleTenantID
}

func (s *sessionService) applyAgentOverridesToChatManage(
	ctx context.Context,
	customAgent *types.CustomAgent,
	cm *types.ChatManage,
) {
	if customAgent == nil {
		return
	}

	// Ensure defaults are set
	customAgent.EnsureDefaults()

	// Override summary config fields
	if customAgent.Config.SystemPrompt != "" {
		cm.SummaryConfig.Prompt = customAgent.Config.SystemPrompt
		logger.Infof(ctx, "Using custom agent's system_prompt")
	}
	if customAgent.Config.ContextTemplate != "" {
		cm.SummaryConfig.ContextTemplate = customAgent.Config.ContextTemplate
		logger.Infof(ctx, "Using custom agent's context_template")
	}
	if customAgent.Config.Temperature >= 0 {
		cm.SummaryConfig.Temperature = customAgent.Config.Temperature
		logger.Infof(ctx, "Using custom agent's temperature: %f", customAgent.Config.Temperature)
	}
	if customAgent.Config.MaxCompletionTokens > 0 {
		cm.SummaryConfig.MaxCompletionTokens = customAgent.Config.MaxCompletionTokens
		logger.Infof(ctx, "Using custom agent's max_completion_tokens: %d", customAgent.Config.MaxCompletionTokens)
	}
	// Agent-level thinking setting takes full control (no global fallback).
	// EnsureDefaults pins nil to explicit false so thinking_control wire formats
	// always receive a value.
	cm.SummaryConfig.Thinking = customAgent.Config.Thinking
	cm.CitationEnabled = customAgent.Config.CitationEnabled
	if customAgent.Config.Thinking != nil {
		logger.Infof(ctx, "Using custom agent's thinking: %v", *customAgent.Config.Thinking)
	} else {
		logger.Warnf(ctx, "Custom agent thinking is unset after EnsureDefaults; model thinking param will be omitted")
	}

	// Override retrieval strategy settings
	if customAgent.Config.EmbeddingTopK > 0 {
		cm.EmbeddingTopK = customAgent.Config.EmbeddingTopK
	}
	if customAgent.Config.KeywordThreshold > 0 {
		cm.KeywordThreshold = customAgent.Config.KeywordThreshold
	}
	if customAgent.Config.VectorThreshold > 0 {
		cm.VectorThreshold = customAgent.Config.VectorThreshold
	}
	if customAgent.Config.RerankTopK > 0 {
		cm.RerankTopK = customAgent.Config.RerankTopK
	}
	cm.RerankThreshold = customAgent.Config.RerankThreshold
	if customAgent.Config.RerankModelID != "" {
		cm.RerankModelID = customAgent.Config.RerankModelID
	}

	// Override rewrite settings
	cm.EnableRewrite = customAgent.Config.EnableRewrite
	cm.EnableQueryExpansion = customAgent.Config.EnableQueryExpansion
	if customAgent.Config.RewritePromptSystem != "" {
		cm.RewritePromptSystem = customAgent.Config.RewritePromptSystem
	}
	if customAgent.Config.RewritePromptUser != "" {
		cm.RewritePromptUser = customAgent.Config.RewritePromptUser
	}
	if customAgent.Config.QueryUnderstandModelID != "" {
		cm.QueryUnderstandModelID = customAgent.Config.QueryUnderstandModelID
		logger.Infof(ctx, "Using custom agent's query_understand_model_id: %s",
			customAgent.Config.QueryUnderstandModelID)
	}

	// Override fallback settings
	if customAgent.Config.FallbackStrategy != "" {
		cm.FallbackStrategy = types.FallbackStrategy(customAgent.Config.FallbackStrategy)
	}
	if customAgent.Config.FallbackResponse != "" {
		cm.FallbackResponse = customAgent.Config.FallbackResponse
	}
	if customAgent.Config.FallbackPrompt != "" {
		cm.FallbackPrompt = customAgent.Config.FallbackPrompt
	}

	// Override history turns
	if customAgent.Config.HistoryTurns > 0 {
		cm.MaxRounds = customAgent.Config.HistoryTurns
		logger.Infof(ctx, "Using custom agent's history_turns: %d", cm.MaxRounds)
	}
	if !customAgent.Config.MultiTurnEnabled {
		cm.MaxRounds = 0
		logger.Infof(ctx, "Multi-turn disabled by custom agent, clearing history")
	}

	// Data-analysis pipeline stage (opt-in, default off).
	cm.DataAnalysisEnabled = customAgent.Config.DataAnalysisEnabled
	if cm.DataAnalysisEnabled {
		logger.Infof(ctx, "Data analysis pipeline stage enabled by custom agent")
	}

	if len(customAgent.Config.IntentPrompts) > 0 {
		cm.IntentPromptOverrides = customAgent.Config.IntentPrompts
		logger.Infof(ctx, "Using custom agent's intent_prompts (%d overrides)", len(cm.IntentPromptOverrides))
	}
}

func agentRequiresRerankModel(agent *types.CustomAgent) bool {
	if agent == nil {
		return false
	}
	if agent.Config.KBSelectionMode == "none" {
		return false
	}
	allowed := agent.Config.AllowedTools
	if len(allowed) == 0 {
		allowed = tools.DefaultAllowedTools()
	}
	for _, t := range allowed {
		if t == tools.ToolKnowledgeSearch {
			return true
		}
	}
	return false
}

func (s *sessionService) resolveScopedRerankTier(ctx context.Context, kbIDs []string) (string, error) {
	var kb *types.KnowledgeBase
	if len(kbIDs) > 0 {
		var err error
		kb, err = s.knowledgeBaseService.GetKnowledgeBaseByID(ctx, kbIDs[0])
		if err != nil {
			return "", err
		}
	}
	return ResolveModelTier(ctx, s.db, kb, types.ModelTierRerank)
}
