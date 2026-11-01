package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	taskqueue "reliquary/internal/queue"

	"github.com/stretchr/testify/require"

	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

func TestReviewIntakeAcceptsAllTemplatesWithoutModelReview(t *testing.T) {
	for _, library := range types.SystemLibraryDefaults() {
		t.Run(library.Template+"/"+library.Directory, func(t *testing.T) {
			entry := &types.Entry{ID: "note", SpaceID: 1, LibraryID: library.ID}
			require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "pending"}))
			capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: `{"approved":false,"reason":"Old model decision"}`}}
			svc := &entryService{repo: &intakeRepo{entry: entry}, modelService: &tierTestModels{chat: capture}}
			err := svc.approveRetainedIntake(context.Background(), entry)
			require.NoError(t, err)
			require.Empty(t, capture.reviews)
			intake, err := entry.IntakeMetadata()
			require.NoError(t, err)
			require.Equal(t, "approved", intake.Decision)
			require.Empty(t, intake.Reason)
		})
	}
}

type intakeRepo struct {
	interfaces.EntryRepository
	entry *types.Entry
}

func (r *intakeRepo) GetEntryByID(context.Context, uint64, string) (*types.Entry, error) {
	return r.entry, nil
}

func (r *intakeRepo) UpdateEntry(_ context.Context, entry *types.Entry) error {
	copy := *entry
	r.entry = &copy
	return nil
}

type intakeLibraryService struct {
	interfaces.LibraryService
	creatorID string
}

func (s intakeLibraryService) GetLibraryByID(context.Context, string) (*types.Library, error) {
	return &types.Library{
		ID: "library", SpaceID: 1, CreatorID: s.creatorID, SummaryModelID: "summary", EmbeddingModelID: "embedding",
		IndexingStrategy: types.IndexingStrategy{VectorEnabled: true},
	}, nil
}

type intakeSpaceRepo struct{ interfaces.SpaceRepository }

func (intakeSpaceRepo) GetSpaceByID(context.Context, uint64) (*types.Space, error) {
	return &types.Space{ID: 1}, nil
}

func (intakeSpaceRepo) AdjustStorageUsed(context.Context, uint64, int64) error { return nil }

type intakeSpaceService struct{ interfaces.SpaceService }

func (intakeChat) GetModelName() string { return "intake" }

func (intakeModelService) GetEmbeddingModel(context.Context, string) (embedding.Embedder, error) {
	return parentChildEmbedder{}, nil
}

func TestReviewIntakeIgnoresObsoleteModelRejection(t *testing.T) {
	entry := &types.Entry{ID: "qwen-article", SpaceID: 1, LibraryID: "library",
		Metadata: types.JSON(`{"intake_reject_reason":"Qwen article missing publication date"}`)}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
		Source: "manual", Decision: "pending", OriginalPath: "stored-original",
		SourceInfo: types.SourceInfo{Publisher: "Qwen", OriginalURL: "https://qwen.ai/blog?id=article", PublishedAt: "2026-09-20"},
	}))
	repo := &intakeRepo{entry: entry}
	svc := &entryService{repo: repo}
	err := svc.approveRetainedIntake(context.Background(), entry)
	require.NoError(t, err)
	intake, err := repo.entry.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Empty(t, intake.Reason)
	require.Equal(t, "stored-original", intake.OriginalPath)
}

