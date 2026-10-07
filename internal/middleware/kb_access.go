package middleware

import (
	"context"
	stderrors "errors"

	"github.com/gin-gonic/gin"
	apprepo "reliquary/internal/application/repository"
	"reliquary/internal/config"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/types"
)

// Knowledge-base access stays within the authenticated team.

// KBAccess captures the result of a successful KB access resolution.
// Stashed on gin.Context under KBAccessContextKey so handlers that
// need the resolved KB / permission (e.g. to render
// my_permission in the response) can pull it without re-running the
// resolution.
type KBAccess struct {
	KnowledgeBase     *types.KnowledgeBase
	EffectiveTenantID uint64
	Permission        types.TenantRole
}

// KBAccessContextKey is the gin.Context key under which a successful
// KB access resolution is stored.
const KBAccessContextKey = "rbac.kb_access"

// KBAccessFromContext returns the KBAccess stashed by the guard, if
// any. Handlers that don't care can just rely on the rewritten
// c.Request.Context() for tenant scoping.
func KBAccessFromContext(c *gin.Context) (*KBAccess, bool) {
	v, ok := c.Get(KBAccessContextKey)
	if !ok {
		return nil, false
	}
	a, ok := v.(*KBAccess)
	return a, ok
}

// KBLookup is the minimum surface ResolveKBAccess needs from the
// knowledge-base service: a single method that turns an ID into a
// KnowledgeBase pointer (or repo.ErrKnowledgeBaseNotFound). Defining
// it as a tiny dedicated interface keeps the guard testable without
// forcing test stubs to satisfy the full KnowledgeBaseService surface.
type KBLookup interface {
	GetKnowledgeBaseByID(ctx context.Context, id string) (*types.KnowledgeBase, error)
}

// KnowledgeLookup mirrors KBLookup but for resolving a knowledge id
// (document id) back to its parent KB. Used by the chunk routes whose
// URL param is a knowledge_id, not a kb_id.
type KnowledgeLookup interface {
	GetKnowledgeByIDOnly(ctx context.Context, id string) (*types.Knowledge, error)
}

// ChunkLookup mirrors KBLookup for resolving a chunk id back to its
// owning knowledge document, which then resolves to the parent KB.
// Used by the /chunks/by-id/:id routes that address chunks directly.
type ChunkLookup interface {
	GetChunkByIDOnly(ctx context.Context, id string) (*types.Chunk, error)
}

// KBIDResolver tells the guard how to find the kb_id for a given
// request. Built-in resolvers below cover the param shapes we use:
// :id, :kb_id, :kbId, :knowledge_id (-> parent KB).
//
// On error, resolvers MUST return either a 4xx apperror (bad request /
// not found) or a generic Go error for transient/internal failures;
// the guard maps the latter to 503.
type KBIDResolver func(c *gin.Context) (string, error)

// KBIDFromParam returns a resolver that reads a fixed gin param.
func KBIDFromParam(param string) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		return v, nil
	}
}

// KBIDFromKnowledgeIDParam reads `:knowledge_id` from the URL, looks
// up the knowledge document, and returns its KB id. Used by the chunk
// routes that address a chunk via /chunks/:knowledge_id.
//
// A genuine "not found" maps to 404; transient errors (DB hiccup,
// service unavailable) are surfaced as a plain Go error so the guard
// can return 503 instead of pretending the resource doesn't exist
// (a 404 here would also short-circuit any retry / monitoring).
func KBIDFromKnowledgeIDParam(param string, kgService KnowledgeLookup) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		k, err := kgService.GetKnowledgeByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Knowledge not found")
			}
			return "", err
		}
		if k == nil {
			return "", apperrors.NewNotFoundError("Knowledge not found")
		}
		return k.KnowledgeBaseID, nil
	}
}

