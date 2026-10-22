package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
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

func TestReviewIntakeAcceptsAllTemplatesWithoutModelReview(t *testing.T) {
	for _, library := range types.SystemLibraryDefaults() {
		t.Run(library.Template+"/"+library.Directory, func(t *testing.T) {
			entry := &types.Entry{ID: "note", SpaceID: 1, LibraryID: library.ID}
			require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{Decision: "pending"}))
			capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: `{"approved":false,"reason":"Old model decision"}`}}
			svc := &entryService{repo: &intakeRepo{entry: entry}, modelService: &tierTestModels{chat: capture}}
			rejected, err := svc.reviewIntake(context.Background(), library, entry, "未完成的草稿。")
			require.NoError(t, err)
			require.False(t, rejected)
			require.Empty(t, capture.reviews)
			intake, err := entry.IntakeMetadata()
			require.NoError(t, err)
			require.Equal(t, "approved", intake.Decision)
			require.Empty(t, intake.Reason)
		})
	}
}

func TestReviewMissingImagesDoNotReserveVisualTokens(t *testing.T) {
	entry := &types.Entry{ID: "missing-images", SpaceID: 1}
	library := &types.Library{ModelTiers: types.ModelTierConfig{Light: "review-model"}}
	capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: `{"approved":false,"reason":"Fixture review decision"}`}}
	svc := &entryService{repo: &intakeRepo{entry: entry}, modelService: &summaryStateModelService{
		model: capture, metadata: &types.Model{ID: "review-model", Type: types.ModelTypeGeneration,
			Parameters: types.ModelParameters{SupportsVision: true, ContextWindow: 16384}},
	}}
	approved, _, err := svc.reviewSegments(context.Background(), library, entry, strings.Repeat("![图表](https://example.org/missing.png)\n\n", 8), types.SourceInfo{})
	require.NoError(t, err)
	require.False(t, approved, "the model still decides whether the supplied material meets the criteria")
	require.Len(t, capture.reviews, 1)
	user := capture.reviews[0][1]
	require.Empty(t, user.Images)
	var input struct {
		Images []reviewImageInput `json:"images"`
	}
	require.NoError(t, json.Unmarshal([]byte(user.Content), &input))
	require.Len(t, input.Images, 8)
	for _, image := range input.Images {
		require.False(t, image.Provided)
		require.Equal(t, "未提供图片文件", image.SkipReason)
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
	rejected, err := svc.reviewIntake(context.Background(), &types.Library{ID: "library", SpaceID: 1}, entry, "Complete original body")
	require.NoError(t, err)
	require.False(t, rejected)
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
	var decision segmentReviewDecision
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
