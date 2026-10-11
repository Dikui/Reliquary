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
	kbID string,
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

func (s *createEntryFileKBServiceStub) GetKnowledgeBaseByID(
	ctx context.Context,
	id string,
) (*types.KnowledgeBase, error) {
	return s.kb, nil
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

func TestCreateEntryFromFileDoesNotPersistWhenStorageSaveFails(t *testing.T) {
	t.Parallel()

	repo := &createEntryFileRepoStub{}
	fileSvc := &createEntryFileServiceStub{saveErr: errors.New("storage unavailable")}
	svc := &entryService{
		repo:      repo,
		kbService: &createEntryFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
	}

	entry, err := svc.CreateEntryFromFile(
		newCreateEntryFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.Error(t, err)
	require.Nil(t, entry)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Zero(t, repo.createCalls)
}

func TestCreateEntryFromFilePersistsStoredFilePathOnCreate(t *testing.T) {
	t.Parallel()

	repo := &createEntryFileRepoStub{}
	fileSvc := &createEntryFileServiceStub{}
	task := &createEntryTaskEnqueuerStub{}
	svc := &entryService{
		repo:      repo,
		kbService: &createEntryFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
		task:      task,
	}

	entry, err := svc.CreateEntryFromFile(
		newCreateEntryFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.NotEmpty(t, fileSvc.savedWithEntryID)
	require.Equal(t, fileSvc.savedWithEntryID, entry.ID)
	require.Equal(t, 1, repo.createCalls)
	require.NotNil(t, repo.createdEntry)
	require.Equal(t, "stored/"+entry.ID, repo.createdEntry.FilePath)
	require.Equal(t, 1, task.calls)
}

func TestCreateEntryFromImageUsesLocalStorage(t *testing.T) {
	t.Parallel()

	repo := &createEntryFileRepoStub{}
	fileSvc := &createEntryFileServiceStub{}
	task := &createEntryTaskEnqueuerStub{}
	kb := &types.KnowledgeBase{
		ID: "kb-1",
	}
	svc := &entryService{
		repo:      repo,
		kbService: &createEntryFileKBServiceStub{kb: kb},
		fileSvc:   fileSvc,
		task:      task,
	}

	entry, err := svc.CreateEntryFromFile(
		newCreateEntryFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "image.png", "image bytes"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Equal(t, 1, repo.createCalls)
	require.Equal(t, 1, task.calls)
}

func TestCreateEntryFromFileDeletesStoredFileWhenCreateFails(t *testing.T) {
	t.Parallel()

	repo := &createEntryFileRepoStub{createErr: errors.New("database unavailable")}
	fileSvc := &createEntryFileServiceStub{}
	svc := &entryService{
		repo:      repo,
		kbService: &createEntryFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
	}

	entry, err := svc.CreateEntryFromFile(
		newCreateEntryFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.EqualError(t, err, "database unavailable")
	require.Nil(t, entry)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Equal(t, 1, repo.createCalls)
	require.Equal(t, 1, fileSvc.deleteCalls)
	require.Equal(t, "stored/"+fileSvc.savedWithEntryID, fileSvc.deletedPath)
}

func TestCreateEntryFromFile_PersistsProcessOverrides(t *testing.T) {
	t.Parallel()

	repo := &createEntryFileRepoStub{}
	fileSvc := &createEntryFileServiceStub{}
	task := &createEntryTaskEnqueuerStub{}
	svc := &entryService{
		repo:      repo,
		kbService: &createEntryFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
		task:      task,
	}

	chunkSize := 512
	overrides := &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: chunkSize},
	}

	entry, err := svc.CreateEntryFromFile(
		newCreateEntryFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		map[string]string{"source": "test"},
		nil,
		"",
		"",
		overrides,
	)

	require.NoError(t, err)
	require.NotNil(t, entry)
	require.Equal(t, 1, repo.createCalls)
	require.NotNil(t, repo.createdEntry)

	parsed, err := repo.createdEntry.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.NotNil(t, parsed.ChunkingConfig)
	require.Equal(t, chunkSize, parsed.ChunkingConfig.ChunkSize)

	metadataMap, err := repo.createdEntry.Metadata.Map()
	require.NoError(t, err)
	require.Equal(t, "test", metadataMap["source"])
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

type createEntryFileKBServiceStub struct {
	interfaces.KnowledgeBaseService

	kb *types.KnowledgeBase
}
