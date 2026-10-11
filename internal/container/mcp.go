package container

import (
	"fmt"

	"reliquary/internal/application/service"
	"reliquary/internal/mcpserver"
	"reliquary/internal/types/interfaces"
)

func newMCPHandler(knowledge interfaces.KnowledgeService, search interfaces.KnowledgeSearchService,
	topics *service.TopicService, entities *service.EntityService, reports *service.VaultDocumentService, follows *service.FollowService) (*mcpserver.Handler, error) {
	articles, ok := knowledge.(mcpserver.ArticleService)
	if !ok {
		return nil, fmt.Errorf("knowledge service does not support external agent operations")
	}
	return mcpserver.New(mcpserver.Services{Articles: articles, Search: search, Topics: topics, Entities: entities, Reports: reports, Follows: follows}), nil
}
