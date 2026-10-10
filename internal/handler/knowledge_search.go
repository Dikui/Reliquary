package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"github.com/gin-gonic/gin"
	"io"
	"net/http"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/storageurl"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"
)

type KnowledgeSearchHandler struct {
	service     interfaces.KnowledgeSearchService
	fileService interfaces.FileService
}

func NewKnowledgeSearchHandler(search interfaces.KnowledgeSearchService, files interfaces.FileService) *KnowledgeSearchHandler {
	return &KnowledgeSearchHandler{service: search, fileService: files}
}

func (h *KnowledgeSearchHandler) SearchKnowledge(c *gin.Context) {
	var request types.ArticleSearchRequest
	if err := decodeSearchJSON(c, &request); err != nil {
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
	ctx := c.Request.Context()
	results, err := h.service.Search(ctx, request)
	if err != nil {
		logger.ErrorWithFields(ctx, err, nil)
		c.Error(searchClientError(err))
		return
	}
	rewriter := storageurl.NewRequestRewriter(ctx, mode, h.fileService)
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rewriteArticleSearch(ctx, rewriter, results)})
}

func rewriteArticleSearch(ctx context.Context, rewriter *storageurl.Rewriter, response *types.ArticleSearchResponse) *types.ArticleSearchResponse {
	if response == nil || !rewriter.Enabled() {
		return response
	}
	var copyResult func(*types.ArticleSearchResult) *types.ArticleSearchResult
	copyResult = func(result *types.ArticleSearchResult) *types.ArticleSearchResult {
		if result == nil {
			return nil
		}
		copy := *result
		copy.Snippets = append([]types.ArticleSearchSnippet{}, result.Snippets...)
		for i := range copy.Snippets {
			copy.Snippets[i].Content = rewriter.String(ctx, copy.Snippets[i].Content)
		}
		copy.SimilarReports = make([]*types.ArticleSearchResult, len(result.SimilarReports))
		for i, report := range result.SimilarReports {
			copy.SimilarReports[i] = copyResult(report)
		}
		return &copy
	}
	copy := *response
	copy.Results = make([]*types.ArticleSearchResult, len(response.Results))
	for i, result := range response.Results {
		copy.Results[i] = copyResult(result)
	}
	return &copy
}

func (h *KnowledgeSearchHandler) Options(c *gin.Context) {
	options, err := h.service.Options(c.Request.Context())
	if err != nil {
		logger.ErrorWithFields(c.Request.Context(), err, nil)
		c.Error(searchClientError(err))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": options})
}

func decodeSearchJSON(c *gin.Context, target interface{}) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		if strings.Contains(err.Error(), "unknown field") {
			return errors.NewBadRequestError("unsupported search parameter: " + strings.TrimPrefix(err.Error(), "json: unknown field "))
		}
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.NewBadRequestError("search request must contain one JSON object")
	}
	return nil
}

func searchClientError(err error) *errors.AppError {
	var appErr *errors.AppError
	if stderrors.As(err, &appErr) && appErr.HTTPCode >= 400 && appErr.HTTPCode < 500 {
		return appErr
	}
	return errors.NewKnowledgeSearchUnavailableError()
}
