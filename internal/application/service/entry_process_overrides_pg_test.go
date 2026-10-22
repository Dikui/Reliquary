package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"

	"testing"

	taskqueue "reliquary/internal/queue"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type processOverridesPGLibrary struct {
	interfaces.LibraryService
	library *types.Library
}

func (s processOverridesPGLibrary) GetLibraryByID(context.Context, string) (*types.Library, error) {
	return s.library, nil
}

type processOverridesPGQueue struct {
	task *taskqueue.Task
	err  error
}

func (q *processOverridesPGQueue) Enqueue(task *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	q.task = task
	return &taskqueue.TaskInfo{}, q.err
}

type processOverridesPGFiles struct {
	interfaces.FileService
	stored  map[string][]byte
	getErr  error
	readErr error
}

func (s *processOverridesPGFiles) SaveFile(ctx context.Context, header *multipart.FileHeader, spaceID uint64, id string) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, spaceID, id, false)
}

func (s *processOverridesPGFiles) SaveBytes(_ context.Context, body []byte, _ uint64, name string, _ bool) (string, error) {
	s.stored[name] = bytes.Clone(body)
	return name, nil
}

func (s *processOverridesPGFiles) GetFile(_ context.Context, path string) (io.ReadCloser, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	if s.readErr != nil {
		return processOverridesPGFailedReader{err: s.readErr}, nil
	}
	return io.NopCloser(bytes.NewReader(s.stored[path])), nil
}

func (s *processOverridesPGFiles) DeleteFile(_ context.Context, path string) error {
	delete(s.stored, path)
	return nil
}

type processOverridesPGFailedReader struct{ err error }

func (r processOverridesPGFailedReader) Read([]byte) (int, error) { return 0, r.err }
func (processOverridesPGFailedReader) Close() error               { return nil }

type processOverridesPGChunks struct {
	interfaces.ChunkService
	repo      interfaces.ChunkRepository
	createErr error
}

func (s processOverridesPGChunks) GetRepository() interfaces.ChunkRepository { return s.repo }
func (s processOverridesPGChunks) CreateChunks(ctx context.Context, chunks []*types.Chunk) error {
	if s.createErr != nil {
		return s.createErr
	}
	return s.repo.CreateChunks(ctx, chunks)
}
func (s processOverridesPGChunks) DeleteChunksByEntryID(ctx context.Context, id string) error {
	return s.repo.DeleteChunksByEntryID(ctx, 1, id)
}

type processOverridesPGHarness struct {
	db      *gorm.DB
	svc     interfaces.EntryService
	repo    interfaces.EntryRepository
	files   *processOverridesPGFiles
	queue   *processOverridesPGQueue
	handler *handler.EntryHandler
	ctx     context.Context
	chunks  *processOverridesPGChunks
}

func newProcessOverridesPGHarness(t *testing.T) *processOverridesPGHarness {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Space{ID: 1, Name: "Process overrides regression"}).Error)
	repo := repository.NewEntryRepository(db)
	spans := repository.NewEntrySpanRepository(db)
	chunks := repository.NewChunkRepository(db)
	libraries := processOverridesPGLibrary{library: &types.Library{ID: "library", SpaceID: 1,
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50}}}
	files := &processOverridesPGFiles{stored: map[string][]byte{}}
	queue := &processOverridesPGQueue{}
	chunkService := &processOverridesPGChunks{repo: chunks}
	svc, err := service.NewEntryService(db, nil, repo, libraries,
		repository.NewSpaceRepository(db), nil, chunkService, chunks,
		files, nil, nil, queue, nil, nil, nil, service.NewSpanTracker(spans, db))
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, &types.Space{ID: 1})
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	return &processOverridesPGHarness{db: db, svc: svc, repo: repo, files: files, queue: queue, chunks: chunkService,
		handler: handler.NewEntryHandler(nil, svc, libraries, queue, spans), ctx: ctx}
}

func processOverridesPGConfig() *types.EntryProcessOverrides {
	return &types.EntryProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 1240, ChunkOverlap: 120,
			EnableParentChild: true, ParentChunkSize: 2480, ChildChunkSize: 400},
	}
}

