package interfaces

import (
	"context"

	"reliquary/internal/types"
)

// StoreRegistry provides VectorStore-based engine registration/lookup.
// Separated from RetrieveEngineRegistry to avoid changing the existing interface
// used by 6 services (17 call sites). Phase 2 may merge into RetrieveEngineRegistry.
type StoreRegistry interface {
	// RegisterWithStoreID registers an engine service by VectorStore ID.
	// Upsert semantics: existing entry is overwritten silently.
	RegisterWithStoreID(storeID string, svc RetrieveEngineService)
	// GetByStoreID retrieves an engine service by VectorStore ID.
	GetByStoreID(storeID string) (RetrieveEngineService, error)
	// UnregisterByStoreID removes an engine service by VectorStore ID (idempotent).
	UnregisterByStoreID(storeID string)
}

// EngineFactory creates a RetrieveEngineService from a VectorStore's config.
// Defined as a function type to avoid circular imports between container and service packages.
type EngineFactory func(ctx context.Context, store types.VectorStore) (RetrieveEngineService, error)

// VectorStoreService defines the service interface for vector store management.
// Tenant isolation is enforced by the handler layer (getOwnedStore pattern).
type VectorStoreService interface {
	// TestConnection reports the shared PostgreSQL connection's status.
	TestConnection(ctx context.Context, engineType types.RetrieverEngineType, config types.ConnectionConfig) (string, error)

	// ResolveStoreView returns the API-safe display projection of a single
	// store ID, scoped to the given tenant. Tries DB stores first, then the
	// cached env-store set. Returns types.UnavailableStoreDisplay() when the
	// store cannot be resolved — callers should still succeed in such cases
	// (the response carries a "unavailable" status signal for the UI).
	//
	// Never returns connection credentials in any form: the StoreDisplay
	// payload carries only Name / Source / EngineType / Status.
	ResolveStoreView(ctx context.Context, tenantID uint64, storeID string) (types.StoreDisplay, error)

	// BatchResolveStoreView resolves multiple store IDs in a single DB read
	// plus the cached env-store match. Returned map keys are the storeIDs
	// originally requested; missing IDs map to types.UnavailableStoreDisplay().
	//
	// Intended for list endpoints that need store metadata for many KBs at
	// once without incurring N+1 ResolveStoreView calls.
	BatchResolveStoreView(ctx context.Context, tenantID uint64, storeIDs []string) (map[string]types.StoreDisplay, error)

	// EnvDefaultStoreView returns the display payload for KBs that fall
	// back to the env-configured store. Unlike DefaultStoreDisplay() in
	// the types package, this method also fills the engine type
	// (e.g. "postgres") so UIs can render the same badge shape for
	// env-bound and user-bound KBs. Independent of ResolveStoreView so
	// list paths can use it without violating the "no per-KB
	// ResolveStoreView" invariant.
	EnvDefaultStoreView(ctx context.Context) types.StoreDisplay
}

// VectorStoreRepository resolves existing knowledge-base bindings.
type VectorStoreRepository interface {
	GetByID(ctx context.Context, tenantID uint64, id string) (*types.VectorStore, error)
	List(ctx context.Context, tenantID uint64) ([]*types.VectorStore, error)
}
