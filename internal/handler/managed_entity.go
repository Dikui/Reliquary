package handler

import (
	"net/http"
	"strconv"
	"time"

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
		Excluded     *bool   `json:"excluded"`
		RelationType *string `json:"relation_type"`
	}
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	if input.Excluded == nil {
		c.Error(apperrors.NewBadRequestError("excluded is required"))
		return
	}
	if err := h.service.SetEntryEntityLink(c.Request.Context(), c.Param("id"), c.Param("entity_id"), *input.Excluded, input.RelationType); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}

func (h *EntityHandler) EntityAttributes(c *gin.Context) {
	at := time.Now()
	if raw := c.Query("at"); raw != "" {
		var err error
		at, err = time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			at, err = time.Parse(time.DateOnly, raw)
		}
		if err != nil {
			c.Error(apperrors.NewBadRequestError("at must be RFC3339 or YYYY-MM-DD"))
			return
		}
	}
	result, err := h.service.EntityAttributes(c.Request.Context(), c.Param("entity_id"), at)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) EntityAttributeHistory(c *gin.Context) {
	page, e1 := strconv.Atoi(c.DefaultQuery("page", "1"))
	size, e2 := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if e1 != nil || e2 != nil {
		c.Error(apperrors.NewBadRequestError("invalid history pagination"))
		return
	}
	result, err := h.service.EntityAttributeHistory(c.Request.Context(), c.Param("entity_id"), page, size)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) EntityReorganizations(c *gin.Context) {
	result, err := h.service.EntityReorganizations(c.Request.Context(), c.Param("entity_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) MergeManagedEntities(c *gin.Context) {
	var input types.ManagedEntityMergeInput
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.MergeManagedEntities(c.Request.Context(), c.Param("entity_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *EntityHandler) SplitManagedEntity(c *gin.Context) {
	var input types.ManagedEntitySplitInput
	if err := decodeLibraryRequest(c, &input); err != nil {
		c.Error(err)
		return
	}
	result, err := h.service.SplitManagedEntity(c.Request.Context(), c.Param("entity_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": result})
}
