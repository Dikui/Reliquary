package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"reliquary/internal/types"
)

func retrievalPatchFixture() *types.RetrievalConfig {
	return &types.RetrievalConfig{
		ImageVectorEnabled: true, EmbeddingTopK: 71, VectorThreshold: 0.25,
		KeywordThreshold: 0.35, RerankTopK: 13, RerankThreshold: -0.1,
		RRFK: 81, RRFVectorWeight: 0.6, RRFKeywordWeight: 0.4, RRFImageWeight: 0.3,
		SimilarReportsThreshold: 0.91, SimilarReportsWindowDays: 8,
	}
}

func putRetrievalPatch(t *testing.T, space *types.Space, body string) *httptest.ResponseRecorder {
	t.Helper()
	engine := newSpaceHandlerTestEngine(t, space)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/spaces/kv/retrieval-config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	engine.ServeHTTP(rec, req)
	return rec
}

func TestRetrievalConfigPartialUpdatesPreserveEveryOmittedField(t *testing.T) {
	original := retrievalPatchFixture()
	encoded, err := json.Marshal(original)
	require.NoError(t, err)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(encoded, &fields))
	// Derive the cases from the config itself so new fields also receive coverage.
	for field, value := range fields {
		t.Run(field, func(t *testing.T) {
			space := secretSpaceFixture()
			cfg := *original
			space.RetrievalConfig = &cfg
			reset := "0"
			if string(value) == "true" {
				reset = "false"
			}
			body := `{"` + field + `":` + reset + `}`
			expected := *original
			require.NoError(t, json.Unmarshal([]byte(body), &expected))
			rec := putRetrievalPatch(t, space, body)
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			require.Equal(t, expected, *space.RetrievalConfig)
			var result struct {
				Data types.RetrievalConfig `json:"data"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
			require.Equal(t, expected, result.Data)
		})
	}
}

func TestRetrievalConfigEmptyPatchAndFirstUpdate(t *testing.T) {
	space := secretSpaceFixture()
	space.RetrievalConfig = retrievalPatchFixture()
	original := *space.RetrievalConfig
	rec := putRetrievalPatch(t, space, `{}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, original, *space.RetrievalConfig)
	space.RetrievalConfig = nil
	rec = putRetrievalPatch(t, space, `{"image_vector_enabled":true,"rrf_image_weight":0.4}`)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, space.RetrievalConfig.ImageVectorEnabled)
	require.Equal(t, 0.4, space.RetrievalConfig.RRFImageWeight)
}

func TestRetrievalConfigInvalidPatchDoesNotChangeStoredFields(t *testing.T) {
	for _, body := range []string{
		`null`, `[]`, `{"image_vector_enabled":null}`, `{"rrf_image_weight":null}`,
		`{"image_vector_enabled":"true"}`, `{"rrf_image_weight":11}`,
		`{"vector_threshold":-1}`, `{"similar_reports_window_days":366}`, `{`,
	} {
		t.Run(body, func(t *testing.T) {
			space := secretSpaceFixture()
			space.RetrievalConfig = retrievalPatchFixture()
			original := *space.RetrievalConfig
			rec := putRetrievalPatch(t, space, body)
			require.GreaterOrEqual(t, rec.Code, 400, rec.Body.String())
			require.Less(t, rec.Code, 500, rec.Body.String())
			require.Equal(t, original, *space.RetrievalConfig)
		})
	}
}
