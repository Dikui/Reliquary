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
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/application/repository"
	"reliquary/internal/application/service"
	"reliquary/internal/handler"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
	"reliquary/internal/utils"
)

type processOverridesPGKB struct {
	interfaces.KnowledgeBaseService
	kb *types.KnowledgeBase
}

func (s processOverridesPGKB) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type processOverridesPGQueue struct {
	task *asynq.Task
	err  error
}

func (q *processOverridesPGQueue) Enqueue(task *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	q.task = task
	return &asynq.TaskInfo{}, q.err
}

type processOverridesPGFiles struct {
	interfaces.FileService
	stored  map[string][]byte
	getErr  error
	readErr error
}

func (s *processOverridesPGFiles) SaveFile(ctx context.Context, header *multipart.FileHeader, tenantID uint64, id string) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, tenantID, id, false)
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
	repo interfaces.ChunkRepository
}

func (s processOverridesPGChunks) GetRepository() interfaces.ChunkRepository { return s.repo }
func (s processOverridesPGChunks) DeleteChunksByKnowledgeID(ctx context.Context, id string) error {
	return s.repo.DeleteChunksByKnowledgeID(ctx, 1, id)
}

type processOverridesPGHarness struct {
	db      *gorm.DB
	svc     interfaces.KnowledgeService
	repo    interfaces.KnowledgeRepository
	files   *processOverridesPGFiles
	queue   *processOverridesPGQueue
	handler *handler.KnowledgeHandler
	ctx     context.Context
}

func newProcessOverridesPGHarness(t *testing.T) *processOverridesPGHarness {
	t.Helper()
	db := pgtest.Open(t)
	require.NoError(t, db.Create(&types.Tenant{ID: 1, Name: "Process overrides regression"}).Error)
	repo := repository.NewKnowledgeRepository(db)
	spans := repository.NewKnowledgeSpanRepository(db)
	chunks := repository.NewChunkRepository(db)
	kbs := processOverridesPGKB{kb: &types.KnowledgeBase{ID: "kb", TenantID: 1,
		ChunkingConfig: types.ChunkingConfig{ChunkSize: 512, ChunkOverlap: 50}}}
	files := &processOverridesPGFiles{stored: map[string][]byte{}}
	queue := &processOverridesPGQueue{}
	svc, err := service.NewKnowledgeService(db, nil, repo, kbs,
		repository.NewTenantRepository(db), nil, processOverridesPGChunks{repo: chunks}, chunks,
		files, nil, nil, queue, nil, nil, nil, nil, service.NewSpanTracker(spans, db))
	require.NoError(t, err)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1))
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, &types.Tenant{ID: 1})
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	return &processOverridesPGHarness{db: db, svc: svc, repo: repo, files: files, queue: queue,
		handler: handler.NewKnowledgeHandler(nil, svc, kbs, queue, spans), ctx: ctx}
}

func processOverridesPGConfig() *types.KnowledgeProcessOverrides {
	disabled := false
	return &types.KnowledgeProcessOverrides{
		ChunkingConfig: &types.ChunkingConfig{ChunkSize: 1240, ChunkOverlap: 120,
			EnableParentChild: true, ParentChunkSize: 2480, ChildChunkSize: 400},
		EnableMultimodel:         &disabled,
		VLMConfig:                &types.VLMConfig{Enabled: false, DescriptionLanguage: "English", CustomInstructions: "Read image labels"},
		QuestionGenerationConfig: &types.QuestionGenerationConfig{Enabled: true, QuestionCount: 7},
		ParserEngineOverrides:    map[string]string{"mineru_parse_method": "ocr"},
	}
}

func processOverridesPGStoredConfig(t *testing.T, k *types.Knowledge) string {
	t.Helper()
	var metadata map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(k.Metadata, &metadata))
	require.NotEmpty(t, metadata["process_overrides"], "upload overrides must remain in PostgreSQL metadata")
	return string(metadata["process_overrides"])
}

