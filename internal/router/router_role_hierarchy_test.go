package router

import (
	"context"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"unsafe"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"reliquary/internal/config"
	"reliquary/internal/handler"
	sessionhandler "reliquary/internal/handler/session"
	"reliquary/internal/middleware"
	"reliquary/internal/types"
)

type roleAuditLookups struct{}

func (roleAuditLookups) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, TenantID: types.SingleTenantID}, nil
}
func (roleAuditLookups) GetKnowledgeByIDOnly(_ context.Context, id string) (*types.Knowledge, error) {
	return &types.Knowledge{ID: id, TenantID: types.SingleTenantID, KnowledgeBaseID: "audit-kb"}, nil
}
func (roleAuditLookups) GetChunkByIDOnly(_ context.Context, id string) (*types.Chunk, error) {
	return &types.Chunk{ID: id, TenantID: types.SingleTenantID, KnowledgeBaseID: "audit-kb", KnowledgeID: "audit-message"}, nil
}

var roleAuditRoles = []types.TenantRole{
	types.TenantRoleViewer, types.TenantRoleContributor, types.TenantRoleAdmin, types.TenantRoleOwner,
}

var roleAuditExpectedStatus = map[types.TenantRole][4]int{
	types.TenantRoleViewer:      {http.StatusNoContent, http.StatusNoContent, http.StatusNoContent, http.StatusNoContent},
	types.TenantRoleContributor: {http.StatusForbidden, http.StatusNoContent, http.StatusNoContent, http.StatusNoContent},
	types.TenantRoleAdmin:       {http.StatusForbidden, http.StatusForbidden, http.StatusNoContent, http.StatusNoContent},
	types.TenantRoleOwner:       {http.StatusForbidden, http.StatusForbidden, http.StatusForbidden, http.StatusNoContent},
}

type roleAuditRoute struct {
	Method    string           `json:"method"`
	Path      string           `json:"path"`
	Handler   string           `json:"handler"`
	Guards    []string         `json:"guards"`
	Authority string           `json:"authority"`
	RoleFloor types.TenantRole `json:"role_floor,omitempty"`
	Status    map[string]int   `json:"status"`
	Monotonic bool             `json:"monotonic"`
}

type roleAuditRepresentative struct {
	Purpose string           `json:"purpose"`
	Method  string           `json:"method"`
	Path    string           `json:"path"`
	Floor   types.TenantRole `json:"floor"`
}

// These are product permission expectations. Paths are checked against routes
// created by the production registration functions rather than a test router.
var roleAuditRepresentatives = []roleAuditRepresentative{
	{"创建知识库", http.MethodPost, "/api/v1/knowledge-bases", types.TenantRoleContributor},
	{"编辑知识库", http.MethodPut, "/api/v1/knowledge-bases/:id", types.TenantRoleContributor},
	{"手工收录", http.MethodPost, "/api/v1/knowledge-bases/:id/knowledge/manual", types.TenantRoleContributor},
	{"URL 收录", http.MethodPost, "/api/v1/knowledge-bases/:id/knowledge/url", types.TenantRoleContributor},
	{"文件收录", http.MethodPost, "/api/v1/knowledge-bases/:id/knowledge/file", types.TenantRoleContributor},
	{"编辑分块", http.MethodPut, "/api/v1/chunks/:knowledge_id/:id", types.TenantRoleContributor},
	{"纠正对象事实", http.MethodPost, "/api/v1/knowledge-context/facts/:fact_id/correction", types.TenantRoleContributor},
	{"合并对象", http.MethodPost, "/api/v1/knowledge-context/entities/merge", types.TenantRoleContributor},
	{"创建领域", http.MethodPost, "/api/v1/knowledge-domains", types.TenantRoleAdmin},
	{"编辑领域", http.MethodPut, "/api/v1/knowledge-domains/:id", types.TenantRoleAdmin},
	{"编辑模型档位", http.MethodPut, "/api/v1/tenants/model-tiers", types.TenantRoleAdmin},
	{"运行工作流", http.MethodPost, "/api/v1/knowledge-bases/:id/workflows/:workflow_id/run", types.TenantRoleContributor},
	{"邀请成员", http.MethodPost, "/api/v1/tenants/:id/invitations", types.TenantRoleOwner},
	{"生成邀请链接", http.MethodPost, "/api/v1/tenants/:id/invite-links", types.TenantRoleOwner},
	{"创建 API key", http.MethodPost, "/api/v1/tenants/:id/api-keys", types.TenantRoleOwner},
	{"编辑 API key", http.MethodPut, "/api/v1/tenants/:id/api-keys/:key_id", types.TenantRoleOwner},
	{"删除 API key", http.MethodDelete, "/api/v1/tenants/:id/api-keys/:key_id", types.TenantRoleOwner},
	{"修改成员角色", http.MethodPut, "/api/v1/tenants/:id/members/:user_id", types.TenantRoleOwner},
	{"清空知识库内容", http.MethodDelete, "/api/v1/knowledge-bases/:id/knowledge", types.TenantRoleAdmin},
	{"预签名文件诊断", http.MethodGet, "/api/v1/files/presigned-preview", types.TenantRoleAdmin},
}

