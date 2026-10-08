package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"

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
	repo := repository.NewKnowledgeRepository(db)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	source := types.SourceInfo{Author: "Ada", OriginalURL: "https://example.com/sample.pdf", PublishedAt: "2026-01-01"}
	knowledge := &types.Knowledge{
		ID: uuid.NewString(), TenantID: types.SingleTenantID, KnowledgeBaseID: "kb",
		Type: "file", Title: "sample.pdf", FileName: "sample.pdf", FileType: "pdf", FilePath: "input",
		ParseStatus: types.ParseStatusPending, EnableStatus: "disabled",
	}
	require.NoError(t, knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{
		Source: "file", Decision: "pending", SourceInfo: source,
	}))
	require.NoError(t, repo.CreateKnowledge(ctx, knowledge))
	files := &intakeFileService{saved: map[string][]byte{"input": []byte("%PDF-1.4\n")}}
	svc := &knowledgeService{
		db: db, repo: repo, kbService: intakeKBService{}, tenantRepo: intakeTenantRepo{}, fileSvc: files,
	}
	payload, err := json.Marshal(types.DocumentProcessPayload{
		TenantID: types.SingleTenantID, KnowledgeID: knowledge.ID, KnowledgeBaseID: knowledge.KnowledgeBaseID,
		FilePath: knowledge.FilePath, FileName: knowledge.FileName, FileType: knowledge.FileType,
	})
	require.NoError(t, err)
	require.NoError(t, svc.ProcessDocument(ctx, asynq.NewTask(types.TypeDocumentProcess, payload, asynq.MaxRetry(0))))

	// Reload through the same service used by the knowledge detail API.
	loaded, err := svc.GetKnowledgeByID(ctx, knowledge.ID)
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
