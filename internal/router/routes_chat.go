package router

import (
	"github.com/gin-gonic/gin"

	"reliquary/internal/handler"
	"reliquary/internal/handler/session"
)

func RegisterMessageRoutes(r *gin.RouterGroup, handler *handler.MessageHandler, g *accessGuards) {
	messages := r.Group("/messages")
	chatMessages := messages
	historyMessages := messages
	{
		historyMessages.POST("/search", handler.SearchMessages)
		historyMessages.GET("/chat-history-stats", handler.GetChatHistoryKBStats)
		chatMessages.GET("/:session_id/load", handler.LoadMessages)
		chatMessages.DELETE("/:session_id/:id", handler.DeleteMessage)
	}
}

func RegisterSessionRoutes(
	r *gin.RouterGroup,
	handler *session.Handler,
	g *accessGuards) {
	sessions := r.Group("/sessions")
	{
		sessions.POST("", handler.CreateSession)
		sessions.DELETE("/batch", handler.BatchDeleteSessions)
		sessions.GET("/:id", handler.GetSession)
		sessions.GET("", handler.GetSessionsByTenant)
		sessions.PUT("/:id", handler.UpdateSession)
		sessions.DELETE("/:id", handler.DeleteSession)
		sessions.DELETE("/:id/messages", handler.ClearSessionMessages)
		sessions.POST("/:session_id/generate_title", handler.GenerateTitle)
		sessions.POST("/:session_id/attachments", handler.UploadTemporaryDocument)
		sessions.GET("/:id/attachments", handler.ListTemporaryDocuments)
		sessions.GET("/:id/attachments/:attachment_id", handler.GetTemporaryDocument)
		sessions.GET("/:id/attachments/:attachment_id/preview", handler.PreviewTemporaryDocument)
		sessions.DELETE("/:id/attachments/:attachment_id", handler.DeleteTemporaryDocument)
		sessions.POST("/:session_id/stop", handler.StopSession)
		// POST and DELETE share this path but gin maintains a separate radix tree
		// per HTTP verb, and the existing trees use different wildcard names
		// (POST uses :session_id, DELETE uses :id). Use whatever matches each
		// tree to avoid "wildcard conflicts" panic at route registration.
		sessions.POST("/:session_id/pin", handler.PinSession)
		sessions.DELETE("/:id/pin", handler.UnpinSession)
		// 继续接收活跃流
		sessions.GET("/continue-stream/:session_id", handler.ContinueStream)

		// Persisted message file artifacts. The list endpoints only expose
		// metadata; the actual bytes are streamed via /artifacts/:index/download
		// so the storage URL never appears on the wire.
		//
		// NOTE: gin builds a separate radix tree per HTTP verb but every
		// path in the same tree must share the same wildcard name. The GET
		// tree already binds :id via /sessions/:id (GetSession); reusing
		// :id here (instead of :session_id) avoids the
		// "wildcard conflicts" panic at route registration. The handlers
		// read the URL param via c.Param("session_id") with a fallback to
		// c.Param("id") for exactly this reason.
		sessions.GET("/:id/artifacts", handler.ListSessionArtifacts)
		sessions.GET("/:id/messages/:message_id/artifacts", handler.ListMessageArtifacts)
		sessions.GET("/:id/messages/:message_id/artifacts/:index/download", handler.DownloadMessageArtifact)
	}
}

func RegisterChatRoutes(r *gin.RouterGroup, handler *session.Handler, g *accessGuards) {
	knowledgeChat := r.Group("/knowledge-chat")
	{
		knowledgeChat.POST("/:session_id", handler.KnowledgeQA)
	}

	// Agent-based chat
	agentChat := r.Group("/agent-chat")
	{
		agentChat.POST("/:session_id", handler.AgentQA)
	}

	// 新增知识检索接口，不需要session_id
	knowledgeSearch := r.Group("/knowledge-search")
	{
		knowledgeSearch.POST("", handler.SearchKnowledge)
	}
}
