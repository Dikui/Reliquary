package service

import (
	"gorm.io/gorm"
	knowledgesearch "reliquary/internal/application/service/knowledge_search"
	"reliquary/internal/types/interfaces"
)

type knowledgeSearchService struct {
	knowledgeBaseService interfaces.KnowledgeBaseService
	spaceService         interfaces.SpaceService
	modelService         interfaces.ModelService
	rerank               *knowledgesearch.RerankStage
	db                   *gorm.DB
}

func NewKnowledgeSearchService(kbService interfaces.KnowledgeBaseService, spaceService interfaces.SpaceService, modelService interfaces.ModelService, db *gorm.DB) interfaces.KnowledgeSearchService {
	return &knowledgeSearchService{
		knowledgeBaseService: kbService, spaceService: spaceService,
		modelService: modelService, db: db,
		rerank: knowledgesearch.NewRerankStage(modelService),
	}
}
