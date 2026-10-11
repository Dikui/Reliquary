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

type createKnowledgeFileRepoStub struct {
	interfaces.KnowledgeRepository

	createCalls      int
	createErr        error
	createdKnowledge *types.Knowledge
}

func (r *createKnowledgeFileRepoStub) CheckKnowledgeExists(
	ctx context.Context,
	spaceID uint64,
	kbID string,
	params *types.KnowledgeCheckParams,
) (bool, *types.Knowledge, error) {
	return false, nil, nil
}

func (r *createKnowledgeFileRepoStub) CreateKnowledge(ctx context.Context, knowledge *types.Knowledge) error {
	r.createCalls++
	copied := *knowledge
	r.createdKnowledge = &copied
	return r.createErr
}

func (s *createKnowledgeFileKBServiceStub) GetKnowledgeBaseByID(
	ctx context.Context,
	id string,
) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type createKnowledgeFileServiceStub struct {
	saveErr              error
	saveCalls            int
	savedWithKnowledgeID string
	deleteCalls          int
	deletedPath          string
}

func (s *createKnowledgeFileServiceStub) CheckConnectivity(ctx context.Context) error {
	return nil
}

func (s *createKnowledgeFileServiceStub) SaveFile(
	ctx context.Context,
	file *multipart.FileHeader,
	spaceID uint64,
	knowledgeID string,
) (string, error) {
	s.saveCalls++
	s.savedWithKnowledgeID = knowledgeID
	if s.saveErr != nil {
		return "", s.saveErr
	}
	return "stored/" + knowledgeID, nil
}

func (s *createKnowledgeFileServiceStub) SaveBytes(
	ctx context.Context,
	data []byte,
	spaceID uint64,
	fileName string,
	temp bool,
) (string, error) {
	return "", errors.New("not implemented")
}

func (s *createKnowledgeFileServiceStub) GetFile(ctx context.Context, filePath string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func (s *createKnowledgeFileServiceStub) GetFileURL(ctx context.Context, filePath string) (string, error) {
	return "", errors.New("not implemented")
}

func (s *createKnowledgeFileServiceStub) DeleteFile(ctx context.Context, filePath string) error {
	s.deleteCalls++
	s.deletedPath = filePath
	return nil
}

func (s *createKnowledgeFileServiceStub) CopyFile(ctx context.Context, srcPath string, spaceID uint64, knowledgeID string) (string, error) {
	return "", errors.New("not implemented")
}

type createKnowledgeTaskEnqueuerStub struct {
	calls int
}

func (s *createKnowledgeTaskEnqueuerStub) Enqueue(
	task *taskqueue.Task,
	opts ...taskqueue.Option,
) (*taskqueue.TaskInfo, error) {
	s.calls++
	return &taskqueue.TaskInfo{ID: "task-1", Queue: "default"}, nil
}

func TestCreateKnowledgeFromFileDoesNotPersistWhenStorageSaveFails(t *testing.T) {
	t.Parallel()

	repo := &createKnowledgeFileRepoStub{}
	fileSvc := &createKnowledgeFileServiceStub{saveErr: errors.New("storage unavailable")}
	svc := &knowledgeService{
		repo:      repo,
		kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
	}

	knowledge, err := svc.CreateKnowledgeFromFile(
		newCreateKnowledgeFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.Error(t, err)
	require.Nil(t, knowledge)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Zero(t, repo.createCalls)
}

func TestCreateKnowledgeFromFilePersistsStoredFilePathOnCreate(t *testing.T) {
	t.Parallel()

	repo := &createKnowledgeFileRepoStub{}
	fileSvc := &createKnowledgeFileServiceStub{}
	task := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{
		repo:      repo,
		kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
		task:      task,
	}

	knowledge, err := svc.CreateKnowledgeFromFile(
		newCreateKnowledgeFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, knowledge)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.NotEmpty(t, fileSvc.savedWithKnowledgeID)
	require.Equal(t, fileSvc.savedWithKnowledgeID, knowledge.ID)
	require.Equal(t, 1, repo.createCalls)
	require.NotNil(t, repo.createdKnowledge)
	require.Equal(t, "stored/"+knowledge.ID, repo.createdKnowledge.FilePath)
	require.Equal(t, 1, task.calls)
}

func TestCreateKnowledgeFromImageUsesLocalStorage(t *testing.T) {
	t.Parallel()

	repo := &createKnowledgeFileRepoStub{}
	fileSvc := &createKnowledgeFileServiceStub{}
	task := &createKnowledgeTaskEnqueuerStub{}
	kb := &types.KnowledgeBase{
		ID: "kb-1",
	}
	svc := &knowledgeService{
		repo:      repo,
		kbService: &createKnowledgeFileKBServiceStub{kb: kb},
		fileSvc:   fileSvc,
		task:      task,
	}

	knowledge, err := svc.CreateKnowledgeFromFile(
		newCreateKnowledgeFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "image.png", "image bytes"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.NoError(t, err)
	require.NotNil(t, knowledge)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Equal(t, 1, repo.createCalls)
	require.Equal(t, 1, task.calls)
}

func TestCreateKnowledgeFromFileDeletesStoredFileWhenCreateFails(t *testing.T) {
	t.Parallel()

	repo := &createKnowledgeFileRepoStub{createErr: errors.New("database unavailable")}
	fileSvc := &createKnowledgeFileServiceStub{}
	svc := &knowledgeService{
		repo:      repo,
		kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
	}

	knowledge, err := svc.CreateKnowledgeFromFile(
		newCreateKnowledgeFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		nil,
		nil,
		"",
		"",
		nil,
	)

	require.EqualError(t, err, "database unavailable")
	require.Nil(t, knowledge)
	require.Equal(t, 1, fileSvc.saveCalls)
	require.Equal(t, 1, repo.createCalls)
	require.Equal(t, 1, fileSvc.deleteCalls)
	require.Equal(t, "stored/"+fileSvc.savedWithKnowledgeID, fileSvc.deletedPath)
}

func TestCreateKnowledgeFromFile_PersistsProcessOverrides(t *testing.T) {
	t.Parallel()

	repo := &createKnowledgeFileRepoStub{}
	fileSvc := &createKnowledgeFileServiceStub{}
	task := &createKnowledgeTaskEnqueuerStub{}
	svc := &knowledgeService{
		repo:      repo,
		kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
		fileSvc:   fileSvc,
		task:      task,
	}

	chunkSize := 512
	overrides := &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: chunkSize},
	}

	knowledge, err := svc.CreateKnowledgeFromFile(
		newCreateKnowledgeFileContext(),
		"kb-1",
		newMultipartFileHeader(t, "doc.txt", "hello"),
		map[string]string{"source": "test"},
		nil,
		"",
		"",
		overrides,
	)

	require.NoError(t, err)
	require.NotNil(t, knowledge)
	require.Equal(t, 1, repo.createCalls)
	require.NotNil(t, repo.createdKnowledge)

	parsed, err := repo.createdKnowledge.ProcessOverrides()
	require.NoError(t, err)
	require.NotNil(t, parsed)
	require.NotNil(t, parsed.ChunkingConfig)
	require.Equal(t, chunkSize, parsed.ChunkingConfig.ChunkSize)

	metadataMap, err := repo.createdKnowledge.Metadata.Map()
	require.NoError(t, err)
	require.Equal(t, "test", metadataMap["source"])
}

func newCreateKnowledgeFileContext() context.Context {
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

type createKnowledgeFileKBServiceStub struct {
	interfaces.KnowledgeBaseService

	kb *types.KnowledgeBase
}