func (h *processOverridesPGHarness) assertTimelineConfig(t *testing.T, id, want string) *types.Knowledge {
	t.Helper()
	stored, err := h.repo.GetKnowledgeByID(h.ctx, 1, id)
	require.NoError(t, err)
	require.JSONEq(t, want, processOverridesPGStoredConfig(t, stored))
	// The timeline reads configuration from GET knowledge, and trace state
	// from GET spans. Exercise both handlers against the persisted row.
	for _, endpoint := range []struct {
		path   string
		handle gin.HandlerFunc
	}{{"/api/v1/knowledge/:id", h.handler.GetKnowledge}, {"/api/v1/knowledge/:id/spans", h.handler.GetKnowledgeSpans}} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Set(types.TenantIDContextKey.String(), uint64(1))
		c.Request = httptest.NewRequest(http.MethodGet, endpoint.path, nil).WithContext(h.ctx)
		c.Params = gin.Params{{Key: "id", Value: id}}
		endpoint.handle(c)
		require.Empty(t, c.Errors)
		require.Equal(t, http.StatusOK, w.Code)
		if endpoint.path == "/api/v1/knowledge/:id" {
			var response struct{ Data types.Knowledge }
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

func (h *processOverridesPGHarness) seed(t *testing.T, kind, decision string) *types.Knowledge {
	t.Helper()
	k := &types.Knowledge{ID: "message", TenantID: 1, KnowledgeBaseID: "kb", Type: kind,
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
	require.NoError(t, k.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: kind, Decision: decision,
		OriginalPath: "original", Images: []string{"image"}, SourceInfo: types.SourceInfo{
			Publisher: "Publisher", OriginalURL: k.Source, PublishedAt: "2026-10-10"}}))
	h.files.stored["original"], h.files.stored["image"] = []byte("original"), []byte("image")
	require.NoError(t, h.repo.CreateKnowledge(h.ctx, k))
	return k
}

func TestProcessOverridesPGURLFetchFailure(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "fetch failed", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	h := newProcessOverridesPGHarness(t)
	overrides := processOverridesPGConfig()
	k, err := h.svc.CreateKnowledgeFromURL(h.ctx, "kb", server.URL+"/article", "", "", nil,
		"Source article", types.ChannelWeb, overrides)
	require.NoError(t, err)
	want := processOverridesPGStoredConfig(t, k)
	require.JSONEq(t, mustProcessOverridesPGJSON(t, overrides), want)
	require.NotNil(t, h.queue.task)
	require.Error(t, h.svc.ProcessDocument(h.ctx, h.queue.task))
	require.Positive(t, requests.Load(), "the real URL parser must fetch the failing HTTP server")
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	intake, err := stored.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "error", intake.Decision)
}

func mustProcessOverridesPGJSON(t *testing.T, v any) string {
	t.Helper()
	encoded, err := json.Marshal(v)
	require.NoError(t, err)
	return string(encoded)
}

func TestProcessOverridesPGFileFailures(t *testing.T) {
	for _, failure := range []string{"storage read", "body read", "parser error", "upload enqueue", "approved parser error"} {
		t.Run(failure, func(t *testing.T) {
			h := newProcessOverridesPGHarness(t)
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "source.json")
			require.NoError(t, err)
			_, err = part.Write([]byte("{invalid-json"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			form, err := multipart.NewReader(&body, writer.Boundary()).ReadForm(1024)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, form.RemoveAll()) })
			if failure == "upload enqueue" {
				h.queue.err = errors.New("queue unavailable")
			}
			k, err := h.svc.CreateKnowledgeFromFile(h.ctx, "kb", form.File["file"][0],
				map[string]string{"discard_on_cleanup": "source metadata"}, nil, "", types.ChannelWeb, processOverridesPGConfig())
			require.NoError(t, err)
			if failure == "approved parser error" {
				intake, err := k.IntakeMetadata()
				require.NoError(t, err)
				intake.Decision = "approved"
				require.NoError(t, k.SetIntakeMetadata(intake))
				require.NoError(t, h.repo.UpdateKnowledge(h.ctx, k))
			}
			want := mustProcessOverridesPGJSON(t, processOverridesPGConfig())
			switch failure {
			case "storage read":
				h.files.getErr = errors.New("storage unavailable")
			case "body read":
				h.files.readErr = errors.New("read interrupted")
			}
			if failure != "upload enqueue" {
				require.Error(t, h.svc.ProcessDocument(h.ctx, h.queue.task))
			}
			stored := h.assertTimelineConfig(t, k.ID, want)
			require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
			if failure == "approved parser error" {
				require.JSONEq(t, string(k.Metadata), string(stored.Metadata))
				require.Equal(t, k.FilePath, stored.FilePath)
				require.NotEmpty(t, h.files.stored)
				return
			}
			require.NotContains(t, string(stored.Metadata), "discard_on_cleanup")
			require.Empty(t, stored.FilePath)
			require.Empty(t, h.files.stored)
		})
	}
}

