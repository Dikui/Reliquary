package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"reliquary/internal/config"
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

func TestReviewIntakeCompletenessWarning(t *testing.T) {
	for _, tc := range []struct {
		name, body, response, reason string
		rejected                     bool
	}{
		{
			name: "usable_body", body: "缓存可复用已有上下文，避免重复上传。创建缓存后，后续请求通过缓存 ID 引用。下面是调用代码：",
			response: `{"approved":true,"reason":"缓存机制说明可独立理解","warning":"疑似不完整：提到调用代码，但未提供代码"}`,
			reason:   "疑似不完整：提到调用代码，但未提供代码",
		},
		{
			name: "no_usable_body", body: "# 缓存教程\n\n## 参考链接\n\nhttps://example.org/cache",
			response: `{"approved":false,"auxiliary":true,"reason":"只有标题和资源链接，没有可用实质内容"}`,
			reason:   "缺少符合收录标准的实质正文", rejected: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			library := types.SystemLibraryDefaults()[0]
			library.ModelTiers.Light = "review-model"
			entry := &types.Entry{ID: tc.name, SpaceID: 1, LibraryID: library.ID}
			require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
				Source: "manual", Decision: "pending", SourceInfo: types.SourceInfo{OriginalURL: "https://example.org/cache"},
			}))
			capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: tc.response}}
			svc := &entryService{repo: &intakeRepo{entry: entry}, modelService: &tierTestModels{chat: capture},
				config: &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{MaxBodyChars: 30, SegmentOverlapChars: 1}},
			}
			rejected, err := svc.reviewIntake(context.Background(), library, entry, tc.body)
			require.NoError(t, err)
			require.Equal(t, tc.rejected, rejected)
			intake, err := entry.IntakeMetadata()
			require.NoError(t, err)
			require.Contains(t, intake.Reason, tc.reason)
			require.Greater(t, len(capture.reviews), 1, "warning aggregation also works across segments")
			if !tc.rejected {
				require.Equal(t, "approved", intake.Decision)
				require.NotEqual(t, types.ParseStatusRejected, entry.ParseStatus)
			}
		})
	}
}

func TestReviewIntakeSendsSegmentImagesAndReadStatus(t *testing.T) {
	library := types.SystemLibraryDefaults()[0]
	library.ModelTiers.Light = "review-model"
	entry := &types.Entry{ID: "image-review", SpaceID: 1, LibraryID: library.ID}
	require.NoError(t, entry.SetIntakeMetadata(&types.EntryIntakeMetadata{
		Decision: "pending", SourceInfo: types.SourceInfo{Publisher: "Acme", OriginalURL: "https://example.org/report"},
	}))
	uri := articleImageDataURI(t, 9)
	_, payload, _ := strings.Cut(uri, ",")
	data, err := base64.StdEncoding.DecodeString(payload)
	require.NoError(t, err)
	capture := &mineReviewCapture{retainedProcessingChatInterface: intakeChat{response: `{"approved":true,"reason":"Chart supports the report"}`}}
	svc := &entryService{
		repo: &intakeRepo{entry: entry},
		modelService: &summaryStateModelService{model: capture, metadata: &types.Model{
			ID: "review-model", Type: types.ModelTypeGeneration,
			Parameters: types.ModelParameters{SupportsVision: true, ContextWindow: 32768},
		}},
		fileSvc: &intakeFileService{saved: map[string][]byte{"chart.png": data}},
		config:  &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{MaxImages: 2}},
	}
	body := "# 结果\n\n![缺失](https://example.org/missing.png)\n\n![得分](chart.png)\n\n## 结论\n\n![对比](chart.png)"
	ctx := context.Background()
	require.False(t, svc.singleReviewSegment(ctx, library, entry, body), "image count also requires background segmentation")
	rejected, err := svc.reviewIntake(ctx, library, entry, body)
	require.NoError(t, err)
	require.False(t, rejected)
	require.Len(t, capture.reviews, 2)
	for i, messages := range capture.reviews {
		user := messages[len(messages)-1]
		require.Equal(t, []string{uri}, user.Images, "each segment sends its readable image bytes")
		var input struct {
			Body   string             `json:"body"`
			Images []reviewImageInput `json:"images"`
		}
		require.NoError(t, json.Unmarshal([]byte(user.Content), &input))
		if i == 0 {
			require.Len(t, input.Images, 2)
			require.Equal(t, 1, input.Images[0].Index)
			require.False(t, input.Images[0].Provided)
			require.Equal(t, "未提供图片文件", input.Images[0].SkipReason)
		}
		image := input.Images[len(input.Images)-1]
		require.Equal(t, len(input.Images), image.Index, "unreadable images do not renumber the remaining attachment")
		require.True(t, image.Provided)
		require.Empty(t, image.SkipReason)
		require.Equal(t, len([]rune(input.Body[:strings.Index(input.Body, "chart.png")])), image.Start)
	}
	intake, err := entry.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
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
