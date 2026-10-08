package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/hibiken/asynq"
	"image"
	"image/png"
	"io"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/infrastructure/docparser"
	"testing"

	"github.com/stretchr/testify/require"
	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/types"
)

func (r *createKnowledgeFileRepoStub) GetKnowledgeByID(_ context.Context, _ uint64, _ string) (*types.Knowledge, error) {
	return r.createdKnowledge, nil
}

func TestUploadedOriginalIsSeparateFromParsedBody(t *testing.T) {
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	for _, sample := range []struct {
		name string
		data []byte
	}{
		{"image.png", imageBytes.Bytes()},
		{"source.md", []byte("# Original\r\n![image](original.png)\r\n")},
		{"source.pdf", []byte("%PDF-1.4\noriginal bytes")},
		{"source.docx", []byte("PK\x03\x04original bytes")},
	} {
		t.Run(sample.name, func(t *testing.T) {
			repo := &createKnowledgeFileRepoStub{}
			files := filesvc.NewLocalFileService(t.TempDir(), "")
			svc := &knowledgeService{repo: repo, fileSvc: files, task: &createKnowledgeTaskEnqueuerStub{}, kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}}}
			ctx := newCreateKnowledgeFileContext()
			k, err := svc.CreateKnowledgeFromFile(ctx, "kb-1", newMultipartFileHeader(t, sample.name, string(sample.data)), nil, nil, "", nil, "", nil)
			require.NoError(t, err)
			bodyText := "# Parsed body\n![image](resource://rewritten)"
			parsedPath, err := files.SaveBytes(ctx, []byte(bodyText), k.TenantID, k.ID+".md", false)
			require.NoError(t, err)
			intake, err := k.IntakeMetadata()
			require.NoError(t, err)
			intake.OriginalPath = parsedPath
			require.NoError(t, k.SetIntakeMetadata(intake))
			repo.createdKnowledge = k
			original, name, err := svc.GetKnowledgeFile(ctx, k.ID)
			require.NoError(t, err)
			got, err := io.ReadAll(original)
			require.NoError(t, err)
			require.NoError(t, original.Close())
			require.Equal(t, sample.name, name)
			require.Equal(t, sample.data, got)
			body, _, err := svc.GetKnowledgeBody(ctx, k.ID)
			require.NoError(t, err)
			got, err = io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			require.Equal(t, bodyText, string(got))
		})
	}
}

func TestSimpleImageProcessingWithoutVisionKeepsOriginal(t *testing.T) {
	for _, enableMultimodal := range []bool{false, true} {
		t.Run(fmt.Sprintf("request_multimodal_%t", enableMultimodal), func(t *testing.T) {
			var imageBytes bytes.Buffer
			require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
			k := &types.Knowledge{ID: "image", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", Title: "image.png", FileName: "image.png", FileType: "png", FilePath: "input", ParseStatus: types.ParseStatusPending}
			require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: "file", Decision: "pending", SourceInfo: types.SourceInfo{Author: "Ada", OriginalURL: "https://example.com/image", PublishedAt: "2026-01-01"}}))
			repo := &intakeRepo{knowledge: k}
			files := filesvc.NewLocalFileService(t.TempDir(), "")
			input, err := files.SaveBytes(context.Background(), imageBytes.Bytes(), 1, "image.png", false)
			require.NoError(t, err)
			k.FilePath = input
			svc := &knowledgeService{repo: repo, kbService: intakeKBService{}, tenantRepo: intakeTenantRepo{}, tenantService: intakeTenantService{}, fileSvc: files, imageResolver: docparser.NewImageResolver(), modelService: intakeModelService{chat: intakeChat{response: `{"approved":true,"reason":"valid source","natures":["描述"],"referents":[{"kind":"topic","name":"行业"}]}`}}, chunkService: &parentChildChunkService{}, retrieveEngine: &retriever.PostgresRetrievalService{RetrieveEngineService: &parentChildRetrieveEngine{}}, task: parentChildTaskEnqueuer{}}
			payload, err := json.Marshal(types.DocumentProcessPayload{TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: "kb", FilePath: input, FileName: "image.png", FileType: "png", EnableMultimodel: enableMultimodal})
			require.NoError(t, err)
			require.NoError(t, svc.ProcessDocument(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload, asynq.MaxRetry(0))))
			require.NotEqual(t, types.ParseStatusFailed, repo.knowledge.ParseStatus, repo.knowledge.ErrorMessage)
			intake, err := repo.knowledge.IntakeMetadata()
			require.NoError(t, err)
			require.NotEmpty(t, intake.OriginalPath)
			body, err := files.GetFile(context.Background(), intake.OriginalPath)
			require.NoError(t, err)
			parsed, err := io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			require.Contains(t, string(parsed), "![image.png]")
			original, err := files.GetFile(context.Background(), repo.knowledge.FilePath)
			require.NoError(t, err)
			got, err := io.ReadAll(original)
			require.NoError(t, err)
			require.NoError(t, original.Close())
			require.Equal(t, imageBytes.Bytes(), got)
		})
	}
}
