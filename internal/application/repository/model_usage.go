package repository

import (
	"gorm.io/gorm"
	"reliquary/internal/types"
)

func knowledgeBaseModelUsageBindings(kb *types.KnowledgeBase, modelID string) []types.ModelUsageBinding {
	bindings := kb.ModelTiers.UsageBindings(modelID)
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

// scopeKnowledgeBasesByModelID filters knowledge_bases rows that reference
// modelID in any model-binding field.
func scopeKnowledgeBasesByModelID(db *gorm.DB, modelID string) *gorm.DB {
	return db.Where(
		"embedding_model_id = ? OR summary_model_id = ? OR "+
			"image_processing_config->>'model_id' = ? OR "+
			"vlm_config->>'model_id' = ? OR "+
			"model_tiers->>'light' = ? OR model_tiers->>'entity_resolution' = ? OR model_tiers->>'generation' = ? OR model_tiers->>'embedding' = ? OR model_tiers->>'rerank' = ?",
		modelID, modelID, modelID, modelID, modelID, modelID, modelID, modelID, modelID,
	)
}
