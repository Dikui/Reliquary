package router

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
)

func RegisterModelRoutes(
	r *gin.RouterGroup,
	handler *handler.ModelHandler,
	credHandler *handler.ModelCredentialsHandler,
	g *accessGuards) {
	// 模型路由组，登录用户和 API Key 均可访问。
	models := r.Group("/models")
	{
		models.GET("/providers", handler.ListModelProviders)
		// 创建模型
		models.POST("", handler.CreateModel)
		models.GET("", handler.ListModels)
		// 调试已保存模型会发起真实上游调用并产生费用
		models.POST("/:id/debug", handler.DebugModel)
		models.GET("/:id", handler.GetModel)
		models.PUT("/:id", handler.UpdateModel)
		// 删除模型
		models.DELETE("/:id", handler.DeleteModel)
		// Per-field credential subresource (see internal/handler/model_credentials.go)
		models.PUT("/:id/credentials", credHandler.Put)
		models.DELETE("/:id/credentials/:field", credHandler.DeleteField)
	}
}

// Sandbox configs are workspace infrastructure that hold provider credentials.
func RegisterSandboxConfigRoutes(
	r *gin.RouterGroup,
	h *handler.SandboxConfigHandler,
	skills *handler.SandboxSkillHandler,
	g *accessGuards) {
	configs := r.Group("/sandbox-configs")
	{
		configs.GET("", h.List)
		configs.PUT("/workspace-policy", h.SetWorkspacePolicy)
		configs.POST("/templates/query", h.QueryTemplates)
		configs.POST("", h.Create)
		configs.GET("/:id", h.Get)
		configs.PUT("/:id", h.Update)
		configs.DELETE("/:id", h.Delete)
		configs.GET("/:id/sandboxes", h.Inventory)
		// Skill uploads update the image used by sessions of this config.
		// The listing describes the skills available in that image.
		configs.GET("/:id/skills", skills.List)
		configs.POST("/:id/skills", skills.Upload)
		configs.GET("/:id/skills/:skillId", skills.Get)
		configs.GET("/:id/skills/:skillId/files", skills.ListFiles)
		configs.GET("/:id/skills/:skillId/files/content", skills.GetFile)
		configs.POST("/:id/skills/:skillId/reinstall", skills.Reinstall)
		configs.POST("/:id/skills/:skillId/stop", skills.Stop)
		configs.PATCH("/:id/skills/:skillId", skills.Patch)
		configs.DELETE("/:id/skills/:skillId", skills.Delete)
		configs.GET("/:id/skills/:skillId/install-events", skills.InstallEvents)
		configs.GET("/:id/skills/:skillId/transcript", skills.InstallTranscript)
	}
}

// RegisterEvaluationRoutes registers evaluation endpoints. Running an
// evaluation calls the LLM and reads knowledge bases in the personal workspace.
func RegisterEvaluationRoutes(r *gin.RouterGroup, handler *handler.EvaluationHandler, g *accessGuards) {
	evaluationRoutes := r.Group("/evaluation")
	{
		evaluationRoutes.POST("", handler.Evaluation)
		evaluationRoutes.GET("", handler.GetEvaluationResult)
	}
}

func RegisterInitializationRoutes(r *gin.RouterGroup, handler *handler.InitializationHandler, g *accessGuards) {
	r.Handle(http.MethodGet, "/initialization/config/:kbId", g.KBAccessRead("kbId"), handler.GetCurrentConfigByKB)
	r.Handle(http.MethodPost, "/initialization/initialize/:kbId", g.KBAccessWrite("kbId"), handler.InitializeByKB)
	r.Handle(http.MethodPut, "/initialization/config/:kbId", g.KBAccessWrite("kbId"), handler.UpdateKBConfig)

	r.Handle(http.MethodGet, "/initialization/ollama/status", handler.CheckOllamaStatus)
	r.Handle(http.MethodGet, "/initialization/ollama/models", handler.ListOllamaModels)
	r.Handle(http.MethodPost, "/initialization/ollama/models/check", handler.CheckOllamaModels)
	r.Handle(http.MethodPost, "/initialization/ollama/models/download", handler.DownloadOllamaModel)
	r.Handle(http.MethodGet, "/initialization/ollama/download/progress/:taskId", handler.GetDownloadProgress)
	r.Handle(http.MethodGet, "/initialization/ollama/download/tasks", handler.ListDownloadTasks)

	// 远程API相关接口
	r.Handle(http.MethodPost, "/initialization/remote/check", handler.CheckRemoteModel)
	r.Handle(http.MethodPost, "/initialization/embedding/test", handler.TestEmbeddingModel)
	r.Handle(http.MethodPost, "/initialization/rerank/check", handler.CheckRerankModel)
	r.Handle(http.MethodPost, "/initialization/asr/check", handler.CheckASRModel)
	r.Handle(http.MethodPost, "/initialization/multimodal/test", handler.TestMultimodalFunction)

}

