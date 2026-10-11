package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	werrors "reliquary/internal/errors"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func (h *KnowledgeHandler) Ingest(c *gin.Context) {
	var input types.IngestRequest
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		c.Error(werrors.NewBadRequestError("invalid ingest request: " + err.Error()))
		return
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		c.Error(werrors.NewBadRequestError("ingest request must contain one JSON object"))
		return
	}
	collector, ok := h.kgService.(interfaces.IngestService)
	if !ok {
		c.Error(werrors.NewInternalServerError("ingest service unavailable"))
		return
	}
	result, err := collector.Ingest(c.Request.Context(), &input)
	if err != nil {
		c.Error(err)
		return
	}
	status := http.StatusCreated
	if result.Status == "exists" {
		status = http.StatusOK
	}
	c.JSON(status, gin.H{"success": true, "data": result})
}
