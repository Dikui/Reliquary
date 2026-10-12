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

// CopyReferences returns rewritten copies of retrieval results, covering both the
// chunk text and the structured image_info payload.
//
// Copies keep the original retrieval results unchanged.
func (w *Rewriter) CopyReferences(ctx context.Context, refs []*types.SearchResult) []*types.SearchResult {
	if !w.Enabled() || refs == nil {
		return refs
	}
	out := make([]*types.SearchResult, len(refs))
	for i, ref := range refs {
		if ref == nil {
			continue
		}
		rewritten := *ref
		rewritten.Content = w.String(ctx, ref.Content)
		rewritten.MatchedContent = w.String(ctx, ref.MatchedContent)
		rewritten.ImageInfo = w.String(ctx, ref.ImageInfo)
		out[i] = &rewritten
	}
	return out
}
