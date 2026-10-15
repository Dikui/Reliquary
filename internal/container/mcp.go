package container

import (
	"fmt"

	"reliquary/internal/application/service"
	"reliquary/internal/mcpserver"
	"reliquary/internal/types/interfaces"
)

func newMCPHandler(entry interfaces.EntryService, search interfaces.SearchService, libraries interfaces.LibraryService,
	topics *service.TopicService, entities *service.EntityService, reports *service.VaultDocumentService, follows *service.FollowService, graph *service.GraphSearchService) (*mcpserver.Handler, error) {
	entries, ok := entry.(mcpserver.EntryService)
	if !ok {
		return nil, fmt.Errorf("entry service does not support external agent operations")
	}
	return mcpserver.New(mcpserver.Services{Libraries: libraries, Entries: entries, Search: search, Topics: topics, Entities: entities, Reports: reports, Follows: follows, Graph: graph}), nil
}
