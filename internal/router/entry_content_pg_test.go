package router

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestEntryContentUpdateHTTPAndMCP(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	router := ingestAuthHTTPRouter(f)
	client := f.connect(t, "2025-11-25")
	request := &types.IngestRequest{Library: "official", Title: "Editable source", URL: "https://example.org/editable", Publisher: "Publisher", Acquisition: "verbatim", Content: "# Original\n\nolduniquemarker describes the original measurements."}
	first, err := f.articles.Ingest(f.ctx, request)
	require.NoError(t, err)
	processor := f.articles.(interfaces.EntryService)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var original types.Entry
	require.NoError(t, f.db.First(&original, "id=?", first.EntryID).Error)
	require.Equal(t, types.ParseStatusCompleted, original.ParseStatus)
	require.NoError(t, f.db.Model(&original).Update("starred", true).Error)
	newBody := "# Corrected\n\nnewuniquemarker describes corrected measurements."
	invalid, err := json.Marshal(map[string]string{"content": newBody + "\n\n![missing](../attachments/" + original.ID + "/missing.png)"})
	require.NoError(t, err)
	rejected := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(invalid), f.key, true)
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	encoded, err := json.Marshal(map[string]string{"content": newBody, "acquisition": "extracted"})
	require.NoError(t, err)
	response := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(encoded), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	var result struct{ Data types.IngestResult }
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
	require.Equal(t, original.ID, result.Data.EntryID)
	require.Equal(t, "accepted", result.Data.Status)
	var pending types.Entry
	require.NoError(t, f.db.First(&pending, "id=?", original.ID).Error)
	require.Equal(t, newBody, pending.VaultBody)
	require.Equal(t, original.VaultPath, pending.VaultPath)
	require.Equal(t, original.Source, pending.Source)
	require.Equal(t, original.CreatedAt, pending.CreatedAt)
	require.True(t, pending.Starred)
	require.Equal(t, "extracted", pending.Acquisition)
	require.Empty(t, pending.Description)
	intake, err := pending.IntakeMetadata()
	require.NoError(t, err)
	require.Empty(t, intake.OriginalPath, "published replacements use the vault without another uploaded body copy")
	for _, table := range []string{"embeddings", "article_embeddings", "image_embeddings", "entry_processing_outputs"} {
		var count int64
		require.NoError(t, f.db.Table(table).Where("entry_id=?", original.ID).Count(&count).Error)
		require.Zero(t, count, table)
	}
	// Reject another body while the replacement is still being processed.
	response = personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", `{"content":"Another replacement"}`, f.key, true)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var updated types.Entry
	require.NoError(t, f.db.First(&updated, "id=?", original.ID).Error)
	require.Equal(t, types.ParseStatusCompleted, updated.ParseStatus, updated.ErrorMessage)
	var chunks []types.Chunk
	require.NoError(t, f.db.Where("entry_id=?", original.ID).Find(&chunks).Error)
	chunkData, err := json.Marshal(chunks)
	require.NoError(t, err)
	require.Contains(t, string(chunkData), "newuniquemarker")
	require.NotContains(t, string(chunkData), "olduniquemarker")
	data, err := os.ReadFile(filepath.Join(f.root, updated.VaultPath))
	require.NoError(t, err)
	require.Contains(t, string(data), newBody)
	var unchanged types.IngestResult
	mcpCall(t, client, "update_entry_content", map[string]any{"entry_id": original.ID, "content": newBody}, &unchanged)
	require.Equal(t, "exists", unchanged.Status)
	var changed types.IngestResult
	mcpCall(t, client, "update_entry_content", map[string]any{"entry_id": original.ID, "content": newBody + "\n\nMCP correction."}, &changed)
	require.Equal(t, "accepted", changed.Status)
	require.Equal(t, original.ID, changed.EntryID)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	// A source_ref still points to the same record after explicit replacement.
	personal := &types.IngestRequest{Library: "personal", SourceRef: "notes/editable.md", Title: "Editable note", Content: "Original personal measurements.", Acquisition: "verbatim"}
	own, err := f.articles.Ingest(f.ctx, personal)
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	personal.Content = "Corrected personal measurements."
	_, err = f.articles.UpdateEntryContent(f.ctx, own.EntryID, personal.Content, "")
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	again, err := f.articles.Ingest(f.ctx, personal)
	require.NoError(t, err)
	require.Equal(t, "exists", again.Status)
	require.Equal(t, own.EntryID, again.EntryID)
	// Generated material reuses the report indexer and updates its idempotency key.
	report := &types.IngestRequest{Library: "ai-output", Title: "Editable report", Content: "Original generated analysis.", Acquisition: "model_written"}
	generated, err := f.articles.Ingest(f.ctx, report)
	require.NoError(t, err)
	report.Content = "Corrected generated analysis."
	_, err = f.articles.UpdateEntryContent(f.ctx, generated.EntryID, report.Content, "")
	require.NoError(t, err)
	task := f.queue.tasks[len(f.queue.tasks)-1]
	require.Equal(t, types.TypeReportIndex, task.Type())
	require.NoError(t, f.documents.HandleReportIndex(f.ctx, task))
	again, err = f.articles.Ingest(f.ctx, report)
	require.NoError(t, err)
	require.Equal(t, "exists", again.Status)
	require.Equal(t, generated.EntryID, again.EntryID)
}

func TestEntryContentUpdateEnqueueFailureRestoresOriginal(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	router := ingestAuthHTTPRouter(f)
	first, err := f.articles.Ingest(f.ctx, &types.IngestRequest{Library: "official", Title: "Rollback replacement", URL: "https://example.org/rollback-replacement", Publisher: "Publisher", Acquisition: "verbatim", Content: "# Original\n\nThe saved original must remain intact."})
	require.NoError(t, err)
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var original types.Entry
	require.NoError(t, f.db.First(&original, "id=?", first.EntryID).Error)
	before, err := os.ReadFile(filepath.Join(f.root, original.VaultPath))
	require.NoError(t, err)
	var oldChunks int64
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("entry_id=?", original.ID).Count(&oldChunks).Error)
	require.Positive(t, oldChunks)
	f.queue.enqueueErr = errors.New("fixture queue unavailable")
	response := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", `{"content":"# Replacement\n\nThis must roll back.","acquisition":"extracted"}`, f.key, true)
	require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
	var retained types.Entry
	require.NoError(t, f.db.First(&retained, "id=?", original.ID).Error)
	require.Equal(t, original.VaultBody, retained.VaultBody)
	require.Equal(t, original.Metadata, retained.Metadata)
	require.Equal(t, original.ParseStatus, retained.ParseStatus)
	require.Equal(t, original.StructuredSummary, retained.StructuredSummary)
	var retainedChunks int64
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("entry_id=?", original.ID).Count(&retainedChunks).Error)
	require.Equal(t, oldChunks, retainedChunks)
	after, err := os.ReadFile(filepath.Join(f.root, original.VaultPath))
	require.NoError(t, err)
	require.Equal(t, before, after)
	var logs int64
	require.NoError(t, f.db.Table("operation_logs").Where("object_id=? AND after_value->>'action'=?", original.ID, "update_content").Count(&logs).Error)
	require.Zero(t, logs)
}
