package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
)

type AgentModelHandler struct{ service *service.AgentModelService }

func NewAgentModelHandler(service *service.AgentModelService) *AgentModelHandler {
	return &AgentModelHandler{service: service}
}

func (h *AgentModelHandler) Respond(c *gin.Context) {
	principal, ok := types.PrincipalFromContext(c.Request.Context())
	if !ok || principal.Type != types.PrincipalAPIKey {
		c.JSON(http.StatusUnauthorized, gin.H{"error": gin.H{"code": "api_key_required"}})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 2*1024*1024))
	if err != nil || !json.Valid(body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": gin.H{"code": "invalid_request"}})
		return
	}
	started := false
	err = h.service.Respond(c.Request.Context(), c.Param("model_id"), body, func(event []byte) error {
		if !started {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-store")
			c.Header("X-Accel-Buffering", "no")
			started = true
		}
		_, err := c.Writer.Write(event)
		c.Writer.Flush()
		return err
	})
	if err == nil {
		return
	}
	code, message, status := "model_unavailable", "模型调用失败，请检查模型配置或登录状态", http.StatusBadGateway
	if types.IsChatGPTDeferred(err) || types.IsChatGPTQuotaError(err) {
		code, message, status = "subscription_paused", "订阅调用已暂停，请检查用量设置", http.StatusServiceUnavailable
	}
	var upstream *types.ChatGPTError
	if errors.As(err, &upstream) && upstream.Code != "" {
		code = upstream.Code
	}
	if !started {
		c.JSON(status, gin.H{"error": gin.H{"code": code, "message": message}})
		return
	}
	data, _ := json.Marshal(gin.H{"type": "error", "code": code, "message": message})
	_, _ = c.Writer.Write(append(append([]byte("data: "), data...), []byte("\n\n")...))
	c.Writer.Flush()
}
