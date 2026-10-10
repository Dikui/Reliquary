package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"reliquary/internal/application/service"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func (h *KnowledgeContextHandler) SetBackfillService(backfill *service.KnowledgeContextBackfillService) {
	h.backfill = backfill
}

type KnowledgeContextBackfillEstimateResponse struct {
	Success bool                                    `json:"success"`
	Data    *types.KnowledgeContextBackfillEstimate `json:"data"`
}

type KnowledgeContextBackfillBatchResponse struct {
	Success bool                                 `json:"success"`
	Data    *types.KnowledgeContextBackfillBatch `json:"data"`
}

type KnowledgeContextBackfillBatchesResponse struct {
	Success bool                                  `json:"success"`
	Data    []types.KnowledgeContextBackfillBatch `json:"data"`
}

func backfillTarget(c *gin.Context) types.KnowledgeContextBackfillTarget {
	return types.KnowledgeContextBackfillTarget{EntityID: c.Query("entity_id"), TopicID: c.Query("topic_id")}
}

// BackfillEstimate returns eligible messages and the current batch call estimate.
func (h *KnowledgeContextHandler) BackfillEstimate(c *gin.Context) {
	if h.backfill == nil {
		c.Error(werrors.NewInternalServerError("backfill unavailable"))
		return
	}
	result, err := h.backfill.Estimate(c.Request.Context(), backfillTarget(c))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// StartBackfill manually queues up to 200 of the newest eligible snapshots.
func (h *KnowledgeContextHandler) StartBackfill(c *gin.Context) {
	if h.backfill == nil {
		c.Error(werrors.NewInternalServerError("backfill unavailable"))
		return
	}
	var target types.KnowledgeContextBackfillTarget
	if err := c.ShouldBindJSON(&target); err != nil {
		c.Error(werrors.NewBadRequestError("invalid backfill target"))
		return
	}
	result, err := h.backfill.Start(c.Request.Context(), target)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"success": true, "data": result})
}

// BackfillBatches lists recent batches for the requested object or topic.
func (h *KnowledgeContextHandler) BackfillBatches(c *gin.Context) {
	if h.backfill == nil {
		c.Error(werrors.NewInternalServerError("backfill unavailable"))
		return
	}
	result, err := h.backfill.Batches(c.Request.Context(), backfillTarget(c))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}

// GetBackfillBatch includes terminal errors and per-message results.
func (h *KnowledgeContextHandler) GetBackfillBatch(c *gin.Context) {
	if h.backfill == nil {
		c.Error(werrors.NewInternalServerError("backfill unavailable"))
		return
	}
	result, err := h.backfill.Batch(c.Request.Context(), c.Param("batch_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": result})
}