func RegisterMCPServiceRoutes(
	r *gin.RouterGroup,
	handler *handler.MCPServiceHandler,
	credHandler *handler.MCPCredentialsHandler,
	oauthHandler *handler.MCPOAuthHandler,
	g *accessGuards) {
	// MCP OAuth provider redirect. Registered OUTSIDE the /mcp-services group
	// to avoid a static-vs-":id" route conflict, and left unauthenticated
	// (allow-listed in middleware/auth.go) because the third-party browser
	// redirect carries no Reliquary bearer — the single-use state authenticates.
	r.GET("/mcp-oauth/callback", oauthHandler.Callback)

	mcpServices := r.Group("/mcp-services")
	{
		// Create MCP service
		mcpServices.POST("", handler.CreateMCPService)
		mcpServices.GET("", handler.ListMCPServices)
		mcpServices.GET("/:id", handler.GetMCPService)
		// Update MCP service
		mcpServices.PUT("/:id", handler.UpdateMCPService)
		// Delete MCP service
		mcpServices.DELETE("/:id", handler.DeleteMCPService)
		// Test MCP service connection (probes external infrastructure)
		mcpServices.POST("/:id/test", handler.TestMCPService)
		mcpServices.GET("/:id/tools", handler.GetMCPServiceTools)
		mcpServices.GET("/:id/resources", handler.GetMCPServiceResources)
		// Per-field credential subresource: secrets never travel via the main
		// PUT body. See internal/handler/mcp_credentials.go for the contract.
		mcpServices.PUT("/:id/credentials", credHandler.Put)
		mcpServices.DELETE("/:id/credentials/:field", credHandler.DeleteField)
		mcpServices.GET("/:id/tool-approvals", handler.ListMCPToolApprovals)
		mcpServices.PUT("/:id/tool-approvals/:tool_name", handler.SetMCPToolApproval)
		mcpServices.POST("/:id/oauth/authorize-url", oauthHandler.AuthorizeURL)
		mcpServices.GET("/:id/oauth/status", oauthHandler.Status)
		mcpServices.DELETE("/:id/oauth/token", oauthHandler.Revoke)
	}

	// /agent tool-approval + OAuth resolution are interactive human flows;
	// not declared for API keys (default-deny).
	agentTool := r.Group("/agent")
	{
		agentTool.POST("/tool-approvals/:pending_id", handler.ResolveToolApproval)
		agentTool.POST("/mcp-oauth-resolutions/:pending_id", oauthHandler.ResolveMCPOAuth)
		agentTool.POST("/mcp-oauth-resolutions/:pending_id/cancel", oauthHandler.CancelMCPOAuth)
	}
}

// RegisterWebSearchRoutes registers web search routes
func RegisterWebSearchRoutes(r *gin.RouterGroup, webSearchHandler *handler.WebSearchHandler, g *accessGuards) {
	webSearch := r.Group("/web-search")
	{
		webSearch.GET("/providers", webSearchHandler.GetProviders)
	}
}

func RegisterWebSearchProviderRoutes(
	r *gin.RouterGroup,
	h *handler.WebSearchProviderHandler,
	credHandler *handler.WebSearchProviderCredentialsHandler,
	g *accessGuards) {
	providers := r.Group("/web-search-providers")
	{
		providers.GET("/types", h.ListProviderTypes)
		// Test with raw credentials (no persistence)
		providers.POST("/test", h.TestProviderRaw)
		// CRUD
		providers.POST("", h.CreateProvider)
		providers.GET("", h.ListProviders)
		providers.GET("/:id", h.GetProvider)
		providers.PUT("/:id", h.UpdateProvider)
		providers.DELETE("/:id", h.DeleteProvider)
		// Per-field credential subresource
		providers.PUT("/:id/credentials", credHandler.Put)
		providers.DELETE("/:id/credentials/:field", credHandler.DeleteField)
		// Test existing saved provider
		providers.POST("/:id/test", h.TestProviderByID)
	}
}

func RegisterDataSourceRoutes(
	r *gin.RouterGroup,
	handler *handler.DataSourceHandler,
	credHandler *handler.DataSourceCredentialsHandler,
	g *accessGuards) {
	// Data source routes
	ds := r.Group("/datasource")
	{
		ds.GET("/types", handler.GetAvailableConnectors)

		// Validate credentials without persistence (for "Test Connection" button)
		ds.POST("/validate-credentials", handler.ValidateCredentials)

		// CRUD operations
		ds.POST("", handler.CreateDataSource)
		ds.GET("", handler.ListDataSources)
		ds.GET("/:id", handler.GetDataSource)
		ds.PUT("/:id", handler.UpdateDataSource)
		ds.DELETE("/:id", handler.DeleteDataSource)

		// Credential subresource. Single logical field "credentials" because
		// connector credentials are a per-connector atomic map (see
		// internal/handler/datasource_credentials.go).
		ds.PUT("/:id/credentials", credHandler.Put)
		ds.DELETE("/:id/credentials/:field", credHandler.DeleteField)

		// Connection and resource management
		ds.POST("/:id/validate", handler.ValidateConnection)
		ds.GET("/:id/resources", handler.ListAvailableResources)
		ds.POST("/:id/resource-ancestors", handler.ResolveResourceAncestors)

		// Sync management
		ds.POST("/:id/sync", handler.ManualSync)
		ds.POST("/:id/pause", handler.PauseDataSource)
		ds.POST("/:id/resume", handler.ResumeDataSource)

		ds.GET("/:id/logs", handler.GetSyncLogs)
		ds.GET("/logs/:log_id", handler.GetSyncLog)
	}
}
