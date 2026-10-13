package service

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"reliquary/internal/application/service/retriever"
	"reliquary/internal/infrastructure/docparser"
	taskqueue "reliquary/internal/queue"
	"testing"

	"github.com/stretchr/testify/require"
	filesvc "reliquary/internal/application/service/file"
	"reliquary/internal/types"
)

func (r *createEntryFileRepoStub) GetEntryByID(_ context.Context, _ uint64, _ string) (*types.Entry, error) {
	return r.createdEntry, nil
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
			repo := &createEntryFileRepoStub{}
			files := filesvc.NewLocalFileService(t.TempDir(), "")
			svc := &entryService{repo: repo, fileSvc: files, task: &createEntryTaskEnqueuerStub{}, libraryService: &createEntryFileLibraryServiceStub{library: &types.Library{ID: "library-1"}}}
			ctx := newCreateEntryFileContext()
			k, err := svc.createLegacyFileFixture(ctx, "library-1", newMultipartFileHeader(t, sample.name, string(sample.data)), nil, nil, "", "", nil)
			require.NoError(t, err)
			bodyText := "# Parsed body\n![image](resource://rewritten)"
			parsedPath, err := files.SaveBytes(ctx, []byte(bodyText), k.SpaceID, k.ID+".md", false)
			require.NoError(t, err)
			intake, err := k.IntakeMetadata()
			require.NoError(t, err)
			intake.OriginalPath = parsedPath
			require.NoError(t, k.SetIntakeMetadata(intake))
			repo.createdEntry = k
			original, name, err := svc.GetEntryFile(ctx, k.ID)
			require.NoError(t, err)
			got, err := io.ReadAll(original)
			require.NoError(t, err)
			require.NoError(t, original.Close())
			require.Equal(t, sample.name, name)
			require.Equal(t, sample.data, got)
			body, _, err := svc.GetEntryBody(ctx, k.ID)
			require.NoError(t, err)
			got, err = io.ReadAll(body)
			require.NoError(t, err)
			require.NoError(t, body.Close())
			require.Equal(t, bodyText, string(got))
		})
	}
}

func TestSimpleImageProcessingWithoutVisionKeepsOriginal(t *testing.T) {
	var imageBytes bytes.Buffer
	require.NoError(t, png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 2))))
	k := &types.Entry{ID: "image", SpaceID: 1, LibraryID: "library", Type: "file", Title: "image.png", FileName: "image.png", FileType: "png", FilePath: "input", ParseStatus: types.ParseStatusPending}
	require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: "file", Decision: "pending", SourceInfo: types.SourceInfo{Author: "Ada", OriginalURL: "https://example.com/image", PublishedAt: "2026-01-01"}}))
	repo := &intakeRepo{entry: k}
	files := filesvc.NewLocalFileService(t.TempDir(), "")
	input, err := files.SaveBytes(context.Background(), imageBytes.Bytes(), 1, "image.png", false)
	require.NoError(t, err)
	k.FilePath = input
	svc := &entryService{repo: repo, libraryService: intakeLibraryService{}, spaceRepo: intakeSpaceRepo{}, spaceService: intakeSpaceService{}, fileSvc: files, imageResolver: docparser.NewImageResolver(), modelService: intakeModelService{chat: intakeChat{response: `{"approved":true,"reason":"valid source"}`}}, chunkService: &parentChildChunkService{}, retrieveEngine: &retriever.PostgresRetrievalService{RetrieveEngineService: &parentChildRetrieveEngine{}}, task: parentChildTaskEnqueuer{}}
	payload, err := json.Marshal(types.DocumentProcessPayload{SpaceID: 1, EntryID: k.ID, LibraryID: "library", FilePath: input, FileName: "image.png", FileType: "png"})
	require.NoError(t, err)
	require.ErrorContains(t, svc.ProcessDocument(context.Background(), taskqueue.NewTask(types.TypeDocumentProcess, payload, taskqueue.MaxRetry(0))), "does not support vision")
	require.Equal(t, types.ParseStatusFailed, repo.entry.ParseStatus)
	require.Contains(t, repo.entry.ErrorMessage, "does not support vision")
	intake, err := repo.entry.IntakeMetadata()
	require.NoError(t, err)
	require.NotEmpty(t, intake.OriginalPath)
	body, err := files.GetFile(context.Background(), intake.OriginalPath)
	require.NoError(t, err)
	parsed, err := io.ReadAll(body)
	require.NoError(t, err)
	require.NoError(t, body.Close())
	require.Contains(t, string(parsed), "![image.png]")
	original, err := files.GetFile(context.Background(), repo.entry.FilePath)
	require.NoError(t, err)
	got, err := io.ReadAll(original)
	require.NoError(t, err)
	require.NoError(t, original.Close())
	require.Equal(t, imageBytes.Bytes(), got)
}
