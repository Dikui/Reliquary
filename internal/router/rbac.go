package router

import (
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"reliquary/internal/config"
	"reliquary/internal/handler"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// rbacGuards centralizes team role and API-key permissions.
// Contributors edit team content; admins manage team settings.
type rbacGuards struct {
	cfg *config.Config

	kbService        middleware.KBLookup
	knowledgeService middleware.KnowledgeLookup
	chunkService     middleware.ChunkLookup

	// apiKeyAuthorizer is the single source of truth for which routes an
	// X-API-Key principal may call. Routes opt in via the apiKeyGroup
	// helpers below; anything not declared is denied by the gate. See
	// middleware.APIKeyRouteAuthorizer.
	apiKeyAuthorizer *middleware.APIKeyRouteAuthorizer
}

// newRBACGuards wires the guards from the live configuration and the
// already-built handlers. Called once from NewRouter.
func newRBACGuards(
	cfg *config.Config,
	kbHandler *handler.KnowledgeBaseHandler,
	agentHandler *handler.CustomAgentHandler,
	knowledgeHandler *handler.KnowledgeHandler,
	chunkHandler *handler.ChunkHandler,
	wikiHandler *handler.WikiPageHandler,
	kbService interfaces.KnowledgeBaseService,
	knowledgeService interfaces.KnowledgeService,
	chunkService interfaces.ChunkService,
) *rbacGuards {
	g := &rbacGuards{cfg: cfg, apiKeyAuthorizer: middleware.NewAPIKeyRouteAuthorizer()}
	g.kbService = kbService
	g.knowledgeService = knowledgeService
	g.chunkService = chunkService
	return g
}

// Role-only guards — pure RequireRole convenience wrappers, named after
// the matrix entries so route lines stay readable.

func (g *rbacGuards) Viewer() gin.HandlerFunc {
	return middleware.RequireRole(types.TenantRoleViewer, g.cfg)
}

func (g *rbacGuards) Contributor() gin.HandlerFunc {
	return middleware.RequireRole(types.TenantRoleContributor, g.cfg)
}

func (g *rbacGuards) Admin() gin.HandlerFunc {
	return middleware.RequireRole(types.TenantRoleAdmin, g.cfg)
}

func (g *rbacGuards) AdminOrSystemAdmin() gin.HandlerFunc {
	return middleware.RequireRoleOrSystemAdmin(types.TenantRoleAdmin, g.cfg)
}

func (g *rbacGuards) Owner() gin.HandlerFunc {
	return middleware.RequireRole(types.TenantRoleOwner, g.cfg)
}

// API-key authorization — a SEPARATE authority from the JWT role/ownership
// guards above. Instead of stacking a per-route guard that also had to know
// the caller's ownership, every API-key-accessible route declares one
// APIKeyRoutePolicy via the apiKeyGroup helpers; the gate on /api/v1 enforces
// it and denies any undeclared route by default. JWT sessions ignore all of
// this (they short-circuit the gate).
//
// Policy constructors. API keys do not reuse tenant-member roles: a key is
// either full-access, or it carries explicit capabilities. KB allow-lists are
// pure data filters applied downstream by KBAccess guards and handlers.

func apiKeyAny() middleware.APIKeyRoutePolicy {
	return middleware.APIKeyRoutePolicy{}
}

func apiKeyFullAccess() middleware.APIKeyRoutePolicy {
	return middleware.APIKeyRoutePolicy{RequireFullAccess: true}
}

// apiKeyRetrieve grants read/search access to knowledge-base data.
func apiKeyRetrieve(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityRetrieve)
}

// apiKeyChat layers the "chat" capability on top of a base policy: keys that
// carry the chat capability can use the conversation flow (sessions, agent
// listing) without full tenant access.
func apiKeyChat(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityChat)
}

// apiKeyReadAgents layers the "read_agents" capability on top of a base
// policy so scoped integrations can inspect available agents without chat or
// authoring permissions.
func apiKeyReadAgents(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityReadAgents)
}

func apiKeyIngest(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityIngest)
}

// apiKeyManageKnowledgeBases layers the "manage_kbs" capability on top of a
// base policy so a scoped key can manage the KB lifecycle (create/copy/
// duplicate/update/delete + config). Existing-KB operations stay bounded by
// the key's allow-list downstream; create has no source to bound.
func apiKeyManageKnowledgeBases(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageKnowledgeBases)
}

