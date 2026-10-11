package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"

	"reliquary/internal/application/repository"
	"reliquary/internal/infrastructure/docparser/anydoc"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

func TestPersonalPDFParserUnavailableErrorPersistsForAPI(t *testing.T) {
	if anydoc.Available() {
		t.Skip("this test covers a binary built without the anydoc binding")
	}
	db := pgtest.Open(t)
	repo := repository.NewEntryRepository(db)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, types.SingleSpaceID)
	source := types.SourceInfo{Author: "Ada", OriginalURL: "https://example.com/sample.pdf", PublishedAt: "2026-01-01"}
	entry := &types.Entry{
		ID: uuid.NewString(), SpaceID: types.SingleSpaceID, KnowledgeBaseID: "kb",
		Type: "file", Title: "sample.pdf", FileName: "sample.pdf", FileType: "pdf", FilePath: "input",
		ParseStatus: types.ParseStatusPending, EnableStatus: "disabled",
	}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
		Source: "file", Decision: "pending", SourceInfo: source,
	}))
	require.NoError(t, repo.CreateEntry(ctx, entry))
	files := &intakeFileService{saved: map[string][]byte{"input": []byte("%PDF-1.4\n")}}
	svc := &entryService{
		db: db, repo: repo, kbService: intakeKBService{}, spaceRepo: intakeSpaceRepo{}, fileSvc: files,
	}
	payload, err := json.Marshal(types.DocumentProcessPayload{
		SpaceID: types.SingleSpaceID, EntryID: entry.ID, KnowledgeBaseID: entry.KnowledgeBaseID,
		FilePath: entry.FilePath, FileName: entry.FileName, FileType: entry.FileType,
	})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessDocument(ctx, taskqueue.NewTask(types.TypeDocumentProcess, payload, taskqueue.MaxRetry(0))))

	// Reload through the same service used by the entry detail API.
	loaded, err := svc.GetEntryByID(ctx, entry.ID)
	require.NoError(t, err)
	require.Equal(t, types.ParseStatusFailed, loaded.ParseStatus)
	require.Contains(t, loaded.ErrorMessage, "anydoc")
	require.Contains(t, loaded.ErrorMessage, "-tags anydoc")
	intake, err := loaded.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "error", intake.Decision)
	require.Equal(t, loaded.ErrorMessage, intake.Reason)
	require.Equal(t, source, intake.SourceInfo)
	require.Empty(t, loaded.FilePath)
	require.Empty(t, intake.OriginalPath)
	require.Empty(t, files.saved)

	response, err := json.Marshal(loaded)
	require.NoError(t, err)
	var body map[string]any
	require.NoError(t, json.Unmarshal(response, &body))
	require.Contains(t, body["error_message"], "anydoc")
	require.Contains(t, body["error_message"], "-tags anydoc")
	t.Logf("persisted API parse error: %s", loaded.ErrorMessage)
}
