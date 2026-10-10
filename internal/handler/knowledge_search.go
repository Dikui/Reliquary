package handler

import (
	stderrors "errors"
	"github.com/gin-gonic/gin"
	"net/http"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/storageurl"
	"reliquary/internal/types/interfaces"
)

type KnowledgeSearchHandler struct {
	service     interfaces.KnowledgeSearchService
	fileService interfaces.FileService
}

func NewKnowledgeSearchHandler(search interfaces.KnowledgeSearchService, files interfaces.FileService) *KnowledgeSearchHandler {
	return &KnowledgeSearchHandler{service: search, fileService: files}
}

type SearchKnowledgeRequest struct {
	Query            string   `json:"query" binding:"required"`
	KnowledgeBaseID  string   `json:"knowledge_base_id"`
	KnowledgeBaseIDs []string `json:"knowledge_base_ids"`
	KnowledgeIDs     []string `json:"knowledge_ids"`
}

func (h *KnowledgeSearchHandler) SearchKnowledge(c *gin.Context) {
	var request SearchKnowledgeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.Error(errors.NewBadRequestError(err.Error()))
		return
	}
	mode, err := storageurl.ResolveMode(c.Request.Context(), c.Query(storageurl.QueryParam))
	if err != nil {
		if stderrors.Is(err, storageurl.ErrPublicModeForbidden) {
			c.Error(errors.NewForbiddenError(err.Error()))
		} else {
			c.Error(errors.NewBadRequestError(err.Error()))
		}
		return
	}
	if request.KnowledgeBaseID != "" {
		found := false
		for _, id := range request.KnowledgeBaseIDs {
			if id == request.KnowledgeBaseID {
				found = true
				break
			}
		}
		if !found {
			request.KnowledgeBaseIDs = append(request.KnowledgeBaseIDs, request.KnowledgeBaseID)
		}
	}
	if len(request.KnowledgeBaseIDs) == 0 && len(request.KnowledgeIDs) == 0 {
		c.Error(errors.NewBadRequestError("At least one knowledge_base_id, knowledge_base_ids, or knowledge_ids must be provided"))
		return
	}
	ctx := c.Request.Context()
	results, err := h.service.SearchKnowledge(ctx, request.KnowledgeBaseIDs, request.KnowledgeIDs, request.Query)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(errors.NewInternalServerError(err.Error()))
		return
	}
	rewriter := storageurl.NewRequestRewriter(ctx, mode, h.fileService)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rewriter.CopyReferences(ctx, results)})
}
