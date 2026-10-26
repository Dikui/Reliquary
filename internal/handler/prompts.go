package handler

import (
	"net/http"
	"slices"

	"github.com/gin-gonic/gin"
	"reliquary/internal/config"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

// PromptHandler keeps library identity checks at the same boundary as other library settings.
type PromptHandler struct {
	libraries interfaces.LibraryService
	settings  interfaces.SystemSettingService
}

func NewPromptHandler(libraries interfaces.LibraryService, settings interfaces.SystemSettingService) *PromptHandler {
	return &PromptHandler{libraries: libraries, settings: settings}
}

func (h *PromptHandler) scope(c *gin.Context) (string, bool) {
	id := c.Query("library_id")
	if id != "" {
		if _, err := h.libraries.GetLibraryByID(c.Request.Context(), id); err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "library not found"})
			return "", false
		}
	}
	return id, true
}

func promptStorageKey(libraryID, id string) string {
	if libraryID != "" {
		return "prompt.library." + libraryID + "." + id
	}
	return "prompt." + id
}

func (h *PromptHandler) List(c *gin.Context) {
	libraryID, ok := h.scope(c)
	if !ok {
		return
	}
	rows := []*types.SystemSetting{}
	for _, id := range config.PromptScopeIDs(libraryID != "") {
		row, err := h.settings.Get(c.Request.Context(), promptStorageKey(libraryID, id))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		copy := *row
		copy.Key = "prompt." + id
		rows = append(rows, &copy)
	}
	c.JSON(http.StatusOK, rows)
}

func (h *PromptHandler) Update(c *gin.Context) {
	libraryID, ok := h.scope(c)
	if !ok {
		return
	}
	id := c.Param("id")
	if !slices.Contains(config.PromptScopeIDs(libraryID != ""), id) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "prompt is not available in this scope"})
		return
	}
	key := promptStorageKey(libraryID, id)
	if c.Request.Method == http.MethodDelete {
		if err := h.settings.Reset(c.Request.Context(), key); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"success": true})
		return
	}
	var input UpdateSystemSettingRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	row, err := h.settings.Update(c.Request.Context(), key, input.Value)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	copy := *row
	copy.Key = "prompt." + id
	c.JSON(http.StatusOK, &copy)
}
