package storageurl

import (
	"context"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func NewRequestRewriter(
	ctx context.Context,
	mode Mode,
	defaultSvc interfaces.FileService,
) *Rewriter {
	if mode != ModePublic {
		return NewRewriter(nil, "API")
	}
	space, _ := types.SpaceInfoFromContext(ctx)
	resolver := NewFileServiceResolver(space, defaultSvc).WithContext(ctx)
	return NewRewriter(resolver, "API")
}
