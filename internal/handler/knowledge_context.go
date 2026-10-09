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
	service   *service.KnowledgeContextService
	worker    *service.KnowledgeContextWorker
	rebuilder *service.ContextNameEmbeddingRebuilder
	backfill  *service.KnowledgeContextBackfillService
}

func NewKnowledgeContextHandler(s *service.KnowledgeContextService, w *service.KnowledgeContextWorker) *KnowledgeContextHandler {
	return &KnowledgeContextHandler{service: s, worker: w}
}

func (h *KnowledgeContextHandler) SetNameEmbeddingRebuilder(rebuilder *service.ContextNameEmbeddingRebuilder) {
	h.rebuilder = rebuilder
}

// NameEmbeddingRebuildStatus exposes pending work and the most recent task failure.
func (h *KnowledgeContextHandler) NameEmbeddingRebuildStatus(c *gin.Context) {
	if h.rebuilder == nil {
		c.Error(werrors.NewInternalServerError("name embedding rebuilder unavailable"))
		return
	}
	status, err := h.rebuilder.Status(c.Request.Context(), types.MustTenantIDFromContext(c.Request.Context()))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": status})
}

// RebuildNameEmbeddings schedules the pending global object names on the task queue.
func (h *KnowledgeContextHandler) RebuildNameEmbeddings(c *gin.Context) {
	if h.rebuilder == nil {
		c.Error(werrors.NewInternalServerError("name embedding rebuilder unavailable"))
		return
	}
	ctx := c.Request.Context()
	if err := h.rebuilder.MarkAndEnqueue(ctx, types.MustTenantIDFromContext(ctx), "administrator"); err != nil {
		c.Error(err)
		return
	}
	h.NameEmbeddingRebuildStatus(c)
}

