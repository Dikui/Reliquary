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

}
