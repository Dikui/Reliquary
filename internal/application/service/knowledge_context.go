package service

import (
	"gorm.io/gorm"
	"reliquary/internal/types/interfaces"
)

// KnowledgeContextService owns the structured, evidence-backed context kept
// alongside the existing message and chunk search indexes.
type KnowledgeContextService struct {
	db                     *gorm.DB
	models                 interfaces.ModelService
	nameEmbeddingRebuilder *ContextNameEmbeddingRebuilder
}

func NewKnowledgeContextService(db *gorm.DB, models interfaces.ModelService) *KnowledgeContextService {
	return &KnowledgeContextService{db: db, models: models}
}