func (h *KnowledgeContextHandler) UpdateEntityDescription(c *gin.Context) {
	var input struct {
		Description *string `json:"description" binding:"required"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.Error(werrors.NewBadRequestError("description is required"))
		return
	}
	entity, err := h.service.UpdateEntityDescription(c.Request.Context(), c.Param("entity_id"), *input.Description)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": entity})
}

// Query queries sourced facts in a knowledge base, domain or all knowledge bases.
// @Summary Query structured knowledge
// @Tags Knowledge Context
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body types.KnowledgeContextQuery true "Scope and structured conditions"
// @Success 200 {object} KnowledgeContextQueryResponse "Sourced entities, facts, events and latest template-only message count"
// @Failure 400 {object} map[string]interface{}
// @Router /knowledge-context/query [post]
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
	q := types.KnowledgeContextQuery{Scope: c.Query("scope"), DomainID: c.Query("domain_id"), KnowledgeBaseID: c.Query("knowledge_base_id"), Entity: c.Query("entity"), EntityID: c.Query("entity_id"), Predicate: c.Query("predicate"), Relation: c.Query("relation"), ObjectEntityID: c.Query("object_entity_id"), EventID: c.Query("event_id")}
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

// Entities lists the distinct global objects visible in the selected scope.
// @Summary List global objects
// @Tags Knowledge Context
// @Produce json
// @Security Bearer
// @Param scope query string false "Query scope; team is a legacy alias of all" Enums(knowledge_base,domain,all,team)
// @Param knowledge_base_id query string false "Knowledge base for knowledge_base scope"
// @Param domain_id query string false "Domain for domain scope"
// @Param entity query string false "Object name or alias"
// @Param limit query int false "Page size (1-100)"
// @Param offset query int false "Page offset"
// @Success 200 {object} map[string]interface{}
// @Router /knowledge-context/entities [get]
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

// Events lists sourced events in the selected scope.
// @Summary List events by scope
// @Tags Knowledge Context
// @Produce json
// @Security Bearer
// @Param scope query string false "Query scope; team is a legacy alias of all" Enums(knowledge_base,domain,all,team)
// @Param knowledge_base_id query string false "Knowledge base for knowledge_base scope"
// @Param domain_id query string false "Domain for domain scope"
// @Param entity_id query string false "Related global object"
// @Param limit query int false "Page size (1-100)"
// @Param offset query int false "Page offset"
// @Success 200 {object} map[string]interface{}
// @Router /knowledge-context/events [get]
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

// Evidence lists source evidence in the selected scope.
// @Summary List evidence by scope
// @Tags Knowledge Context
// @Produce json
// @Security Bearer
// @Param scope query string false "Query scope; team is a legacy alias of all" Enums(knowledge_base,domain,all,team)
// @Param knowledge_base_id query string false "Knowledge base for knowledge_base scope"
// @Param domain_id query string false "Domain for domain scope"
// @Param entity_id query string false "Related global object"
// @Param event_id query string false "Related event"
// @Param limit query int false "Page size (1-100)"
// @Param offset query int false "Page offset"
// @Success 200 {object} map[string]interface{}
// @Router /knowledge-context/evidence [get]
func (h *KnowledgeContextHandler) Evidence(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, excludedCount, err := h.service.ListEvidenceWithPublicationStats(c.Request.Context(), c.Param("id"), q)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items, "excluded_missing_published_at_count": excludedCount})
}

// GetEvidence returns a source evidence record.
// @Summary View source evidence
// @Tags Knowledge Context
// @Produce json
// @Security Bearer
// @Param evidence_id path string true "Evidence ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /knowledge-context/evidence/{evidence_id} [get]
func (h *KnowledgeContextHandler) GetEvidence(c *gin.Context) {
	item, err := h.service.GetEvidence(c.Request.Context(), c.Param("id"), c.Param("evidence_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}

// CorrectFact records a sourced correction and an audit entry.
// @Summary Correct a sourced fact
// @Tags Knowledge Context
// @Accept json
// @Produce json
// @Security Bearer
// @Param fact_id path string true "Fact ID"
// @Param request body types.KnowledgeFactCorrection true "Fact correction"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /knowledge-context/facts/{fact_id}/correction [post]
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

// MergeEntities merges global identity and audits affected knowledge bases.
// @Summary Merge global objects
// @Tags Knowledge Context
// @Accept json
// @Produce json
// @Security Bearer
// @Param request body object{keep_entity_id=string,merge_entity_id=string} true "Global object IDs"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]interface{}
// @Router /knowledge-context/entities/merge [post]
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

// EntityDetail collects an object's sourced facts and events across the space.
// @Summary View a global object with source knowledge bases
// @Tags Knowledge Context
// @Produce json
// @Security Bearer
// @Param entity_id path string true "Global object ID"
// @Param include_history query boolean false "Include historical facts and evidence (default true)"
// @Failure 400 {object} map[string]interface{}
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]interface{}
// @Router /knowledge-context/entities/{entity_id} [get]
func (h *KnowledgeContextHandler) EntityDetail(c *gin.Context) {
	includeHistory, err := strconv.ParseBool(c.DefaultQuery("include_history", "true"))
	if err != nil {
		c.Error(werrors.NewBadRequestError("invalid history flag"))
		return
	}
	result, err := h.service.EntityDetail(c.Request.Context(), c.Param("entity_id"), includeHistory)
	if err != nil {
		c.Error(err)
		return
	}
	if len(result.Entities) == 0 {
		c.Error(werrors.NewNotFoundError("knowledge entity not found"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

func (h *KnowledgeContextHandler) MergeSuggestions(c *gin.Context) {
	q, err := contextListQuery(c)
	if err != nil {
		c.Error(err)
		return
	}
	items, err := h.service.ListMergeSuggestions(c.Request.Context(), c.Param("id"), q, c.Query("status"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": items})
}
func (h *KnowledgeContextHandler) AcceptMergeSuggestion(c *gin.Context) {
	h.decideMergeSuggestion(c, true)
}
func (h *KnowledgeContextHandler) RejectMergeSuggestion(c *gin.Context) {
	h.decideMergeSuggestion(c, false)
}
func (h *KnowledgeContextHandler) decideMergeSuggestion(c *gin.Context, accept bool) {
	item, err := h.service.DecideMergeSuggestion(c.Request.Context(), c.Param("suggestion_id"), accept)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": item})
}
