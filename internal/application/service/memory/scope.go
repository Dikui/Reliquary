// Package memory implements cross-session long-term memory: what the system
// remembers about one principal inside one workspace, independently of any
// single chat session.
package memory

import (
	"context"
	"errors"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// ErrNoMemoryScope means the request carries no principal we can attribute
// memory to. Callers on the read path treat it as "no memory"; callers on the
// API path turn it into an error, because a memory manager with no owner is a
// bug rather than an empty state.
var ErrNoMemoryScope = errors.New("memory: no principal in context")

func ResolveScope(ctx context.Context) (interfaces.MemoryScope, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return interfaces.MemoryScope{}, ErrNoMemoryScope
	}
	principal, ok := types.PrincipalFromContext(ctx)
	if !ok {
		return interfaces.MemoryScope{}, ErrNoMemoryScope
	}
	subjectID := principal.StorageID()
	if subjectID == "" {
		return interfaces.MemoryScope{}, ErrNoMemoryScope
	}
	return interfaces.MemoryScope{TenantID: tenantID, SubjectID: subjectID}, nil
}
