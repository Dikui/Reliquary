package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

type KnowledgeContextHandler struct {
	service *service.KnowledgeContextService
	worker  *service.KnowledgeContextWorker
}

func NewKnowledgeContextHandler(s *service.KnowledgeContextService, w *service.KnowledgeContextWorker) *KnowledgeContextHandler {
	return &KnowledgeContextHandler{service: s, worker: w}
}

func (h *KnowledgeContextHandler) Query(c *gin.Context) {
	var input types.KnowledgeContextQuery
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid structured query"))
		return
	}
	result, err := h.service.Query(c.Request.Context(), c.Param("id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func contextListQuery(c *gin.Context) (types.KnowledgeContextQuery, error) {
	q := types.KnowledgeContextQuery{Entity: c.Query("entity"), EntityID: c.Query("entity_id"), Predicate: c.Query("predicate"), Relation: c.Query("relation"), ObjectEntityID: c.Query("object_entity_id"), EventID: c.Query("event_id")}
	var err error
	q.Limit, err = strconv.Atoi(c.DefaultQuery("limit", "50"))
	if err != nil || q.Limit < 1 || q.Limit > 100 {
		return q, werrors.NewBadRequestError("limit must be 1-100")
	}
	q.Offset, err = strconv.Atoi(c.DefaultQuery("offset", "0"))
	if err != nil || q.Offset < 0 {
		return q, werrors.NewBadRequestError("invalid offset")
	}
	q.IncludeHistory, err = strconv.ParseBool(c.DefaultQuery("include_history", "false"))
	if err != nil {
		return q, werrors.NewBadRequestError("invalid history flag")
	}
	for key, target := range map[string]**time.Time{"since": &q.Since, "until": &q.Until} {
		if raw := c.Query(key); raw != "" {
			value, err := time.Parse(time.RFC3339, raw)
			if err != nil {
				value, err = time.Parse("2006-01-02", raw)
			}
			if err != nil {
				return q, werrors.NewBadRequestError("invalid " + key + " time")
			}
			*target = &value
		}
	}
	if q.Since != nil && q.Until != nil && q.Since.After(*q.Until) {
		return q, werrors.NewBadRequestError("invalid time range")
	}
	return q, nil
}

func (h *KnowledgeContextHandler) Entities(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListEntities(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *KnowledgeContextHandler) Events(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListEvents(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *KnowledgeContextHandler) Evidence(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListEvidence(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *KnowledgeContextHandler) GetEvidence(c *gin.Context) {
	item, err := h.service.GetEvidence(c.Request.Context(), c.Param("id"), c.Param("evidence_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *KnowledgeContextHandler) CorrectFact(c *gin.Context) {
	var input types.KnowledgeFactCorrection
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid fact correction"))
		return
	}
	item, err := h.service.CorrectFact(c.Request.Context(), c.Param("id"), c.Param("fact_id"), input)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
func (h *KnowledgeContextHandler) MergeEntities(c *gin.Context) {
	var input struct {
		Keep  string `json:"keep_entity_id" binding:"required"`
		Merge string `json:"merge_entity_id" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("two entity IDs required"))
		return
	}
	if err := h.service.MergeEntities(c.Request.Context(), c.Param("id"), input.Keep, input.Merge); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
func (h *KnowledgeContextHandler) Rebuild(c *gin.Context) {
	var input struct {
		KnowledgeIDs []string `json:"knowledge_ids"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid rebuild scope"))
		return
	}
	if len(input.KnowledgeIDs) > 500 {
		c.Error(werrors.NewBadRequestError("at most 500 selected messages"))
		return
	}
	count, err := h.worker.Rebuild(c.Request.Context(), c.Param("id"), input.KnowledgeIDs)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": gin.H{"queued": count}})
}