func processOverridesPGStoredConfig(t *testing.T, k *types.Entry) string {
	t.Helper()
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	require.NotEmpty(t, metadata["process_overrides"], "upload overrides must remain in PostgreSQL metadata")
	return string(metadata["process_overrides"])
}

func (h *processOverridesPGHarness) assertTimelineConfig(t *testing.T, id, want string) *types.Entry {
	t.Helper()
	stored, err := h.repo.GetEntryByID(h.ctx, 1, id)
	require.NoError(t, err)
	require.JSONEq(t, want, processOverridesPGStoredConfig(t, stored))
	// The timeline reads configuration from GET entry, and trace state
	// from GET spans. Exercise both handlers against the persisted row.
	for _, endpoint := range []struct {
		path   string
		handle gin.HandlerFunc
	}{{"/api/v1/entries/:id", h.handler.GetEntry}, {"/api/v1/entries/:id/spans", h.handler.GetEntrySpans}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(types.SpaceIDContextKey.String(), uint64(1))
		c.Request = httptest.NewRequest(http.MethodGet, endpoint.path, nil).WithContext(h.ctx)
		c.Params = gin.Params{{Key: "id", Value: id}}
		endpoint.handle(c)
		require.Empty(t, c.Errors)
		require.Equal(t, http.StatusOK, w.Code)
		if endpoint.path == "/api/v1/entries/:id" {
			var response struct{ Data types.Entry }
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.JSONEq(t, want, processOverridesPGStoredConfig(t, &response.Data))
		} else {
			var response struct {
				Data struct {
					ParseStatus string `json:"parse_status"`
					Trace       *types.SpanTreeNode
				}
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
			require.Equal(t, stored.ParseStatus, response.Data.ParseStatus)
			require.NotNil(t, response.Data.Trace)
			require.Len(t, response.Data.Trace.Children, len(types.AllStages))
		}
	}
	return stored
}

func (h *processOverridesPGHarness) seed(t *testing.T, kind, decision string) *types.Entry {
	t.Helper()
	k := &types.Entry{ID: "message", SpaceID: 1, LibraryID: "library", Type: kind,
		Title: "Processing configuration", Source: "https://example.org/source", ParseStatus: types.ParseStatusPending,
		Metadata: types.JSON(`{"discard_on_cleanup":"old source metadata"}`)}
	require.NoError(t, k.SetProcessOverrides(processOverridesPGConfig()))
	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(k.Metadata, &raw))
	var config map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(raw["process_overrides"], &config))
	config["future_option"] = json.RawMessage(`{"enabled":false,"count":0}`)
	raw["process_overrides"] = json.RawMessage(mustProcessOverridesPGJSON(t, config))
	k.Metadata = types.JSON(mustProcessOverridesPGJSON(t, raw))
	require.NoError(t, k.SetIntakeMetadata(&types.EntryIntakeMetadata{Source: kind, Decision: decision,
		OriginalPath: "original", Images: []string{"image"}, SourceInfo: types.SourceInfo{
			Publisher: "Publisher", OriginalURL: k.Source, PublishedAt: "2026-10-10"}}))
	h.files.stored["original"], h.files.stored["image"] = []byte("original"), []byte("image")
	require.NoError(t, h.repo.CreateEntry(h.ctx, k))
	return k
}

func mustProcessOverridesPGJSON(t *testing.T, v any) string {
	t.Helper()
	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return string(encoded)
}

func TestProcessOverridesPGReparseEnqueueFailure(t *testing.T) {
	for _, kind := range []string{"url", "file", "file_url", types.EntryTypeManual} {
		t.Run(kind, func(t *testing.T) {
			h := newProcessOverridesPGHarness(t)
			k := h.seed(t, kind, "pending")
			if kind == "file" {
				k.FilePath, k.FileName, k.FileType = "upload", "source.txt", "txt"
				h.files.stored["upload"] = []byte("source")
			}
			if kind == types.EntryTypeManual {
				require.NoError(t, k.SetManualMetadata(types.NewManualEntryMetadata("source", types.ManualEntryStatusPublish, 1)))
			}
			require.NoError(t, h.repo.UpdateEntry(h.ctx, k))
			want := processOverridesPGStoredConfig(t, k)
			h.queue.err = errors.New("queue unavailable")
			_, err := h.svc.ReparseEntry(h.ctx, k.ID, nil)
			require.Error(t, err)
			stored := h.assertTimelineConfig(t, k.ID, want)
			require.Equal(t, k.ParseStatus, stored.ParseStatus, "failed enqueue rolls back the new processing state")
			require.JSONEq(t, string(k.Metadata), string(stored.Metadata), "processing overrides and source metadata survive rollback")
			require.Equal(t, k.FilePath, stored.FilePath)
			intake, err := stored.IntakeMetadata()
			require.NoError(t, err)
			require.Contains(t, h.files.stored, intake.OriginalPath, "the original source remains available for a later retry")
			if kind == "file" {
				require.Contains(t, h.files.stored, k.FilePath)
			}
		})
	}
}

