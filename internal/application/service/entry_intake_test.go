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

func TestIntakeXSourceEvidence(t *testing.T) {
	for _, raw := range []string{"https://x.com/bcherny/status/123", "https://twitter.com/bcherny/status/123", "https://x.com./post/1", "https://www.x.com./post/1", "https://twitter.com../post/1", "https://example.com/article"} {
		require.False(t, blockedIntakeURL(raw), raw)
	}
	for _, raw := range []string{"not-a-url", "https:///post/1", "ftp://x.com/post/1", "https://user:password@x.com/post/1"} {
		require.True(t, blockedIntakeURL(raw), raw)
	}
}

func TestReviewIntakeXUsesLibraryCriteria(t *testing.T) {
	for _, tc := range []struct {
		name, url, response string
		rejected            bool
	}{
		{"approved", "https://x.com/author/status/123", `{"approved":true,"reason":"Substantive product experience"}`, false},
		{"rejected", "https://twitter.com/author/status/123", `{"approved":false,"reason":"No substantive opinion"}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			library := types.SystemLibraryDefaults()[1]
			library.ModelTiers.Light = "review-model"
			entry := &types.Entry{ID: "post", SpaceID: 1, LibraryID: library.ID}
			require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
				Decision: "pending", SourceInfo: types.SourceInfo{Author: "@author", OriginalURL: tc.url},
			}))
			capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: tc.response}}
			svc := &entryService{repo: &intakeRepo{entry: entry}, modelService: &tierTestModels{chat: capture}}
			rejected, err := svc.reviewIntake(context.Background(), library, entry, "The author's product experience.")
			require.NoError(t, err)
			require.Equal(t, tc.rejected, rejected)
			require.Len(t, capture.reviews, 1)
			require.Contains(t, capture.reviews[0][0].Content, library.ReviewCriteria)
			intake, err := entry.IntakeMetadata()
			require.NoError(t, err)
			require.Equal(t, tc.name, intake.Decision)
			if tc.rejected {
				require.Contains(t, intake.Reason, "No substantive opinion")
			}
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

func TestReviewIntakeKeepsSourcedBodyWithExplicitRejection(t *testing.T) {
	const reason = "Qwen article missing publication date"
	entry := &types.Entry{ID: "qwen-article", SpaceID: 1, LibraryID: "library",
		Metadata: types.JSON(`{"intake_reject_reason":"Qwen article missing publication date"}`)}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
		Source: "manual", Decision: "pending", OriginalPath: "stored-original",
		SourceInfo: types.SourceInfo{Publisher: "Qwen", OriginalURL: "https://qwen.ai/blog?id=article", PublishedAt: "2026-09-20"},
	}))
	repo := &intakeRepo{entry: entry}
	svc := &entryService{repo: repo}
	rejected, err := svc.reviewIntake(context.Background(), &types.Library{ID: "library", SpaceID: 1}, entry, "Complete original body")
	require.NoError(t, err)
	require.True(t, rejected)
	intake, err := repo.entry.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "rejected", intake.Decision)
	require.Equal(t, reason, intake.Reason)
	require.Equal(t, "stored-original", intake.OriginalPath)
	require.Equal(t, types.ParseStatusRejected, repo.entry.ParseStatus)
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
	library, err := svc.libraryService.GetLibraryByID(ctx, "library")
	require.NoError(t, err)
	rejected, err := svc.reviewIntake(ctx, library, updated, "Complete original body")
	require.NoError(t, err)
	require.False(t, rejected)
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

func (m intakeChat) Chat(ctx context.Context, _ []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.err != nil {
		return nil, m.err
	}
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	if stage == "article_processing" {
		return &types.ChatResponse{Content: validProcessingFixture("event")}, nil
	}
	return &types.ChatResponse{Content: m.response}, nil
}

func (intakeModelService) GetModelByID(_ context.Context, id string) (*types.Model, error) {
	return &types.Model{ID: id, Type: types.ModelTypeGeneration}, nil
}
