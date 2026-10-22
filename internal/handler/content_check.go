package handler

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/application/service"
	"reliquary/internal/types"
	"strconv"
)

type ContentCheckHandler struct{ service *service.ContentCheckService }

func NewContentCheckHandler(s *service.ContentCheckService) *ContentCheckHandler {
	return &ContentCheckHandler{service: s}
}
func (h *ContentCheckHandler) Settings(c *gin.Context) {
	out, err := h.service.Settings(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": out})
}
func (h *ContentCheckHandler) Save(c *gin.Context) {
	var in types.ContentCheckSettings
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(400, gin.H{"success": false, "message": "invalid settings"})
		return
	}
	if err := h.service.Configure(c.Request.Context(), in); err != nil {
		c.Error(err)
		return
	}
	h.Settings(c)
}
func (h *ContentCheckHandler) List(c *gin.Context) {
	page := 1
	if raw := c.Query("page"); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil || v < 1 {
			c.JSON(400, gin.H{"success": false, "message": "invalid page"})
			return
		}
		page = v
	}
	out, err := h.service.List(c.Request.Context(), page)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": out})
}
func (h *ContentCheckHandler) Get(c *gin.Context) {
	out, err := h.service.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(200, gin.H{"success": true, "data": out})
}
func (h *ContentCheckHandler) Recheck(c *gin.Context) {
	out, err := h.service.Start(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(202, gin.H{"success": true, "data": out})
}