func TestProcessOverridesPGIntakeRetryFailure(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, "file", "rejected")
	want := processOverridesPGStoredConfig(t, k)
	h.files.getErr = errors.New("original unavailable")
	_, err := h.svc.ReviewEntryIntake(h.ctx, k.ID, "retry", types.SourceInfo{})
	require.Error(t, err)
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	require.NotContains(t, string(stored.Metadata), "discard_on_cleanup")
	require.Empty(t, h.files.stored)
}

func TestProcessOverridesPGManualProcessingFailure(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, types.EntryTypeManual, "approved")
	want := processOverridesPGStoredConfig(t, k)
	h.chunks.createErr = errors.New("chunk persistence unavailable")
	payload := types.ManualProcessPayload{SpaceID: 1, EntryID: k.ID, LibraryID: "library",
		Content: "![unresolved image](https://example.org/image.png)"}
	require.Error(t, h.svc.ProcessManualUpdate(h.ctx, taskqueue.NewTask(types.TypeManualProcess,
		[]byte(mustProcessOverridesPGJSON(t, payload)))))
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata), "background index failure preserves source metadata for retry")
	require.Contains(t, h.files.stored, "original")
	require.Contains(t, h.files.stored, "image")
}

func TestProcessOverridesPGCleanupWithoutOverrides(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, "file", "pending")
	require.NoError(t, k.SetProcessOverrides(nil))
	k.FilePath, k.FileType, k.FileName = "upload", "json", "source.json"
	h.files.stored["upload"] = []byte("{invalid-json")
	require.NoError(t, h.repo.UpdateEntry(h.ctx, k))
	payload := types.DocumentProcessPayload{SpaceID: 1, EntryID: k.ID, LibraryID: "library",
		FilePath: k.FilePath, FileType: k.FileType, FileName: k.FileName}
	require.Error(t, h.svc.ProcessDocument(h.ctx, taskqueue.NewTask(types.TypeDocumentProcess,
		[]byte(mustProcessOverridesPGJSON(t, payload)))))
	stored, err := h.repo.GetEntryByID(h.ctx, 1, k.ID)
	require.NoError(t, err)
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(stored.Metadata, &metadata))
	require.Len(t, metadata, 1)
	require.Contains(t, metadata, "intake")
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	require.Empty(t, h.files.stored)
}

func TestProcessOverridesPGCancelAndReparse(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, "url", "approved")
	want := processOverridesPGStoredConfig(t, k)
	_, err := h.svc.CancelEntryParse(h.ctx, k.ID)
	require.NoError(t, err)
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata), "cancellation keeps existing source metadata")
	require.Equal(t, types.ParseStatusCancelled, stored.ParseStatus)
	newOverrides := processOverridesPGConfig()
	newOverrides.ChunkingConfig.ChunkSize = 1800
	_, err = h.svc.ReparseEntry(h.ctx, k.ID, newOverrides)
	require.NoError(t, err)
	want = mustProcessOverridesPGJSON(t, newOverrides)
	stored = h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.Contains(t, string(stored.Metadata), "discard_on_cleanup")
	_, err = h.svc.ReparseEntry(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.assertTimelineConfig(t, k.ID, want)
}

func (q *processOverridesPGQueue) EnqueueTx(_ context.Context, _ *gorm.DB, task *taskqueue.Task, opts ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	return q.Enqueue(task, opts...)
}
