package handler

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"reliquary/internal/errors"
	"reliquary/internal/logger"
	"reliquary/internal/storageurl"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"strings"

	"github.com/gin-gonic/gin"
)

type SearchHandler struct {
	service     interfaces.SearchService
	fileService interfaces.FileService
}

func NewSearchHandler(search interfaces.SearchService, files interfaces.FileService) *SearchHandler {
	return &SearchHandler{service: search, fileService: files}
}

func (h *SearchHandler) SearchEntry(c *gin.Context) {
	var request types.EntrySearchRequest
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
	c.JSON(http.StatusOK, gin.H{"success": true, "data": rewriteEntrySearch(ctx, rewriter, results)})
}

func rewriteEntrySearch(ctx context.Context, rewriter *storageurl.Rewriter, response *types.EntrySearchResponse) *types.EntrySearchResponse {
	if response == nil || !rewriter.Enabled() {
		return response
	}
	copyResult := func(result *types.EntrySearchResult) *types.EntrySearchResult {
		if result == nil {
			return nil
		}
		copy := *result
		copy.Snippets = append([]types.EntrySearchSnippet{}, result.Snippets...)
		for i := range copy.Snippets {
			copy.Snippets[i].Content = rewriter.String(ctx, copy.Snippets[i].Content)
		}
		return &copy
	}
	copy := *response
	copy.Results = make([]*types.EntrySearchResult, len(response.Results))
	for i, result := range response.Results {
		copy.Results[i] = copyResult(result)
	}
	return &copy
}

func (h *SearchHandler) Options(c *gin.Context) {
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
	return errors.NewSearchUnavailableError()
}
