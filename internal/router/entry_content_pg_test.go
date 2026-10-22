package router

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"reliquary/internal/infrastructure/docparser"
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
	body := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+original.ID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, body.Code)
	baseHash := strings.Trim(body.Header().Get("ETag"), "\"")
	require.Len(t, baseHash, 64)
	newBody := "# Corrected\n\nnewuniquemarker describes corrected measurements."
	invalid, err := json.Marshal(map[string]string{"content": newBody + "\n\n![missing](../attachments/" + original.ID + "/missing.png)"})
	require.NoError(t, err)
	rejected := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(invalid), f.key, true)
	require.Equal(t, http.StatusBadRequest, rejected.Code, rejected.Body.String())
	encoded, err := json.Marshal(map[string]string{"content": newBody, "acquisition": "extracted", "base_body_hash": baseHash})
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
	// A stale editor cannot overwrite the pending replacement.
	response = personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", `{"content":"Another replacement","base_body_hash":"`+baseHash+`"}`, f.key, true)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var updated types.Entry
	require.NoError(t, f.db.First(&updated, "id=?", original.ID).Error)
	require.Equal(t, types.ParseStatusCompleted, updated.ParseStatus, updated.ErrorMessage)
	taskCount := len(f.queue.tasks)
	stale, err := json.Marshal(types.EntryContentUpdate{Content: "A stale editor must not overwrite this entry.", BaseBodyHash: baseHash})
	require.NoError(t, err)
	conflict := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(stale), f.key, true)
	require.Equal(t, http.StatusConflict, conflict.Code, conflict.Body.String())
	require.Contains(t, conflict.Body.String(), `"reason":"body_changed"`)
	var retained types.Entry
	require.NoError(t, f.db.First(&retained, "id=?", original.ID).Error)
	require.Equal(t, updated.VaultBody, retained.VaultBody)
	require.Equal(t, updated.ParseStatus, retained.ParseStatus)
	require.Len(t, f.queue.tasks, taskCount)
	// A retry whose first response was lost still succeeds without processing twice.
	response = personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(encoded), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.Contains(t, response.Body.String(), `"status":"exists"`)
	require.Len(t, f.queue.tasks, taskCount)
	var chunks []types.Chunk
	require.NoError(t, f.db.Where("entry_id=?", original.ID).Find(&chunks).Error)
	chunkData, err := json.Marshal(chunks)
	require.NoError(t, err)
	require.Contains(t, string(chunkData), "newuniquemarker")
	require.NotContains(t, string(chunkData), "olduniquemarker")
	updatedBody := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+original.ID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, updatedBody.Code)
	require.Equal(t, newBody, updatedBody.Body.String())
	var unchanged types.IngestResult
	mcpCall(t, client, "update_entry_content", map[string]any{"entry_id": original.ID, "content": newBody}, &unchanged)
	require.Equal(t, "exists", unchanged.Status)
	var changed types.IngestResult
	mcpCall(t, client, "update_entry_content", map[string]any{"entry_id": original.ID, "content": newBody + "\n\nMCP correction."}, &changed)
	require.Equal(t, "accepted", changed.Status)
	require.Equal(t, original.ID, changed.EntryID)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	// Update Markdown and an uploaded image together, using its original relative target.
	imageBody := newBody + "\n\n![Figure](figures/chart.png)"
	firstImage := entryContentPNG(t, 40)
	asset, err := f.articles.UploadIngestAsset(f.ctx, "chart.png", firstImage)
	require.NoError(t, err)
	encoded, err = json.Marshal(types.EntryContentUpdate{Content: imageBody, Images: []types.IngestImage{{SourceURL: "figures/chart.png", AssetID: asset.AssetID}}})
	require.NoError(t, err)
	response = personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(encoded), f.key, true)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	require.NoError(t, f.db.First(&updated, "id=?", original.ID).Error)
	refs := docparser.MarkdownImageReferences(updated.VaultBody)
	require.Len(t, refs, 1)
	oldRef := refs[0].URL
	reader, err := f.vault.ReadImage(&updated, oldRef)
	require.NoError(t, err)
	picture, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, firstImage, picture)
	firstImageBody, firstImageHash := updated.VaultBody, updated.ContentHash
	var claimed types.IngestAsset
	require.NoError(t, f.db.First(&claimed, "id=?", asset.AssetID).Error)
	require.Equal(t, original.ID, claimed.EntryID)
	// Identical Markdown with new image bytes is a change; replaying it is not.
	secondImage := entryContentPNG(t, 180)
	imageUpdate := map[string]any{"entry_id": original.ID, "content": imageBody, "images": []any{map[string]any{"source_url": "figures/chart.png", "data": base64.StdEncoding.EncodeToString(secondImage)}}}
	mcpCall(t, client, "update_entry_content", imageUpdate, &changed)
	require.Equal(t, "accepted", changed.Status)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	require.NoError(t, f.db.First(&updated, "id=?", original.ID).Error)
	refs = docparser.MarkdownImageReferences(updated.VaultBody)
	require.Len(t, refs, 1)
	require.NotEqual(t, oldRef, refs[0].URL)
	reader, err = f.vault.ReadImage(&updated, refs[0].URL)
	require.NoError(t, err)
	picture, err = io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, secondImage, picture)
	reader, err = f.vault.ReadImage(&updated, oldRef)
	require.NoError(t, err, "replacing a picture must not overwrite its previous attachment")
	require.NoError(t, reader.Close())
	tasks := len(f.queue.tasks)
	mcpCall(t, client, "update_entry_content", imageUpdate, &unchanged)
	require.Equal(t, "exists", unchanged.Status)
	require.Len(t, f.queue.tasks, tasks)
	attachments, err := os.ReadDir(filepath.Join(f.root, "attachments", original.ID))
	require.NoError(t, err)
	require.Len(t, attachments, 2, "same-image retries reuse the existing attachment")
	_, err = f.articles.UpdateEntryContent(f.ctx, original.ID, &types.EntryContentUpdate{Content: firstImageBody})
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	require.NoError(t, f.db.First(&updated, "id=?", original.ID).Error)
	require.Equal(t, firstImageHash, updated.ContentHash, "restoring an older attachment preserves its byte-based fingerprint")
	// A source_ref still points to the same record after explicit replacement.
	personal := &types.IngestRequest{Library: "personal", SourceRef: "notes/editable.md", Title: "Editable note", Content: "Original personal measurements.", Acquisition: "verbatim"}
	own, err := f.articles.Ingest(f.ctx, personal)
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	personal.Content = "Corrected personal measurements."
	_, err = f.articles.UpdateEntryContent(f.ctx, own.EntryID, &types.EntryContentUpdate{Content: personal.Content})
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	again, err := f.articles.Ingest(f.ctx, personal)
	require.NoError(t, err)
	require.Equal(t, "exists", again.Status)
	require.Equal(t, own.EntryID, again.EntryID)
	// Generated material uses the same background processing path.
	report := &types.IngestRequest{Library: "ai-output", SourceRef: "editable-report", Title: "Editable report", Content: "Original generated analysis.", Acquisition: "model_written"}
	generated, err := f.articles.Ingest(f.ctx, report)
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var initialReport types.Entry
	require.NoError(t, f.db.First(&initialReport, "id=?", generated.EntryID).Error)
	require.Equal(t, types.ParseStatusCompleted, initialReport.ParseStatus, initialReport.ErrorMessage)
	report.Content = "Corrected generated analysis."
	_, err = f.articles.UpdateEntryContent(f.ctx, generated.EntryID, &types.EntryContentUpdate{Content: report.Content})
	require.NoError(t, err)
	task := f.queue.tasks[len(f.queue.tasks)-1]
	require.Equal(t, types.TypeManualProcess, task.Type())
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, task))
	again, err = f.articles.Ingest(f.ctx, report)
	require.NoError(t, err)
	require.Equal(t, "exists", again.Status)
	require.Equal(t, generated.EntryID, again.EntryID)
	generatedUpdate := &types.EntryContentUpdate{Content: "# Generated analysis\n\n![Chart](chart.png)", Images: []types.IngestImage{{SourceURL: "chart.png", Data: base64.StdEncoding.EncodeToString(secondImage)}}}
	_, err = f.articles.UpdateEntryContent(f.ctx, generated.EntryID, generatedUpdate)
	require.NoError(t, err)
	require.NoError(t, processor.ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	again, err = f.articles.UpdateEntryContent(f.ctx, generated.EntryID, generatedUpdate)
	require.NoError(t, err)
	require.Equal(t, "exists", again.Status)
}