// KBIDFromChunkIDParam walks chunk_id -> knowledge_id -> kb_id.
// Used by /chunks/by-id/:id routes that address a chunk directly. The
// chunk's KnowledgeBaseID is denormalised on the row, so a single
// lookup is enough — no need to chain through GetKnowledgeByIDOnly.
//
// Not-found / transient split mirrors KBIDFromKnowledgeIDParam.
func KBIDFromChunkIDParam(param string, chunkService ChunkLookup) KBIDResolver {
	return func(c *gin.Context) (string, error) {
		v := c.Param(param)
		if v == "" {
			return "", apperrors.NewBadRequestError("missing " + param + " in path")
		}
		ch, err := chunkService.GetChunkByIDOnly(c.Request.Context(), v)
		if err != nil {
			if isResourceNotFound(err) {
				return "", apperrors.NewNotFoundError("Chunk not found")
			}
			return "", err
		}
		if ch == nil {
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		if ch.KnowledgeBaseID == "" {
			// Should-never-happen on a fresh schema; on legacy rows the
			// chunk effectively isn't resolvable to a KB so the client
			// gets the same 404 they'd get for a missing chunk rather
			// than a 500 that pollutes alerting.
			logger.Warnf(c.Request.Context(),
				"[kb_access] chunk %s has empty knowledge_base_id; treating as not-found", v)
			return "", apperrors.NewNotFoundError("Chunk not found")
		}
		return ch.KnowledgeBaseID, nil
	}
}

// isResourceNotFound recognises the various "not found" sentinels we
// might see from the underlying services. Keeps the resolvers above
// from forcing every service to standardise on a single error type
// before this refactor is useful.
func isResourceNotFound(err error) bool {
	// ErrChunkNotFound is defined in the repository layer and aliased by the
	// service; match the canonical repo sentinel so this predicate depends
	// only on the repository package (KB / Knowledge / Chunk are all here).
	return stderrors.Is(err, apprepo.ErrKnowledgeBaseNotFound) ||
		stderrors.Is(err, apprepo.ErrKnowledgeNotFound) ||
		stderrors.Is(err, apprepo.ErrChunkNotFound) ||
		stderrors.Is(err, ErrResourceNotFound)
}

func RequireKBAccess(
	resolveKBID KBIDResolver,
	requiredPermission types.TenantRole,
	kbService KBLookup,
	cfg *config.Config,
) gin.HandlerFunc {
	warnOnNilConfig(cfg)
	return func(c *gin.Context) {
		kbID, err := resolveKBID(c)
		if err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		ctx := c.Request.Context()
		if err := types.AuthorizeTenantAPIKeyKnowledgeBases(ctx, kbID); err != nil {
			_ = c.Error(err)
			c.Abort()
			return
		}

		// Rollout window: enforcement off -> log the would-be check and
		// pass through. We still resolve the KB (best-effort) so the
		// effective-tenant context rewrite still happens for shared
		// KBs; that way embedding queries hit the right tenant
		// regardless of whether RBAC enforcement is active.
		enforcing := rbacEnforcementEnabled(cfg)

		access, err := resolveKBAccessOnce(ctx, c, kbID, requiredPermission, kbService)
		switch {
		case stderrors.Is(err, errKBAccessUnauthorized):
			if !enforcing {
				logger.Warnf(ctx, "[rbac] kb-access would 401 (enforcement off): kb=%s", kbID)
				c.Next()
				return
			}
			_ = c.Error(apperrors.NewUnauthorizedError("Unauthorized"))
			c.Abort()
			return
		case stderrors.Is(err, errKBAccessNotFound):
			// 404 still fires when enforcement is off — a missing KB is
			// not an authorisation event, the client genuinely asked
			// for nothing.
			_ = c.Error(apperrors.NewNotFoundError("knowledge base not found"))
			c.Abort()
			return
		case stderrors.Is(err, errKBAccessForbidden):
			if !enforcing {
				logger.Warnf(ctx, "[rbac] kb-access would 403 (enforcement off): kb=%s required=%s",
					kbID, requiredPermission)
				c.Next()
				return
			}
			_ = c.Error(apperrors.NewForbiddenError("Permission denied to access this knowledge base"))
			c.Abort()
			return
		case err != nil:
			logger.ErrorWithFields(ctx, err, nil)
			// Transient/internal -> 503 so monitoring catches the
			// underlying failure rather than a misleading 500.
			_ = c.Error(apperrors.NewServiceUnavailableError("cannot verify KB access right now"))
			c.Abort()
			return
		}

		c.Set(KBAccessContextKey, access)
		c.Next()
	}
}

func resolveKBAccessOnce(ctx context.Context, c *gin.Context, kbID string, requiredPermission types.TenantRole, kbService KBLookup) (*KBAccess, error) {
	tenantID, ok := types.TenantIDFromContext(ctx)
	if !ok || tenantID == 0 {
		return nil, errKBAccessUnauthorized
	}
	kb, err := kbService.GetKnowledgeBaseByID(ctx, kbID)
	if err != nil {
		if stderrors.Is(err, apprepo.ErrKnowledgeBaseNotFound) {
			return nil, errKBAccessNotFound
		}
		return nil, err
	}
	if kb == nil {
		return nil, errKBAccessNotFound
	}
	if kb.TenantID != tenantID {
		return nil, errKBAccessForbidden
	}
	role := types.TenantRoleFromContext(ctx)
	if _, apiKey := types.TenantAPIKeyScopeFromContext(ctx); !apiKey && !role.HasPermission(requiredPermission) {
		return nil, errKBAccessForbidden
	}
	return &KBAccess{KnowledgeBase: kb, EffectiveTenantID: tenantID, Permission: role}, nil
}

var (
	errKBAccessUnauthorized = stderrors.New("kb_access: unauthorized")
	errKBAccessNotFound     = stderrors.New("kb_access: not found")
	errKBAccessForbidden    = stderrors.New("kb_access: forbidden")
)
