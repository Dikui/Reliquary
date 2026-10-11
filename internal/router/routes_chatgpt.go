package router

import (
	"github.com/gin-gonic/gin"
	"reliquary/internal/handler"
	"reliquary/internal/types"
)

func RegisterChatGPTRoutes(v1 *gin.RouterGroup, h *handler.ChatGPTHandler, p *handler.ChatGPTPolicyHandler) {
	g := v1.Group("/chatgpt")
	// Linking and controlling subscription credentials belongs to the local
	// user's settings session, never a collector's API key.
	g.Use(func(c *gin.Context) {
		principal, ok := types.PrincipalFromContext(c.Request.Context())
		if !ok || principal.Type != types.PrincipalWebUser {
			c.AbortWithStatusJSON(403, gin.H{"code": 1, "msg": "sign in to Reliquary to manage ChatGPT"})
			return
		}
		c.Next()
	})
	if h != nil {
		g.POST("/login", h.StartLogin)
		g.GET("/accounts", h.Accounts)
		g.GET("/accounts/:account_id/models", h.Models)
		g.POST("/accounts/:account_id/logout", h.Logout)
		g.POST("/welcome/ack", h.AckWelcome)
	}
	if p != nil {
		g.GET("/policy", p.Get)
		g.PUT("/policy", p.Update)
		g.POST("/resume", p.Resume)
	}
}
