package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apperrors "reliquary/internal/errors"
	"reliquary/internal/types"
)

func decodeLibraryRequest(c *gin.Context, target any) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return apperrors.NewBadRequestError(err.Error())
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return apperrors.NewBadRequestError("request requires one JSON object")
	}
	return nil
}

func (h *LibraryHandler) ListLibraries(c *gin.Context) {
	libraries, err := h.service.ListLibraries(c.Request.Context())
	if err != nil {
		c.Error(err)
		return
	}
	data := make([]any, 0, len(libraries))
	for _, library := range libraries {
		data = append(data, buildLibraryResponse(library, map[string]interface{}{"display_name": library.DisplayName(types.LanguageFromContextOrDefault(c.Request.Context()))}))
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *LibraryHandler) CreateLibrary(c *gin.Context) {
	var request types.CreateLibraryRequest
	if err := decodeLibraryRequest(c, &request); err != nil {
		c.Error(err)
		return
	}
	library, err := h.service.CreateLibrary(c.Request.Context(), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": library})
}

func (h *LibraryHandler) ConfigureLibrary(c *gin.Context) {
	var request types.LibraryPatch
	if err := decodeLibraryRequest(c, &request); err != nil {
		c.Error(err)
		return
	}
	library, err := h.service.ConfigureLibrary(c.Request.Context(), c.Param("id"), request)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": library})
}

func (h *LibraryHandler) DeleteLibrary(c *gin.Context) {
	if err := h.service.DeleteLibrary(c.Request.Context(), c.Param("id")); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
