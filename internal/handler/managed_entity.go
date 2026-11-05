package handler

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *EntityHandler) CreateManagedEntity(c *gin.Context) {
	var input types.ManagedEntityInput
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	entity, err := h.service.CreateManagedEntity(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": entity})
}

func (h *EntityHandler) UpdateManagedEntity(c *gin.Context) {
	var input types.ManagedEntityPatch
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	entity, err := h.service.UpdateManagedEntity(c.Request.Context(), c.Param("entity_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entity})
}

func (h *EntityHandler) ManagedEntityHistory(c *gin.Context) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e1 != nil || e2 != nil {
		c.Error(apperrors.NewBadRequestError("invalid history pagination"))
		return
	}
	result, err := h.service.ManagedEntityHistory(c.Request.Context(), c.Param("entity_id"), page, size)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) SaveManagedRelation(c *gin.Context) {
	var input types.ManagedRelationInput
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	row, err := h.service.SaveManagedRelation(c.Request.Context(), c.Param("id"), c.Param("relation_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	status := http.StatusOK
	if c.Request.Method == http.MethodPost {
		status = http.StatusCreated
	}
	c.JSON(status, gin.H{"success": true, "data": row})
}

func (h *EntityHandler) EntryEntityLinks(c *gin.Context) {
	rows, err := h.service.EntryEntityLinks(c.Request.Context(), c.Param("id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rows})
}

func (h *EntityHandler) SetEntryEntityLink(c *gin.Context) {
	var input struct {
		Excluded *bool `json:"excluded"`
	}
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	if input.Excluded == nil {
		c.Error(apperrors.NewBadRequestError("excluded is required"))
		return
	}
	if err := h.service.SetEntryEntityLink(c.Request.Context(), c.Param("id"), c.Param("entity_id"), *input.Excluded); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
