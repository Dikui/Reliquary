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

func RegisterInitializationRoutes(r *gin.RouterGroup, handler *handler.InitializationHandler, g *accessGuards) {
	r.Handle(http.MethodGet, "/initialization/config/:kbId", g.KBExists("kbId"), handler.GetCurrentConfigByKB)
	r.Handle(http.MethodPost, "/initialization/initialize/:kbId", g.KBExists("kbId"), handler.InitializeByKB)
	r.Handle(http.MethodPut, "/initialization/config/:kbId", g.KBExists("kbId"), handler.UpdateKBConfig)

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
	r.Handle(http.MethodPost, "/initialization/multimodal/test", handler.TestMultimodalFunction)

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

func RegisterUserFavoriteRoutes(r *gin.RouterGroup, h *handler.ResourceFavoriteHandler, g *accessGuards) {
	favorites := r.Group("/user/favorites")
	favorites.GET("", h.ListFavorites)
	favorites.POST("", h.AddFavorite)
	favorites.DELETE("/:type/:id", h.RemoveFavorite)
}