// registerRoleAuditRoutes reuses every Register* and file route registration
// called by NewRouter. The AST coverage check below fails if this list drifts.
// Authentication is the test boundary: each request supplies an already
// authenticated team member, while API-key, role, KB and tenant guards remain.
func registerRoleAuditRoutes(engine *gin.Engine, g *rbacGuards) {
	RegisterIMRoutes(engine, &handler.IMHandler{})
	serveResourceGrants(engine, nil, nil, nil, nil)
	serveFilesWithResources(engine, nil, nil, nil)
	servePresignedFiles(engine, nil, nil)
	servePresignedPreview(engine, g.cfg, nil, nil)
	v1 := engine.Group("/api/v1", g.ensureAPIKeyAuthorizer().Middleware())
	RegisterAuthRoutes(v1, &handler.AuthHandler{}, g)
	RegisterTenantRoutes(v1, &handler.TenantHandler{}, &handler.TenantMemberHandler{}, &handler.TenantInvitationHandler{}, &handler.AuditLogHandler{}, g)
	RegisterMyInvitationRoutes(v1, &handler.TenantInvitationHandler{})
	RegisterKnowledgeBaseRoutes(v1, &handler.KnowledgeBaseHandler{}, g)
	RegisterKnowledgeBaseActivityRoutes(v1, &handler.AuditLogHandler{}, g)
	serveKBScopedFiles(v1, g, nil, nil, nil, nil)
	serveMessageScopedFiles(v1, g, nil, nil, nil, nil, nil)
	RegisterKnowledgeTagRoutes(v1, &handler.TagHandler{}, g)
	RegisterKnowledgeRoutes(v1, &handler.KnowledgeHandler{}, g)
	RegisterKnowledgeAnomalyRoutes(v1, &handler.KnowledgeAnomalyHandler{}, g)
	RegisterKnowledgeStrategyRoutes(v1, &handler.KnowledgeStrategyHandler{}, g)
	RegisterKnowledgeContextRoutes(v1, &handler.KnowledgeContextHandler{}, g)
	RegisterKnowledgeDomainRoutes(v1, &handler.KnowledgeDomainHandler{}, g)
	RegisterModelRoutingRoutes(v1, &handler.ModelRoutingHandler{}, g)
	RegisterKnowledgeWorkflowRoutes(v1, &handler.KnowledgeWorkflowHandler{}, g)
	RegisterChunkRoutes(v1, &handler.ChunkHandler{}, g)
	RegisterSessionRoutes(v1, &sessionhandler.Handler{}, &handler.MessageSuggestionHandler{}, g)
	RegisterChatRoutes(v1, &sessionhandler.Handler{}, g)
	RegisterMessageRoutes(v1, &handler.MessageHandler{}, g)
	RegisterModelRoutes(v1, &handler.ModelHandler{}, &handler.ModelCredentialsHandler{}, g)
	RegisterSandboxConfigRoutes(v1, &handler.SandboxConfigHandler{}, &handler.SandboxSkillHandler{}, g)
	RegisterMyEnvVarRoutes(v1, &handler.MeEnvVarHandler{})
	RegisterEvaluationRoutes(v1, &handler.EvaluationHandler{}, g)
	RegisterInitializationRoutes(v1, &handler.InitializationHandler{}, g)
	RegisterSystemRoutes(v1, &handler.SystemHandler{}, g)
	RegisterSystemAdminRoutes(v1, &handler.SystemHandler{}, &handler.AuditLogHandler{}, g)
	RegisterMCPServiceRoutes(v1, &handler.MCPServiceHandler{}, &handler.MCPCredentialsHandler{}, &handler.MCPOAuthHandler{}, g)
	RegisterWebSearchRoutes(v1, &handler.WebSearchHandler{}, g)
	RegisterWebSearchProviderRoutes(v1, &handler.WebSearchProviderHandler{}, &handler.WebSearchProviderCredentialsHandler{}, g)
	RegisterVectorStoreRoutes(v1, &handler.VectorStoreHandler{}, g)
	RegisterStorageBackendRoutes(v1, &handler.StorageBackendHandler{}, g)
	RegisterCustomAgentRoutes(v1, &handler.CustomAgentHandler{}, g)
	RegisterUserFavoriteRoutes(v1, &handler.UserResourceFavoriteHandler{}, g)
	RegisterSkillRoutes(v1, &handler.SkillHandler{}, g)
	RegisterIMChannelRoutes(v1, &handler.IMHandler{}, g)
	RegisterDataSourceRoutes(v1, &handler.DataSourceHandler{}, &handler.DataSourceCredentialsHandler{}, g)
	RegisterWikiPageRoutes(v1, &handler.WikiPageHandler{}, g)
	RegisterMemoryRoutes(v1, &handler.MemoryHandler{}, g)
	RegisterChunkerDebugRoutes(v1, g)
	g.assertAPIKeyPoliciesMatchRoutes(engine)
}

