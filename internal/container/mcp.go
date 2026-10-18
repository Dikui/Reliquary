package container

import (
	"fmt"

	"reliquary/internal/application/service"
	"reliquary/internal/diagnostics"
	"reliquary/internal/mcpserver"
	"reliquary/internal/types/interfaces"
)

func newMCPHandler(entry interfaces.EntryService, search interfaces.SearchService, libraries interfaces.LibraryService,
	topics *service.TopicService, entities *service.EntityService, follows *service.FollowService, graph *service.GraphSearchService, diagnostic *diagnostics.Service) (*mcpserver.Handler, error) {
	entries, ok := entry.(mcpserver.EntryService)
	if !ok {
		return nil, fmt.Errorf("entry service does not support external agent operations")
	}
	return mcpserver.New(mcpserver.Services{Libraries: libraries, Entries: entries, Search: search, Topics: topics, Entities: entities, Follows: follows, Graph: graph, Diagnostics: diagnostic}), nil
}
