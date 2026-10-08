package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func (h *KnowledgeContextHandler) FocusScope(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.FocusScope(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *KnowledgeContextHandler) SetEntityStar(c *gin.Context) {
	if err := h.service.SetEntityStar(c.Request.Context(), c.Param("entity_id"), c.Request.Method == http.MethodPut); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *KnowledgeContextHandler) SetTopicStar(c *gin.Context) {
	if err := h.service.SetTopicStar(c.Request.Context(), c.Param("tag_id"), c.Request.Method == http.MethodPut); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