func (s intakeModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.chat, nil
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

func TestReviewEntryIntakeCorrectionClearsQwenRejection(t *testing.T) {
	entry := &types.Entry{ID: "qwen-article", SpaceID: 1, LibraryID: "library", Type: "manual",
		ParseStatus: types.ParseStatusRejected,
		Metadata:    types.JSON(`{"intake_reject_reason":"Qwen article missing publication date"}`)}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
		Source: "manual", Decision: "rejected", Reason: "Qwen article missing publication date", OriginalPath: "original",
		SourceInfo: types.SourceInfo{Publisher: "Qwen", OriginalURL: "https://qwen.ai/blog?id=article"},
	}))
	repo := &intakeRepo{entry: entry}
	task := &intakeTask{}
	svc := &entryService{repo: repo, libraryService: intakeLibraryService{creatorID: "owner"},
		fileSvc:      &intakeFileService{saved: map[string][]byte{"original": []byte("Complete original body")}},
		modelService: intakeModelService{chat: intakeChat{response: `{"approved":true,"reason":"sourced article"}`}}, task: task}
	ctx := context.WithValue(context.WithValue(context.Background(), types.SpaceIDContextKey, uint64(1)), types.UserIDContextKey, "other-editor")
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	updated, err := svc.ReviewEntryIntake(ctx, entry.ID, "retry", types.SourceInfo{PublishedAt: "2026-09-20"})
	require.NoError(t, err)
	require.Equal(t, 1, task.calls)
	require.NotContains(t, string(updated.Metadata), "intake_reject_reason")
	updated.ParseStatus = types.ParseStatusFailed
	repo.entry = updated
	updated, err = svc.ReviewEntryIntake(ctx, entry.ID, "retry", types.SourceInfo{})
	require.NoError(t, err)
	require.Equal(t, 2, task.calls)
	var review types.ManualProcessPayload
	require.NoError(t, json.Unmarshal(task.last.Payload(), &review))
	require.NotEmpty(t, review.ReviewID, "retry reviews the stored source before image processing")
	require.False(t, review.ReviewApproved)
	err = svc.approveRetainedIntake(ctx, updated)
	require.NoError(t, err)
	intake, err := repo.entry.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, "2026-09-20", intake.SourceInfo.PublishedAt)
}

func (s *intakeFileService) DeleteFile(_ context.Context, path string) error {
	s.deleted = append(s.deleted, path)
	delete(s.saved, path)
	delete(s.written, path)
	return nil
}

type intakeModelService struct {
	interfaces.ModelService
	chat chat.Chat
}

type intakeFileService struct {
	interfaces.FileService
	saved   map[string][]byte
	written map[string][]byte
	deleted []string
	getErr  error
}

type intakeTask struct {
	calls int
	last  *taskqueue.Task
}

func (t *intakeTask) Enqueue(task *taskqueue.Task, _ ...taskqueue.Option) (*taskqueue.TaskInfo, error) {
	t.calls++
	t.last = task
	return &taskqueue.TaskInfo{}, nil
}

func (s *intakeFileService) GetFile(_ context.Context, path string) (io.ReadCloser, error) {
	if s.getErr != nil {
		return nil, s.getErr
	}
	return io.NopCloser(bytes.NewReader(s.saved[path])), nil
}

func (intakeChat) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("not used")
}

func (intakeChat) GetModelID() string { return "intake" }

type intakeChat struct {
	response string
	err      error
}

func (m intakeChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_processing" {
		return &types.ChatResponse{Content: validProcessingFixture("event")}, nil
	}
	return &types.ChatResponse{Content: reviewFixtureContent(m.response, messages)}, nil
}

func reviewFixtureContent(raw string, messages []chat.Message) string {
	var decision struct {
		Approved  *bool  `json:"approved"`
		Auxiliary bool   `json:"auxiliary,omitempty"`
		Reason    string `json:"reason"`
		Warning   string `json:"warning,omitempty"`
		Summary   string `json:"summary,omitempty"`
	}
	if json.Unmarshal([]byte(raw), &decision) != nil {
		return raw
	}
	var input struct {
		Scope   string `json:"scope"`
		Body    string `json:"body"`
		Segment struct {
			Count int `json:"count"`
		} `json:"segment"`
	}
	if json.Unmarshal([]byte(messages[len(messages)-1].Content), &input) != nil {
		return raw
	}
	if input.Scope == "document" && decision.Auxiliary {
		decision.Auxiliary = false
		decision.Reason = "全文缺少符合收录标准的实质正文：" + decision.Reason
	}
	if input.Segment.Count > 1 {
		body := []rune(input.Body)
		decision.Summary = "Fixture content: " + string(body[:min(300, len(body))])
	}
	encoded, _ := json.Marshal(decision)
	return string(encoded)
}

func (intakeModelService) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	return &types.Model{ID: id, Type: types.ModelTypeGeneration}, nil
}