// apiKeyManageAgents layers the "manage_agents" capability on top of a base policy.
func apiKeyManageAgents(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageAgents)
}

// apiKeyMessageHistory layers the "message_history" capability on top of a
// base policy so an explicitly granted key can search or inspect tenant chat
// history without being promoted to full Owner.
func apiKeyMessageHistory(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityMessageHistory)
}

func apiKeyManageModels(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageModels)
}

func apiKeyManageMCPServices(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageMCPServices)
}

func apiKeyManageDataSources(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageDataSources)
}

func apiKeyManageChannels(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageChannels)
}

func apiKeyManageVectorStores(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageVectorStores)
}

// apiKeyManageStorageBackends layers the "manage_storage_backends" capability
// on top of a base policy so a scoped key can manage object/file storage
// backend instances without carrying vector-store or full tenant access.
func apiKeyManageStorageBackends(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageStorageBackends)
}

func apiKeyManageWebSearch(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageWebSearch)
}

func apiKeyRunEvaluations(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityRunEvaluations)
}

func apiKeyManageMembers(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageMembers)
}

func apiKeyManageTenantSettings(base middleware.APIKeyRoutePolicy) middleware.APIKeyRoutePolicy {
	return base.WithCapability(types.APIKeyCapabilityManageTenantSettings)
}

// apiKeyRouteGroup wraps a *gin.RouterGroup so route registration also records
// the route's API-key policy into the authorizer. Use g.apiKeyGroup(grp,
// policy) then register the API-key-accessible routes through it; register
// API-key-denied routes on the raw *gin.RouterGroup so they stay undeclared
// (default-deny). Per-route overrides use With().
type apiKeyRouteGroup struct {
	g      *rbacGuards
	grp    *gin.RouterGroup
	policy middleware.APIKeyRoutePolicy
}

// ensureAPIKeyAuthorizer lazily allocates the authorizer so route
// registration is safe even when rbacGuards is built directly in tests
// (bypassing newRBACGuards). In production it is always pre-allocated.
func (g *rbacGuards) ensureAPIKeyAuthorizer() *middleware.APIKeyRouteAuthorizer {
	if g.apiKeyAuthorizer == nil {
		g.apiKeyAuthorizer = middleware.NewAPIKeyRouteAuthorizer()
	}
	return g.apiKeyAuthorizer
}

// apiKeyGroup returns a wrapper that declares `policy` for every route
// registered through it (unless overridden via With).
func (g *rbacGuards) apiKeyGroup(grp *gin.RouterGroup, policy middleware.APIKeyRoutePolicy) *apiKeyRouteGroup {
	g.ensureAPIKeyAuthorizer()
	return &apiKeyRouteGroup{g: g, grp: grp, policy: policy}
}

// With returns a sibling wrapper on the same gin group but with a different
// policy, for the odd route that differs from its group default (e.g. a read
// search inside an otherwise contributor-gated group).
func (a *apiKeyRouteGroup) With(policy middleware.APIKeyRoutePolicy) *apiKeyRouteGroup {
	return &apiKeyRouteGroup{g: a.g, grp: a.grp, policy: policy}
}

func (a *apiKeyRouteGroup) handle(method, rel string, handlers ...gin.HandlerFunc) gin.IRoutes {
	full := path.Join(a.grp.BasePath(), rel)
	a.g.ensureAPIKeyAuthorizer().Register(method, full, a.policy)
	return a.grp.Handle(method, rel, handlers...)
}

func (a *apiKeyRouteGroup) GET(rel string, h ...gin.HandlerFunc) gin.IRoutes {
	return a.handle(http.MethodGet, rel, h...)
}

func (a *apiKeyRouteGroup) POST(rel string, h ...gin.HandlerFunc) gin.IRoutes {
	return a.handle(http.MethodPost, rel, h...)
}

func (a *apiKeyRouteGroup) PUT(rel string, h ...gin.HandlerFunc) gin.IRoutes {
	return a.handle(http.MethodPut, rel, h...)
}

func (a *apiKeyRouteGroup) PATCH(rel string, h ...gin.HandlerFunc) gin.IRoutes {
	return a.handle(http.MethodPatch, rel, h...)
}

func (a *apiKeyRouteGroup) DELETE(rel string, h ...gin.HandlerFunc) gin.IRoutes {
	return a.handle(http.MethodDelete, rel, h...)
}

