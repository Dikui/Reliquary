package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type intakeRepo struct {
	interfaces.KnowledgeRepository
	knowledge *types.Knowledge
}

func (r *intakeRepo) GetKnowledgeByID(context.Context, uint64, string) (*types.Knowledge, error) {
	return r.knowledge, nil
}

func (r *intakeRepo) UpdateKnowledge(_ context.Context, knowledge *types.Knowledge) error {
	copy := *knowledge
	r.knowledge = &copy
	return nil
}

type intakeKBService struct {
	interfaces.KnowledgeBaseService
	creatorID string
}

func (s intakeKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{
		ID: "kb", TenantID: 1, CreatorID: s.creatorID, SummaryModelID: "summary", EmbeddingModelID: "embedding",
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
		ChunkingConfig: types.ChunkingConfig{ParserEngineRules: []types.ParserEngineRule{{
			FileTypes: []string{"txt"}, Engine: "builtin",
		}}},
	}, nil
}

type intakeTenantRepo struct{ interfaces.TenantRepository }

func (intakeTenantRepo) GetTenantByID(context.Context, uint64) (*types.Tenant, error) {
	return &types.Tenant{ID: 1, RetrieverEngines: types.RetrieverEngines{Engines: []types.RetrieverEngineParams{{
		RetrieverType: types.VectorRetrieverType, RetrieverEngineType: types.PostgresRetrieverEngineType,
	}}}}, nil
}

func (intakeTenantRepo) AdjustStorageUsed(context.Context, uint64, int64) error { return nil }

type intakeTenantService struct{ interfaces.TenantService }

func (intakeTenantService) GetWeKnoraCloudCredentials(context.Context) *types.WeKnoraCloudCredentials {
	return nil
}

type intakeReader struct {
	interfaces.DocumentReader
	result *types.ReadResult
}

func (r intakeReader) Read(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
	return r.result, nil
}
func (intakeReader) IsConnected() bool { return true }

type intakeFileService struct {
	interfaces.FileService
	saved   map[string][]byte
	written map[string][]byte
	deleted []string
	getErr  error
}

func (s *intakeFileService) GetFile(_ context.Context, path string) (io.ReadCloser, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return io.NopCloser(bytes.NewReader(s.saved[path])), nil
}

func (s *intakeFileService) SaveBytes(_ context.Context, body []byte, _ uint64, name string, _ bool) (string, error) {
	path := "stored/" + name
	if s.saved == nil {
		s.saved = map[string][]byte{}
	}
	if s.written == nil {
		s.written = map[string][]byte{}
	}
	s.saved[path] = append([]byte(nil), body...)
	s.written[path] = append([]byte(nil), body...)
	return path, nil
}

func (s *intakeFileService) DeleteFile(_ context.Context, path string) error {
	s.deleted = append(s.deleted, path)
	delete(s.saved, path)
	delete(s.written, path)
	return nil
}

type intakeChat struct {
	response string
	err      error
}

func (m intakeChat) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &types.ChatResponse{Content: m.response}, nil
}
func (intakeChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}
func (intakeChat) GetModelName() string { return "intake" }
func (intakeChat) GetModelID() string   { return "intake" }

type intakeModelService struct {
	interfaces.ModelService
	chat chat.Chat
}

func (s intakeModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.chat, nil
}
func (intakeModelService) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return parentChildEmbedder{}, nil
}

type intakeTask struct{ calls int }

func (t *intakeTask) Enqueue(*asynq.Task, ...asynq.Option) (*asynq.TaskInfo, error) {
	t.calls++
	return &asynq.TaskInfo{}, nil
}

