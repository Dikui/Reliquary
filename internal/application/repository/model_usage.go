package repository

import (
	"gorm.io/gorm"
	"reliquary/internal/types"
)

func knowledgeBaseModelUsageBindings(kb *types.KnowledgeBase, modelID string) []types.ModelUsageBinding {
	bindings := kb.ModelTiers.UsageBindings(modelID)
	if kb.AutoTagConfig != nil && kb.AutoTagConfig.ModelID == modelID {
		bindings = append(bindings, types.ModelUsageBinding("light_model_tier"))
	}
	if kb.EmbeddingModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingEmbeddingModel)
	}
	if kb.SummaryModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingSummaryModel)
	}
	if kb.ImageProcessingConfig.ModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingImageProcessingModel)
	}
	if kb.VLMConfig.ModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingVLMModel)
	}
	return bindings
}

func customAgentModelUsageBindings(agent *types.CustomAgent, modelID string) []types.ModelUsageBinding {
	bindings := make([]types.ModelUsageBinding, 0, 6)
	if agent.Config.ModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingChatModel)
	}
	if agent.Config.RerankModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingRerankModel)
	}
	if agent.Config.VLMModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingVLMModel)
	}
	if agent.Config.QueryUnderstandModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingQueryUnderstandModel)
	}
	return bindings
}

// scopeKnowledgeBasesByModelID filters knowledge_bases rows that reference
// modelID in any model-binding field.
func scopeKnowledgeBasesByModelID(db *gorm.DB, modelID string) *gorm.DB {
	return db.Where(
		"embedding_model_id = ? OR summary_model_id = ? OR "+
			"image_processing_config->>'model_id' = ? OR "+
			"vlm_config->>'model_id' = ? OR "+
			"auto_tag_config->>'model_id' = ? OR "+
			"model_tiers->>'light' = ? OR model_tiers->>'entity_resolution' = ? OR model_tiers->>'generation' = ? OR model_tiers->>'embedding' = ? OR model_tiers->>'rerank' = ?",
		modelID, modelID, modelID, modelID, modelID, modelID, modelID, modelID, modelID, modelID,
	)
}

// scopeCustomAgentsByModelID filters custom_agents rows whose config JSON
// references modelID in any model-binding field.
func scopeCustomAgentsByModelID(db *gorm.DB, modelID string) *gorm.DB {
	return db.Where(
		"config->>'model_id' = ? OR config->>'rerank_model_id' = ? OR "+
			"config->>'vlm_model_id' = ? OR "+
			"config->>'query_understand_model_id' = ?",
		modelID, modelID, modelID, modelID,
	)
}
