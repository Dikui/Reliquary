package searchutil

import (
	"context"
	"errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// ResolveContextScope preserves the same union of authorized targets as RAG.
// A resolved document whitelist already includes its tag intersection.
func ResolveContextScope(ctx context.Context, targets types.SearchTargets, kbID string, knowledge interfaces.KnowledgeService) (context.Context, []string, error) {
	found, whole := false, false
	ids := map[string]bool{}
	for _, target := range targets {
		if target == nil || target.KnowledgeBaseID != kbID {
			continue
		}
		found = true
		if target.TenantID != 0 {
			ctx = context.WithValue(ctx, types.TenantIDContextKey, target.TenantID)
		}
		tags := append(append([]string{}, target.TagIDs...), target.ScopeTagIDs...)
		if target.Type == types.SearchTargetTypeKnowledgeBase && len(target.KnowledgeIDs) == 0 && len(tags) == 0 {
			whole = true
			continue
		}
		if len(target.KnowledgeIDs) > 0 {
			for _, id := range target.KnowledgeIDs {
				if id != "" {
					ids[id] = true
				}
			}
			continue
		}
		if len(tags) > 0 {
			if knowledge == nil {
				return ctx, nil, errors.New("tag-scoped knowledge service unavailable")
			}
			resolved, err := knowledge.ListKnowledgeIDsByTagIDs(ctx, types.MustTenantIDFromContext(ctx), kbID, tags)
			if err != nil {
				return ctx, nil, err
			}
			for _, id := range resolved {
				ids[id] = true
			}
		}
	}
	if !found {
		return ctx, nil, errors.New("knowledge base outside authorized search targets")
	}
	if whole {
		return ctx, nil, nil
	}
	selected := make([]string, 0, len(ids))
	for id := range ids {
		selected = append(selected, id)
	}
	// IDs are UUIDs; this non-ID expresses an empty scope without accidentally
	// turning a tag with no documents into an unrestricted query.
	if len(selected) == 0 {
		selected = []string{"no-authorized-knowledge"}
	}
	return ctx, selected, nil
}
