package router

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestPersonalDocumentDeleteHTTPWebAndMCP(t *testing.T) {
	for _, protocol := range []string{"http", "web", "mcp"} {
		t.Run(protocol, func(t *testing.T) {
			f := newMCPFixture(t)
			f.seedProcessingModels(t)
			router := ingestAuthHTTPRouter(f)
			body := "# Personal source\n\nMy original measurements for " + protocol + ".\n\n![photo](https://example.org/photo.png)"
			input := map[string]any{"items": []any{map[string]any{"library": "mine", "content": body, "acquisition": "verbatim", "title": "Personal source", "images": []any{map[string]any{"source_url": "https://example.org/photo.png", "data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a3ioAAAAASUVORK5CYII="}}}}}
			encoded, err := json.Marshal(input)
			require.NoError(t, err)
			var result types.IngestBatchResult
			client := f.connect(t, "2025-11-25")
			if protocol == "mcp" {
				mcpCall(t, client, "ingest", input, &result)
			} else {
				response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(encoded), f.key, true)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var envelope struct{ Data types.IngestBatchResult }
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &envelope))
				result = envelope.Data
			}
			require.Equal(t, "accepted", result.Items[0].Status, "%+v", result)
			id := result.Items[0].EntryID
			require.NotEmpty(t, id)
			require.NotEmpty(t, f.queue.tasks)
			require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[0]))
			var document types.Entry
			require.NoError(t, f.db.First(&document, "id=?", id).Error)
			require.Equal(t, "mine", document.SourceKind)
			require.Equal(t, types.ParseStatusCompleted, document.ParseStatus, document.ErrorMessage)
			filePath := filepath.Join(f.root, document.VaultPath)
			require.FileExists(t, filePath)
			attachmentPath := filepath.Join(f.root, "attachments", id)
			require.DirExists(t, attachmentPath)
			var indexCount int64
			require.NoError(t, f.db.Table("embeddings").Where("entry_id=?", id).Count(&indexCount).Error)
			require.Positive(t, indexCount)
			require.NoError(t, f.db.Create(&types.EntryProcessingSpan{EntryID: id, Attempt: 1, SpanID: "document-input", Name: "article_processing", Kind: types.SpanKindGeneration, Status: types.SpanStatusDone, Input: types.JSONMap{"body": body}}).Error)
			var spans int64
			require.NoError(t, f.db.Model(&types.EntryProcessingSpan{}).Where("entry_id=?", id).Count(&spans).Error)
			require.Positive(t, spans)
			// Deletion must remain available after the indexing model is removed.
			require.NoError(t, f.db.Model(&document).Update("embedding_model_id", "missing-embedding").Error)
			article := f.seedArticle(t, "Article deletion must fail", "full", "Article body")
			rejected := personalRequest(t, router, http.MethodDelete, "/api/v1/documents/"+article.ID, "", f.key, true)
			require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
			toolRejected, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_document", Arguments: map[string]any{"document_id": article.ID}})
			require.NoError(t, err)
			require.True(t, toolRejected.IsError)
			for _, record := range []string{"missing", ""} {
				toolMissing, err := client.CallTool(context.Background(), &mcp.CallToolParams{Name: "delete_document", Arguments: map[string]any{"document_id": record}})
				require.NoError(t, err)
				require.True(t, toolMissing.IsError)
			}
			updated := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+id, `{"title":"Changed"}`, f.key, true)
			require.Equal(t, http.StatusOK, updated.Code, updated.Body.String())
			var edited types.Entry
			require.NoError(t, f.db.First(&edited, "id=?", id).Error)
			require.Equal(t, "Changed", edited.Title)
			switch protocol {
			case "mcp":
				var removed struct {
					DocumentID string `json:"document_id"`
					Status     string `json:"status"`
				}
				mcpCall(t, client, "delete_document", map[string]any{"document_id": id}, &removed)
				require.Equal(t, id, removed.DocumentID)
				require.Equal(t, "deleted", removed.Status)
			case "web":
				response := personalRequest(t, router, http.MethodDelete, "/api/v1/entries/"+id, "", f.token, false)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			default:
				response := personalRequest(t, router, http.MethodDelete, "/api/v1/documents/"+id, "", f.key, true)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			}
			_, err = os.Stat(filePath)
			require.ErrorIs(t, err, os.ErrNotExist)
			_, err = os.Stat(attachmentPath)
			require.ErrorIs(t, err, os.ErrNotExist)
			for _, table := range []string{"entries", "chunks", "embeddings", "article_embeddings", "image_embeddings", "entry_entities", "entry_topics", "entry_processing_spans", "ingest_assets"} {
				column := "entry_id"
				if table == "entries" {
					column = "id"
				}
				var count int64
				require.NoError(t, f.db.Unscoped().Table(table).Where(column+"=?", id).Count(&count).Error, table)
				require.Zero(t, count, table)
			}
			var discards int64
			require.NoError(t, f.db.Table("ingest_discards").Where("entry_id=?", id).Count(&discards).Error)
			require.EqualValues(t, 1, discards)
			var log types.OperationLog
			require.NoError(t, f.db.Where("operation_type=? AND object_id=?", "delete_document", id).First(&log).Error)
			require.Contains(t, string(log.Before), "mine")
			require.NotEmpty(t, log.Actor)
			response := personalRequest(t, router, http.MethodPost, "/api/v1/ingest", string(encoded), f.key, true)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var reimport struct{ Data types.IngestBatchResult }
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &reimport))
			require.Equal(t, "discarded", reimport.Data.Items[0].Status)
			require.Equal(t, id, reimport.Data.Items[0].EntryID)
		})
	}
}
