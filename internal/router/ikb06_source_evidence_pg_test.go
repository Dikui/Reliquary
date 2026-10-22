package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
)

func TestOptionalProvenanceAndExistingUnverifiedFlagHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	router := ingestAuthHTTPRouter(f)
	client := f.connect(t, "2025-11-25")
	for _, protocol := range []string{"http", "mcp"} {
		request := map[string]any{"items": []any{map[string]any{"library": "official", "publisher": "Institute", "published_at": "2026-10-01", "title": "Unlinked " + protocol, "content": "Complete original institutional evidence " + protocol, "acquisition": "verbatim"}}}
		var batch types.IngestBatchResult
		if protocol == "mcp" {
			mcpCall(t, client, "ingest", request, &batch)
		} else {
			data, err := json.Marshal(request)
			require.NoError(t, err)
			response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(data), f.key, true)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var envelope struct{ Data types.IngestBatchResult }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
			batch = envelope.Data
		}
		require.Equal(t, "accepted", batch.Items[0].Status)
		require.False(t, batch.Items[0].SourceUnverified)
		require.NoError(t, f.db.Model(&types.Entry{}).Where("id=?", batch.Items[0].EntryID).Update("source_unverified", true).Error)
		detail := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+batch.Items[0].EntryID, "", f.key, true)
		require.Equal(t, http.StatusOK, detail.Code, detail.Body.String())
		var header struct {
			Data struct {
				SourceUnverified bool `json:"source_unverified"`
			}
		}
		require.NoError(t, json.Unmarshal(detail.Body.Bytes(), &header))
		require.True(t, header.Data.SourceUnverified)
		var article map[string]any
		mcpCall(t, client, "get_entry", map[string]any{"id": batch.Items[0].EntryID}, &article)
		require.Equal(t, true, article["source_unverified"])
	}
}
