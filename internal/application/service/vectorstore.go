package service

import (
	"context"

	"gorm.io/gorm"

	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	secutils "reliquary/internal/utils"
)

// vectorStoreService implements the read-only VectorStoreService projection.
type vectorStoreService struct {
	repo      interfaces.VectorStoreRepository
	db        *gorm.DB
	envStores []types.VectorStore
}

// NewVectorStoreService exposes the application PostgreSQL store as read-only.
func NewVectorStoreService(repo interfaces.VectorStoreRepository, _ interfaces.KnowledgeBaseRepository,
	_ interfaces.StoreRegistry, _ interfaces.EngineFactory, db *gorm.DB,
) interfaces.VectorStoreService {
	return &vectorStoreService{repo: repo, db: db, envStores: types.BuildEnvVectorStores("", nil)}
}

// TestConnection reports the shared PostgreSQL connection's health and version.
func (s *vectorStoreService) TestConnection(ctx context.Context, _ types.RetrieverEngineType, _ types.ConnectionConfig) (string, error) {
	if s.db == nil {
		return "", gorm.ErrInvalidDB
	}
	db, err := s.db.DB()
	if err != nil {
		return "", err
	}
	if err := db.PingContext(ctx); err != nil {
		return "", err
	}
	var version string
	if err := s.db.WithContext(ctx).Raw("SELECT version()").Scan(&version).Error; err != nil {
		return "", err
	}
	return version, nil
}

// ResolveStoreView returns the API-safe display projection of a single
// store ID for embedding in another resource's response (typically a KB).
//
// Resolution order:
//
//  1. storeID == "" to DefaultStoreDisplay (env fallback semantics).
//  2. DB store row matching (id, tenantID) to user-source display.
//  3. Cached env store with matching ID to env-source display.
//  4. Otherwise to UnavailableStoreDisplay with a structured warn log.
//
// Errors from the underlying repository are returned to the caller so
// transient infrastructure failures can be classified, but the returned
// StoreDisplay is still UnavailableStoreDisplay so a handler that ignores
// the error degrades gracefully rather than panicking on a zero value.
// EnvDefaultStoreView is the env-fallback display, enriched with the
// active env store's engine type when one is configured. Exposed
// separately from ResolveStoreView so list paths can fill the
// env-bound entries without invoking the single-KB resolver.
func (s *vectorStoreService) EnvDefaultStoreView(_ context.Context) types.StoreDisplay {
	return s.defaultStoreDisplay()
}

func (s *vectorStoreService) ResolveStoreView(
	ctx context.Context, tenantID uint64, storeID string,
) (types.StoreDisplay, error) {
	if storeID == "" {
		return s.defaultStoreDisplay(), nil
	}
	store, err := s.repo.GetByID(ctx, tenantID, storeID)
	if err != nil {
		return types.UnavailableStoreDisplay(), err
	}
	if store != nil {
		return types.StoreDisplay{
			Name:       store.Name,
			Source:     types.StoreSourceUser,
			EngineType: string(store.EngineType),
			Status:     "available",
		}, nil
	}
	for _, env := range s.envStores {
		if env.ID == storeID {
			return types.StoreDisplay{
				Name:       env.Name,
				Source:     types.StoreSourceEnv,
				EngineType: string(env.EngineType),
				Status:     "available",
			}, nil
		}
	}
	logger.WarnWithFields(ctx, logger.Fields{
		"tenant_id": tenantID,
		"store_id":  secutils.SanitizeForLog(storeID),
	}, "[vectorstore.resolve] bound store missing from DB and env set")
	return types.UnavailableStoreDisplay(), nil
}

// BatchResolveStoreView resolves multiple store IDs in a single DB read
// plus the cached env-store match. Returned map keys are the storeIDs
// originally requested; missing IDs map to UnavailableStoreDisplay.
//
// Intended for list endpoints that need store metadata for many KBs at
// once without incurring N+1 ResolveStoreView calls.
//
// Implementation note: the tenant-store count is bounded by operator
// config (typically tens), so iterating the tenant's full store list
// once is cheaper than a SELECT … WHERE id IN (…) round-trip and avoids
// adding a batch-by-ids repository method that has no other caller.
func (s *vectorStoreService) BatchResolveStoreView(
	ctx context.Context, tenantID uint64, storeIDs []string,
) (map[string]types.StoreDisplay, error) {
	out := make(map[string]types.StoreDisplay, len(storeIDs))
	if len(storeIDs) == 0 {
		return out, nil
	}

	requested := make(map[string]bool, len(storeIDs))
	hasNonEmpty := false
	for _, id := range storeIDs {
		if id == "" {
			continue
		}
		requested[id] = true
		hasNonEmpty = true
	}

	if hasNonEmpty {
		dbStores, err := s.repo.List(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, st := range dbStores {
			if requested[st.ID] {
				out[st.ID] = types.StoreDisplay{
					Name:       st.Name,
					Source:     types.StoreSourceUser,
					EngineType: string(st.EngineType),
					Status:     "available",
				}
			}
		}
		for _, env := range s.envStores {
			if _, ok := out[env.ID]; ok {
				continue
			}
			if requested[env.ID] {
				out[env.ID] = types.StoreDisplay{
					Name:       env.Name,
					Source:     types.StoreSourceEnv,
					EngineType: string(env.EngineType),
					Status:     "available",
				}
			}
		}
	}

	// Fill misses (including empty-string entries) with the appropriate
	// sentinel so callers can rely on a key for every requested ID.
	for _, id := range storeIDs {
		if id == "" {
			out[id] = s.defaultStoreDisplay()
			continue
		}
		if _, ok := out[id]; !ok {
			out[id] = types.UnavailableStoreDisplay()
		}
	}
	return out, nil
}

// defaultStoreDisplay returns the env-fallback display, enriched with the
// active env store's engine type when one is configured. Callers receive a
// fully populated StoreDisplay so UIs can render the same badge shape for
// env-bound and user-bound KBs (e.g. "postgres") without
// branching on Source.
func (s *vectorStoreService) defaultStoreDisplay() types.StoreDisplay {
	d := types.DefaultStoreDisplay()
	if len(s.envStores) > 0 {
		d.EngineType = string(s.envStores[0].EngineType)
	}
	return d
}
