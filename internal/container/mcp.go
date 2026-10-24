package container

import (
	"fmt"
	"reliquary/internal/application/service"
	"reliquary/internal/diagnostics"
	"reliquary/internal/mcpserver"
	"reliquary/internal/types/interfaces"
)

func newMCPHandler(
	entry interfaces.EntryService,
	search interfaces.SearchService,
	libraries interfaces.LibraryService,
	entities *service.EntityService,
	follows *service.FollowService,
	diagnostic *diagnostics.Service,
) (*mcpserver.Handler, error) {
	entries, ok := entry.(mcpserver.EntryService)
	if !ok {
		return nil, fmt.Errorf("entry service does not support external agent operations")
	}
	return mcpserver.New(
		mcpserver.Services{
			Libraries:   libraries,
			Entries:     entries,
			Search:      search,
			Entities:    entities,
			Follows:     follows,
			Diagnostics: diagnostic,
		},
	), nil
}