func TestProcessOverridesPGReparseEnqueueFailure(t *testing.T) {
	for _, kind := range []string{"url", "file", "file_url", types.KnowledgeTypeManual} {
		t.Run(kind, func(t *testing.T) {
			h := newProcessOverridesPGHarness(t)
			k := h.seed(t, kind, "pending")
			if kind == "file" {
				k.FilePath, k.FileName, k.FileType = "upload", "source.txt", "txt"
				h.files.stored["upload"] = []byte("source")
			}
			if kind == types.KnowledgeTypeManual {
				require.NoError(t, k.SetManualMetadata(types.NewManualKnowledgeMetadata("source", types.ManualKnowledgeStatusPublish, 1)))
			}
			require.NoError(t, h.repo.UpdateKnowledge(h.ctx, k))
			want := processOverridesPGStoredConfig(t, k)
			h.queue.err = errors.New("queue unavailable")
			_, err := h.svc.ReparseKnowledge(h.ctx, k.ID, nil)
			require.Error(t, err)
			stored := h.assertTimelineConfig(t, k.ID, want)
			require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
			require.NotContains(t, string(stored.Metadata), "discard_on_cleanup")
			require.Empty(t, h.files.stored)
		})
	}
}

func TestProcessOverridesPGIntakeRetryFailure(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, "file", "rejected")
	want := processOverridesPGStoredConfig(t, k)
	h.files.getErr = errors.New("original unavailable")
	_, err := h.svc.ReviewKnowledgeIntake(h.ctx, k.ID, "retry", types.SourceInfo{})
	require.Error(t, err)
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	require.NotContains(t, string(stored.Metadata), "discard_on_cleanup")
	require.Empty(t, h.files.stored)
}

func TestProcessOverridesPGFileURLDownloadFailure(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		http.Error(w, "file download failed", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)
	h := newProcessOverridesPGHarness(t)
	k, err := h.svc.CreateKnowledgeFromURL(h.ctx, "kb", server.URL+"/source.json", "", "", nil,
		"Source file", types.ChannelWeb, processOverridesPGConfig())
	require.NoError(t, err)
	require.Equal(t, "file_url", k.Type)
	want := processOverridesPGStoredConfig(t, k)
	require.Error(t, h.svc.ProcessDocument(h.ctx, h.queue.task))
	require.Positive(t, requests.Load())
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
}

func TestProcessOverridesPGManualProcessingFailure(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, types.KnowledgeTypeManual, "pending")
	want := processOverridesPGStoredConfig(t, k)
	payload := types.ManualProcessPayload{TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: "kb",
		Content: "![unresolved image](https://example.org/image.png)"}
	require.NoError(t, h.svc.ProcessManualUpdate(h.ctx, asynq.NewTask(types.TypeManualProcess,
		[]byte(mustProcessOverridesPGJSON(t, payload)))))
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusFailed, stored.ParseStatus)
	require.NotContains(t, string(stored.Metadata), "discard_on_cleanup")
	require.Empty(t, h.files.stored)
}

func TestProcessOverridesPGCleanupWithoutOverrides(t *testing.T) {
	h := newProcessOverridesPGHarness(t)
	k := h.seed(t, "file", "pending")
	require.NoError(t, k.SetProcessOverrides(nil))
	k.FilePath, k.FileType, k.FileName = "upload", "json", "source.json"
	h.files.stored["upload"] = []byte("{invalid-json")
	require.NoError(t, h.repo.UpdateKnowledge(h.ctx, k))
	payload := types.DocumentProcessPayload{TenantID: 1, KnowledgeID: k.ID, KnowledgeBaseID: "kb",
		FilePath: k.FilePath, FileType: k.FileType, FileName: k.FileName}
	require.Error(t, h.svc.ProcessDocument(h.ctx, asynq.NewTask(types.TypeDocumentProcess,
		[]byte(mustProcessOverridesPGJSON(t, payload)))))
	stored, err := h.repo.GetKnowledgeByID(h.ctx, 1, k.ID)
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
	_, err := h.svc.CancelKnowledgeParse(h.ctx, k.ID)
	require.NoError(t, err)
	stored := h.assertTimelineConfig(t, k.ID, want)
	require.JSONEq(t, string(k.Metadata), string(stored.Metadata), "cancellation keeps existing source metadata")
	require.Equal(t, types.ParseStatusCancelled, stored.ParseStatus)
	newOverrides := processOverridesPGConfig()
	newOverrides.ChunkingConfig.ChunkSize = 1800
	_, err = h.svc.ReparseKnowledge(h.ctx, k.ID, newOverrides)
	require.NoError(t, err)
	want = mustProcessOverridesPGJSON(t, newOverrides)
	stored = h.assertTimelineConfig(t, k.ID, want)
	require.Equal(t, types.ParseStatusPending, stored.ParseStatus)
	require.Contains(t, string(stored.Metadata), "discard_on_cleanup")
	_, err = h.svc.ReparseKnowledge(h.ctx, k.ID, nil)
	require.NoError(t, err)
	h.assertTimelineConfig(t, k.ID, want)
}