// Gin exposes handler names but not a setter for the request's handler chain.
// Only this test helper accesses that field. It copies the chain per request
// and substitutes the final business handler, preserving every live guard.
// A Gin field/type change fails the test explicitly rather than weakening it.
func replaceRoleAuditBusinessHandler(t *testing.T, c *gin.Context) {
	t.Helper()
	field := reflect.ValueOf(c).Elem().FieldByName("handlers")
	require.True(t, field.IsValid() && field.CanAddr(), "Gin request handler chain is unavailable")
	require.Equal(t, reflect.TypeOf(gin.HandlersChain{}), field.Type())
	chain := (*gin.HandlersChain)(unsafe.Pointer(field.UnsafeAddr()))
	require.Greater(t, len(*chain), 0)
	copy := append(gin.HandlersChain(nil), (*chain)...)
	copy[len(copy)-1] = func(c *gin.Context) {
		c.Header("X-RBAC-Audit-Reached", "true")
		c.Status(http.StatusNoContent)
	}
	*chain = copy
}

func roleAuditConcretePath(template string) string {
	parts := strings.Split(template, "/")
	for i, part := range parts {
		if strings.HasPrefix(part, ":") {
			parts[i] = "1"
		} else if strings.HasPrefix(part, "*") {
			parts[i] = "sample"
		}
	}
	return strings.Join(parts, "/")
}

