package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	taskqueue "reliquary/internal/queue"

	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type createEntryFileRepoStub struct {
	interfaces.EntryRepository

	createCalls  int
	createErr    error
	createdEntry *types.Entry
}

func (r *createEntryFileRepoStub) CheckEntryExists(
	ctx context.Context,
	spaceID uint64,
	libraryID string,
	params *types.EntryCheckParams,
) (bool, *types.Entry, error) {
	return false, nil, nil
}

func (r *createEntryFileRepoStub) CreateEntry(ctx context.Context, entry *types.Entry) error {
	r.createCalls++
	copied := *entry
	r.createdEntry = &copied
	return r.createErr
}

func (s *createEntryFileLibraryServiceStub) GetLibraryByID(
	ctx context.Context,
	id string,
) (*types.Library, error) {
	return s.library, nil
}

type createEntryFileServiceStub struct {
	saveErr          error
	saveCalls        int
	savedWithEntryID string
	deleteCalls      int
	deletedPath      string
}

func (s *createEntryFileServiceStub) CheckConnectivity(ctx context.Context) error {
	return nil
}

func (s *createEntryFileServiceStub) SaveFile(
	ctx context.Context,
	file *multipart.FileHeader,
	spaceID uint64,
	entryID string,
) (string, error) {
	s.saveCalls++
	s.savedWithEntryID = entryID
	if s.saveErr != nil {
		return "", s.saveErr
	}
	return "stored/" + entryID, nil
}

func (s *createEntryFileServiceStub) SaveBytes(
	ctx context.Context,
	data []byte,
	spaceID uint64,
	fileName string,
	temp bool,
) (string, error) {
	return "", errors.New("not implemented")
}

func (s *createEntryFileServiceStub) GetFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *createEntryFileServiceStub) GetFileURL(ctx context.Context, filePath string) (string, error) {
	return "", errors.New("not implemented")
}

func (s *createEntryFileServiceStub) DeleteFile(ctx context.Context, filePath string) error {
	s.deleteCalls++
	s.deletedPath = filePath
	return nil
}

func (s *createEntryFileServiceStub) CopyFile(ctx context.Context, srcPath string, spaceID uint64, entryID string) (string, error) {
	return "", errors.New("not implemented")
}

type createEntryTaskEnqueuerStub struct {
	calls int
}

func (s *createEntryTaskEnqueuerStub) Enqueue(
	task *taskqueue.Task,
	opts ...taskqueue.Option,
) (*taskqueue.TaskInfo, error) {
	s.calls++
	return &taskqueue.TaskInfo{ID: "task-1", Queue: "default"}, nil
}

func newCreateEntryFileContext() context.Context {
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, &types.Space{})
	return ctx
}

func newMultipartFileHeader(t *testing.T, filename string, content string) *multipart.FileHeader {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, err = part.Write([]byte(content))
	require.NoError(t, err)
	require.NoError(t, writer.Close())

	req := httptest.NewRequest("POST", "/", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	require.NoError(t, req.ParseMultipartForm(1024))
	return req.MultipartForm.File["file"][0]
}

type createEntryFileLibraryServiceStub struct {
	interfaces.LibraryService

	library *types.Library
}
