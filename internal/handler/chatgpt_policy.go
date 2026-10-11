package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/application/service"
)

type ChatGPTPolicyHandler struct{ service *service.ChatGPTPolicyService }

func NewChatGPTPolicyHandler(s *service.ChatGPTPolicyService) *ChatGPTPolicyHandler {
	return &ChatGPTPolicyHandler{service: s}
}
func (h *ChatGPTPolicyHandler) Get(c *gin.Context) {
	p, err := h.service.Get(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": gin.H{"code": "chatgpt_unavailable", "message": "cannot load ChatGPT usage state"}})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": p})
}
func (h *ChatGPTPolicyHandler) Update(c *gin.Context) {
	var req struct {
		DailyLimit      *int   `json:"daily_limit"`
		FallbackModelID string `json:"fallback_model_id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(400, gin.H{"success": false, "error": gin.H{"code": "invalid_policy", "message": "invalid usage policy"}})
		return
	}
	p, err := h.service.Update(c.Request.Context(), req.DailyLimit, req.FallbackModelID)
	if err != nil {
		c.JSON(400, gin.H{"success": false, "error": gin.H{"code": "invalid_policy", "message": err.Error()}})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": p})
}
func (h *ChatGPTPolicyHandler) Resume(c *gin.Context) {
	p, err := h.service.Resume(c.Request.Context())
	if err != nil {
		c.JSON(500, gin.H{"success": false, "error": gin.H{"code": "chatgpt_unavailable", "message": "cannot resume ChatGPT usage"}})
		return
	}
	c.JSON(200, gin.H{"success": true, "data": p})
}