func TestProcessDocumentIntakeQualityGate(t *testing.T) {
	tests := []struct {
		name       string
		chat       intakeChat
		wantStatus string
		wantBody   bool
		wantIndex  bool
		wantErr    bool
		source     types.SourceInfo
	}{
		{"approved", intakeChat{response: `{"approved":true,"reason":"analysis"}`}, "", true, true, false, types.SourceInfo{}},
		{"publisher without personal author", intakeChat{response: `{"approved":true,"reason":"reported facts"}`}, "", true, true, false, types.SourceInfo{Publisher: "Google DeepMind", OriginalURL: "https://example.com/post", PublishedAt: "2026-01-01"}},
		{"rejected", intakeChat{response: `{"approved":false,"reason":"opinion only"}`}, types.ParseStatusRejected, true, false, false, types.SourceInfo{}},
		{"model error", intakeChat{err: errors.New("model unavailable")}, types.ParseStatusFailed, false, false, true, types.SourceInfo{}},
		{"unknown source evidence", intakeChat{}, types.ParseStatusRejected, true, false, false, types.SourceInfo{Author: "未知", OriginalURL: "https://example.com/post", PublishedAt: "未知"}},
		{"placeholder author with publisher", intakeChat{}, types.ParseStatusRejected, true, false, false, types.SourceInfo{Author: "未知", Publisher: "Google DeepMind", OriginalURL: "https://example.com/post", PublishedAt: "2026-01-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			knowledge := &types.Knowledge{ID: "knowledge", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", FileName: "message", FilePath: "input", ParseStatus: types.ParseStatusPending}
			source := types.SourceInfo{
				Author: "Ada", OriginalURL: "https://example.com/post", PublishedAt: "2026-01-01",
			}
			if tt.source.Author != "" || tt.source.Publisher != "" {
				source = tt.source
			}
			require.NoError(t, knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{Source: "manual", Decision: "pending", SourceInfo: source}))
			repo := &intakeRepo{knowledge: knowledge}
			files := &intakeFileService{}
			chunks := &parentChildChunkService{}
			engine := &parentChildRetrieveEngine{}
			svc := &knowledgeService{
				repo: repo, kbService: intakeKBService{}, tenantRepo: intakeTenantRepo{}, tenantService: intakeTenantService{}, fileSvc: files,
				documentReader: intakeReader{result: &types.ReadResult{MarkdownContent: "Full source body with analysis."}},
				modelService:   intakeModelService{chat: tt.chat}, chunkService: chunks,
				retrieveEngine: parentChildRetrieveRegistry{engine: engine}, graphEngine: parentChildGraphRepo{}, task: parentChildTaskEnqueuer{},
			}
			files.saved = map[string][]byte{"input": []byte("source")}
			payload, err := json.Marshal(types.DocumentProcessPayload{TenantID: 1, KnowledgeID: knowledge.ID, KnowledgeBaseID: "kb", FilePath: "input", FileName: "message.txt", FileType: "txt"})
			require.NoError(t, err)
			err = svc.ProcessDocument(context.Background(), asynq.NewTask(types.TypeDocumentProcess, payload, asynq.MaxRetry(0)))
			if tt.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tt.wantStatus != "" {
				require.Equalf(t, tt.wantStatus, repo.knowledge.ParseStatus, "error: %s", repo.knowledge.ErrorMessage)
			}
			intake, err := repo.knowledge.IntakeMetadata()
			require.NoError(t, err)
			if tt.wantBody {
				require.NotEmpty(t, intake.OriginalPath)
				require.NotEmpty(t, files.written)
				require.Equal(t, []byte("Full source body with analysis."), files.saved[intake.OriginalPath])
				body, _, getErr := svc.GetKnowledgeFile(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1)), knowledge.ID)
				require.NoError(t, getErr)
				defer body.Close()
				got, readErr := io.ReadAll(body)
				require.NoError(t, readErr)
				require.Equal(t, "Full source body with analysis.", string(got))
			} else {
				require.Empty(t, intake.OriginalPath)
				require.Empty(t, files.written)
				require.Empty(t, files.saved)
				require.NotEmpty(t, repo.knowledge.ErrorMessage)
			}
			if tt.wantIndex {
				require.NotEmpty(t, chunks.created)
				require.NotEmpty(t, engine.indexed)
				require.NotEqual(t, types.ParseStatusPending, repo.knowledge.ParseStatus)
				require.NotEqual(t, types.ParseStatusRejected, repo.knowledge.ParseStatus)
				require.NotEqual(t, types.ParseStatusFailed, repo.knowledge.ParseStatus)
			} else {
				require.Empty(t, chunks.created)
				require.Empty(t, engine.indexed)
			}
			if tt.wantIndex {
				require.Equal(t, "enabled", repo.knowledge.EnableStatus)
			}
		})
	}
}

func TestReviewKnowledgeIntakeSourceReadFailureCleansPendingMessage(t *testing.T) {
	for _, tt := range []struct {
		name   string
		getErr error
		body   []byte
	}{
		{name: "storage error", getErr: errors.New("storage unavailable")},
		{name: "empty original", body: []byte(" \n")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			knowledge := &types.Knowledge{ID: "knowledge", TenantID: 1, KnowledgeBaseID: "kb", Type: "file", FilePath: "input", ParseStatus: types.ParseStatusRejected}
			require.NoError(t, knowledge.SetIntakeMetadata(&types.KnowledgeIntakeMetadata{
				Source: "manual", Decision: "rejected", OriginalPath: "original", Images: []string{"image"},
				SourceInfo: types.SourceInfo{Author: "Ada", OriginalURL: "https://example.com/post", PublishedAt: "2026-01-01"},
			}))
			repo := &intakeRepo{knowledge: knowledge}
			files := &intakeFileService{getErr: tt.getErr, saved: map[string][]byte{"input": []byte("upload"), "original": tt.body, "image": []byte("image")}}
			task := &intakeTask{}
			svc := &knowledgeService{repo: repo, kbService: intakeKBService{creatorID: "owner"}, fileSvc: files, task: task}
			ctx := context.WithValue(context.WithValue(context.Background(), types.TenantIDContextKey, uint64(1)), types.UserIDContextKey, "owner")

			_, err := svc.ReviewKnowledgeIntake(ctx, knowledge.ID, "retry", types.SourceInfo{})
			require.Error(t, err)
			intake, intakeErr := repo.knowledge.IntakeMetadata()
			require.NoError(t, intakeErr)
			require.Equal(t, "error", intake.Decision)
			require.Empty(t, intake.OriginalPath)
			require.Empty(t, intake.Images)
			require.Equal(t, types.ParseStatusFailed, repo.knowledge.ParseStatus)
			require.Empty(t, files.saved)
			require.Zero(t, task.calls)
		})
	}
}