func TestRouterRoleHierarchyAllRegisteredRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	enabled := true
	g := &rbacGuards{cfg: &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}, kbService: roleAuditLookups{}, knowledgeService: roleAuditLookups{}, chunkService: roleAuditLookups{}}
	engine := gin.New()
	engine.Use(middleware.ErrorHandler())
	var guardNames []string
	engine.Use(func(c *gin.Context) {
		guardNames = c.HandlerNames()
		ctx := context.WithValue(c.Request.Context(), types.TenantIDContextKey, types.SingleTenantID)
		ctx = context.WithValue(ctx, types.UserIDContextKey, "role-audit-member")
		ctx = context.WithValue(ctx, types.TenantRoleContextKey, types.TenantRole(c.GetHeader("X-Test-Role")))
		ctx = context.WithValue(ctx, types.SystemAdminContextKey, c.GetHeader("X-Test-System-Admin") == "true")
		c.Request = c.Request.WithContext(ctx)
		replaceRoleAuditBusinessHandler(t, c)
		c.Next()
	})
	registerRoleAuditRoutes(engine, g)
	routes := engine.Routes()
	sort.Slice(routes, func(i, j int) bool { return routes[i].Method+" "+routes[i].Path < routes[j].Method+" "+routes[j].Path })
	inventory := make([]roleAuditRoute, 0, len(routes))
	byRoute := map[string]roleAuditRoute{}
	for _, route := range routes {
		t.Run(route.Method+" "+route.Path, func(t *testing.T) {
			record := roleAuditRoute{Method: route.Method, Path: route.Path, Handler: route.Handler, Status: map[string]int{}, Monotonic: true}
			var previousAllowed bool
			for _, role := range roleAuditRoles {
				response := httptest.NewRecorder()
				request := httptest.NewRequest(route.Method, roleAuditConcretePath(route.Path), nil)
				request.Header.Set("X-Test-Role", string(role))
				engine.ServeHTTP(response, request)
				record.Status[string(role)] = response.Code
				allowed := response.Code == http.StatusNoContent && response.Header().Get("X-RBAC-Audit-Reached") == "true"
				require.True(t, allowed || response.Code == http.StatusForbidden, "unexpected guard status role=%s body=%s", role, response.Body.String())
				if previousAllowed && !allowed {
					record.Monotonic = false
				}
				previousAllowed = allowed
				if allowed && record.RoleFloor == "" {
					record.RoleFloor = role
				}
			}
			for _, name := range guardNames {
				if strings.Contains(name, "RequireRole") || strings.Contains(name, "RequireKBAccess") || strings.Contains(name, "RequireSystemAdmin") || strings.Contains(name, "RequirePathTenantMatch") {
					record.Guards = append(record.Guards, name)
				}
			}
			require.True(t, record.Monotonic, "owner >= admin >= contributor >= viewer must hold")
			if strings.Contains(strings.Join(record.Guards, " "), "RequireSystemAdmin") {
				record.Authority = "system_admin"
				for _, role := range roleAuditRoles {
					require.Equal(t, http.StatusForbidden, record.Status[string(role)])
				}
				response := httptest.NewRecorder()
				request := httptest.NewRequest(route.Method, roleAuditConcretePath(route.Path), nil)
				request.Header.Set("X-Test-Role", string(types.TenantRoleViewer))
				request.Header.Set("X-Test-System-Admin", "true")
				engine.ServeHTTP(response, request)
				require.Equal(t, http.StatusNoContent, response.Code)
				record.Status["system_admin"] = response.Code
			} else if len(record.Guards) > 0 {
				record.Authority = "team_role"
				require.Equal(t, http.StatusNoContent, record.Status[string(types.TenantRoleOwner)], "owner must inherit every team role permission")
			} else {
				record.Authority = "authentication_or_resource_constraint"
				record.RoleFloor = ""
			}
			inventory = append(inventory, record)
			byRoute[route.Method+" "+route.Path] = record
		})
	}
	for _, representative := range roleAuditRepresentatives {
		t.Run(representative.Purpose, func(t *testing.T) {
			record, found := byRoute[representative.Method+" "+representative.Path]
			require.True(t, found, "representative must be registered by production code")
			require.Equal(t, representative.Floor, record.RoleFloor)
			for index, role := range roleAuditRoles {
				want := roleAuditExpectedStatus[representative.Floor][index]
				require.Equal(t, want, record.Status[string(role)], "role=%s", role)
			}
		})
	}
	if output := os.Getenv("RELIQUARY_RBAC_AUDIT_OUTPUT"); output != "" {
		data, err := json.MarshalIndent(struct {
			Roles           []types.TenantRole          `json:"roles"`
			ExpectedStatus  map[types.TenantRole][4]int `json:"expected_role_status"`
			Inventory       []roleAuditRoute            `json:"inventory"`
			Representatives []roleAuditRepresentative   `json:"representatives"`
		}{roleAuditRoles, roleAuditExpectedStatus, inventory, roleAuditRepresentatives}, "", "  ")
		require.NoError(t, err)
		require.NoError(t, os.MkdirAll(filepath.Dir(output), 0755))
		require.NoError(t, os.WriteFile(output, append(data, '\n'), 0644))
	}
}

func TestRouterRoleAuditUsesEveryProductionRegistration(t *testing.T) {
	collect := func(path, name string) []string {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		require.NoError(t, err)
		var calls []string
		for _, decl := range file.Decls {
			function, ok := decl.(*ast.FuncDecl)
			if !ok || function.Name.Name != name {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				identifier, ok := call.Fun.(*ast.Ident)
				if ok && (strings.HasPrefix(identifier.Name, "Register") || strings.HasPrefix(identifier.Name, "serve")) {
					calls = append(calls, identifier.Name)
				}
				return true
			})
		}
		require.NotEmpty(t, calls)
		sort.Strings(calls)
		return calls
	}
	require.Equal(t, collect("router.go", "NewRouter"), collect("router_role_hierarchy_test.go", "registerRoleAuditRoutes"))

	// Also compare the final live route templates. This catches direct routes
	// and conditional registration changes that a function-name list misses.
	enabled := true
	cfg := &config.Config{Tenant: &config.TenantConfig{EnableRBAC: &enabled}}
	params := RouterParams{Config: cfg}
	fields := reflect.ValueOf(&params).Elem()
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		if field.Kind() == reflect.Pointer && strings.HasPrefix(field.Type().Elem().PkgPath(), "reliquary/internal/handler") {
			field.Set(reflect.New(field.Type().Elem()))
		}
	}
	production := NewRouter(params)
	audit := gin.New()
	registerRoleAuditRoutes(audit, &rbacGuards{cfg: cfg})
	routeKeys := func(engine *gin.Engine) []string {
		var keys []string
		for _, route := range engine.Routes() {
			if route.Path == "/health" || strings.HasPrefix(route.Path, "/swagger/") {
				continue
			}
			keys = append(keys, route.Method+" "+route.Path)
		}
		sort.Strings(keys)
		return keys
	}
	require.Equal(t, routeKeys(production), routeKeys(audit), "RBAC inventory must cover the actual NewRouter route set")
}