// apiKeyRoute declares a single API-key-accessible route directly on a gin
// group (for routes registered outside an apiKeyGroup, e.g. top-level r.POST).
func (g *rbacGuards) apiKeyRoute(
	grp *gin.RouterGroup, method, rel string, policy middleware.APIKeyRoutePolicy, handlers ...gin.HandlerFunc,
) gin.IRoutes {
	full := path.Join(grp.BasePath(), rel)
	g.ensureAPIKeyAuthorizer().Register(method, full, policy)
	return grp.Handle(method, rel, handlers...)
}

// assertAPIKeyPoliciesMatchRoutes verifies every declared API-key policy
// resolves to a real registered route. gin's c.FullPath() must match the
// authorizer key verbatim or the gate silently 403s the route for API keys;
// panicking here turns that latent misconfiguration into a startup failure.
func (g *rbacGuards) assertAPIKeyPoliciesMatchRoutes(engine *gin.Engine) {
	registered := map[string]struct{}{}
	for _, ri := range engine.Routes() {
		// Authorizer keys are stored normalized (trailing slash trimmed), so
		// normalize gin's reported path the same way. Otherwise a route
		// registered with a "/" rel (gin path ".../evaluation/") would look
		// missing against the normalized key (".../evaluation") even though
		// the gate — which also normalizes c.FullPath() — matches it fine.
		p := ri.Path
		if len(p) > 1 {
			p = strings.TrimRight(p, "/")
		}
		registered[ri.Method+" "+p] = struct{}{}
	}
	var missing []string
	for method, paths := range g.apiKeyAuthorizer.RegisteredRoutes() {
		for _, p := range paths {
			if _, ok := registered[method+" "+p]; !ok {
				missing = append(missing, method+" "+p)
			}
		}
	}
	if len(missing) > 0 {
		panic("api-key policy declared for non-existent route(s): " + strings.Join(missing, ", "))
	}
}

func (g *rbacGuards) SystemAdmin() gin.HandlerFunc {
	return middleware.RequireSystemAdmin(g.cfg)
}

// PathTenantMatch limits team settings to the active team.
func (g *rbacGuards) PathTenantMatch() gin.HandlerFunc {
	return middleware.RequirePathTenantMatch(g.cfg)
}

// KBAccessRead requires a team viewer and verifies the knowledge base tenant.
func (g *rbacGuards) KBAccessRead(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromParam(param),
		types.TenantRoleViewer,
		g.kbService,
		g.cfg,
	)
}

// KBAccessWrite requires a team editor.
func (g *rbacGuards) KBAccessWrite(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromParam(param),
		types.TenantRoleContributor,
		g.kbService,
		g.cfg,
	)
}

// KBAccessReadFromKnowledgeIDParam is like KBAccessRead but resolves
// the kb_id by walking a knowledge document (URL `:knowledge_id`)
// back to its parent KB. Used by the chunk routes whose URL addresses
// the chunk via /chunks/:knowledge_id rather than /knowledge-bases/:id.
func (g *rbacGuards) KBAccessReadFromKnowledgeIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromKnowledgeIDParam(param, g.knowledgeService),
		types.TenantRoleViewer,
		g.kbService,
		g.cfg,
	)
}

// KBAccessWriteFromKnowledgeIDParam mirrors KBAccessReadFromKnowledgeIDParam
// for mutating routes (Editor minimum).
func (g *rbacGuards) KBAccessWriteFromKnowledgeIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromKnowledgeIDParam(param, g.knowledgeService),
		types.TenantRoleContributor,
		g.kbService,
		g.cfg,
	)
}

// KBAccessReadFromChunkIDParam walks chunk_id -> kb_id (using the
// chunk's denormalised KnowledgeBaseID column). Used by
// /chunks/by-id/:id read routes.
func (g *rbacGuards) KBAccessReadFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromChunkIDParam(param, g.chunkService),
		types.TenantRoleViewer,
		g.kbService,
		g.cfg,
	)
}

// KBAccessWriteFromChunkIDParam — same as KBAccessReadFromChunkIDParam
// but requires Editor minimum. Used by chunk write routes that
// address the chunk via /chunks/by-id/:id.
func (g *rbacGuards) KBAccessWriteFromChunkIDParam(param string) gin.HandlerFunc {
	return middleware.RequireKBAccess(
		middleware.KBIDFromChunkIDParam(param, g.chunkService),
		types.TenantRoleContributor,
		g.kbService,
		g.cfg,
	)
}
