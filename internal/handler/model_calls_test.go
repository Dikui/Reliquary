package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/modelcalls"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestModelCallHTTPFiltersAndSummary(t *testing.T) {
	db := pgtest.Open(t)
	h := NewModelHandlerWithMaintenance(nil, db)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), types.SpaceIDContextKey, uint64(1)))
		c.Next()
		if len(c.Errors) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": c.Errors.Last().Error()})
		}
	})
	router.GET("/summary", h.ModelCallSummary)
	router.GET("/trend", h.ModelCallTrend)
	router.GET("/entries/:id/calls", h.ListModelCalls)
	entry := "a"
	input := int64(10)
	require.NoError(t, db.Create(&types.ModelCall{ID: uuid.NewString(), SpaceID: 1, RequestedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeBodyEmbedding, EntryID: &entry, InputTokens: &input, CostStatus: "unknown", Currency: "USD", DurationMS: 200, Success: true, Attempt: 1}).Error)
	for _, path := range []string{"/summary?period=day", "/trend?from=2026-01-01T00:00:00Z&to=2026-01-02T00:00:00Z", "/entries/a/calls"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 200, rec.Code, rec.Body.String())
		var body struct {
			Data []json.RawMessage `json:"data"`
		}
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
		require.Len(t, body.Data, 1)
		if path == "/summary?period=day" {
			var row modelcalls.Summary
			require.NoError(t, json.Unmarshal(body.Data[0], &row))
			require.EqualValues(t, 1, row.Calls)
			require.EqualValues(t, 10, *row.InputTokens)
			require.Nil(t, row.OutputTokens)
			require.Nil(t, row.Cost)
		}
	}
	for _, path := range []string{"/summary?period=year", "/summary?group_by=invalid", "/summary?purpose=invalid", "/trend?from=broken", "/trend?from=2026-01-02T00:00:00Z&to=2026-01-01T00:00:00Z", "/entries/a/calls?limit=1001", "/entries/a/calls?offset=-1"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
		require.Equal(t, 400, rec.Code, path)
	}
}
func TestModelPricingUpdatePresence(t *testing.T) {
	for _, tc := range []struct {
		body    string
		present bool
	}{
		{`{"parameters":{"provider":"openai"}}`, false},
		{`{"parameters":{"pricing":{"input_per_million":2,"currency":"USD"}}}`, true},
		{`{"parameters":{"pricing":null}}`, true},
	} {
		var req UpdateModelRequest
		require.NoError(t, json.Unmarshal([]byte(tc.body), &req))
		require.Equal(t, tc.present, req.PricingProvided)
	}
}
