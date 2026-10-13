package repository

import (
	"gorm.io/gorm"
	"reliquary/internal/types"
)

func libraryModelUsageBindings(library *types.Library, modelID string) []types.ModelUsageBinding {
	bindings := library.ModelTiers.UsageBindings(modelID)
	if library.EmbeddingModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingEmbeddingModel)
	}
	if library.SummaryModelID == modelID {
		bindings = append(bindings, types.ModelUsageBindingSummaryModel)
	}
	return bindings
}

// scopeLibrariesByModelID filters libraries rows that reference
// modelID in any model-binding field.
func scopeLibrariesByModelID(db *gorm.DB, modelID string) *gorm.DB {
	return db.Where(
		"embedding_model_id = ? OR summary_model_id = ? OR "+
			"model_tiers->>'light' = ? OR model_tiers->>'entity_resolution' = ? OR model_tiers->>'generation' = ? OR model_tiers->>'embedding' = ? OR model_tiers->>'rerank' = ?",
		modelID, modelID, modelID, modelID, modelID, modelID, modelID,
	)
}