func TestEntryContentUpdateEnqueueFailureRestoresOriginal(t *testing.T) {
	f := newMCPFixture(t)
	f.seedProcessingModels(t)
	router := ingestAuthHTTPRouter(f)
	first, err := f.articles.Ingest(f.ctx, &types.IngestRequest{Library: "official", Title: "Rollback replacement", URL: "https://example.org/rollback-replacement", Publisher: "Publisher", Acquisition: "verbatim", Content: "# Original\n\nThe saved original must remain intact.\n\n![Figure](https://example.org/original.png)", Images: []types.IngestImage{{SourceURL: "https://example.org/original.png", Data: base64.StdEncoding.EncodeToString(entryContentPNG(t, 20))}}})
	require.NoError(t, err)
	require.NoError(t, f.articles.(interfaces.EntryService).ProcessManualUpdate(f.ctx, f.queue.tasks[len(f.queue.tasks)-1]))
	var original types.Entry
	require.NoError(t, f.db.First(&original, "id=?", first.EntryID).Error)
	before := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+original.ID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, before.Code)
	var oldChunks int64
	require.NoError(t, f.db.Model(&types.Chunk{}).Where("entry_id=?", original.ID).Count(&oldChunks).Error)
	require.Positive(t, oldChunks)
	beforeImages, err := filepath.Glob(filepath.Join(f.root, "attachments", original.ID, "*"))
	require.NoError(t, err)
	require.Len(t, beforeImages, 1)
	asset, err := f.articles.UploadIngestAsset(f.ctx, "replacement.png", entryContentPNG(t, 210))
	require.NoError(t, err)
	f.queue.enqueueErr = errors.New("fixture queue unavailable")
	encoded, err := json.Marshal(types.EntryContentUpdate{Content: "# Replacement\n\nThis must roll back.\n\n![Figure](figures/replacement.png)", Acquisition: "extracted", Images: []types.IngestImage{{SourceURL: "figures/replacement.png", AssetID: asset.AssetID}}})
	require.NoError(t, err)
	response := personalRequest(t, router, http.MethodPut, "/api/v1/entries/"+original.ID+"/body", string(encoded), f.key, true)
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
	after := personalRequest(t, router, http.MethodGet, "/api/v1/entries/"+original.ID+"/body", "", f.key, true)
	require.Equal(t, http.StatusOK, after.Code)
	require.Equal(t, before.Body.String(), after.Body.String())
	afterImages, err := filepath.Glob(filepath.Join(f.root, "attachments", original.ID, "*"))
	require.NoError(t, err)
	require.Equal(t, beforeImages, afterImages, "rollback removes only the new attachment")
	var unclaimed types.IngestAsset
	require.NoError(t, f.db.First(&unclaimed, "id=?", asset.AssetID).Error)
	require.Empty(t, unclaimed.EntryID)
	require.Nil(t, unclaimed.ReferencedAt)
	upload, err := f.files.GetFile(f.ctx, unclaimed.Path)
	require.NoError(t, err, "failed updates must retain the caller's uploaded source asset for retry")
	require.NoError(t, upload.Close())
	var logs int64
	require.NoError(t, f.db.Table("operation_logs").Where("object_id=? AND after_value->>'action'=?", original.ID, "update_content").Count(&logs).Error)
	require.Zero(t, logs)
}

func entryContentPNG(t *testing.T, red uint8) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 1, 1))
	picture.SetRGBA(0, 0, color.RGBA{R: red, A: 255})
	var data bytes.Buffer
	require.NoError(t, png.Encode(&data, picture))
	return data.Bytes()
}
