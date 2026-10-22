package service

import (
	"context"

	"io"
	"mime/multipart"

	"strings"
	"testing"

	"reliquary/internal/application/repository"
	"reliquary/internal/config"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"reliquary/internal/models/chat"

	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type retainedIntakeFiles struct{ intakeFileService }

func (s *retainedIntakeFiles) SaveFile(ctx context.Context, header *multipart.FileHeader, space uint64, id string) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, space, id, false)
}

type retainedIntakeModel struct {
	retainedProcessingChat
	prompts      []string
	approveRetry bool
	malformed    bool
}

func (m *retainedIntakeModel) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	stage, _ := types.LLMCallMetadataFromContext(ctx)
	m.stages = append(m.stages, stage)
	if stage != "intake_review" {
		return &types.ChatResponse{Content: validProcessingFixture("event")}, nil
	}
	m.prompts = append(m.prompts, messages[0].Content)
	if m.malformed {
		return &types.ChatResponse{Content: `{"approved":true}`}, nil
	}
	if strings.Contains(messages[1].Content, "discardfixture") ||
		(strings.Contains(messages[1].Content, "rejectfixture") && !m.approveRetry) {
		return &types.ChatResponse{Content: `{"approved":false,"reason":"Unreasoned opinion"}`}, nil
	}
	return &types.ChatResponse{Content: `{"approved":true,"reason":"Complete original publication"}`}, nil
}

type retainedIntakeHarness struct {
	db        *gorm.DB
	ctx       context.Context
	library   *types.Library
	service   *entryService
	queue     *retainedProcessingQueue
	model     *retainedIntakeModel
	processed int
}

func newRetainedIntakeHarness(t *testing.T) *retainedIntakeHarness {
	t.Helper()
	db := pgtest.Open(t)
	space := &types.Space{ID: types.SingleSpaceID, Name: "Personal space"}
	require.NoError(t, db.Create(space).Error)
	library := &types.Library{ID: "library", SpaceID: space.ID, SystemRole: "official", FeedEnabled: true, Template: types.TemplateExternalTimely, Directory: "official", SourceEvidenceRule: "link_required", Name: "Articles", Type: types.LibraryTypeDocument,
		SummaryModelID: "local-fixture", ChunkingConfig: types.ChunkingConfig{ChunkSize: 1024}}
	require.NoError(t, db.Create(library).Error)
	ctx := context.WithValue(context.Background(), types.SpaceIDContextKey, space.ID)
	ctx = context.WithValue(ctx, types.SpaceInfoContextKey, space)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	require.NoError(t, EnsureSystemLibraries(ctx, db))
	repo := repository.NewEntryRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	chunks := &chunkService{chunkRepository: chunkRepo}
	queue := &retainedProcessingQueue{}
	model := &retainedIntakeModel{}
	bases := retainedProcessingLibrary{library: library}
	svc := &entryService{db: db, repo: repo, chunkRepo: chunkRepo, chunkService: chunks,
		libraryService: bases, modelService: &summaryStateModelService{model: model}, task: queue,
		spaceRepo: repository.NewSpaceRepository(db), fileSvc: &retainedIntakeFiles{},
		config: &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{}}}
	return &retainedIntakeHarness{db: db, ctx: ctx, library: library, service: svc, queue: queue, model: model}
}

func (h *retainedIntakeHarness) drain(t *testing.T) {
	t.Helper()
	for h.processed < len(h.queue.tasks) {
		require.Less(t, h.processed, 30, "ingestion must finish with bounded background work")
		task := h.queue.tasks[h.processed]
		h.processed++
		switch task.Type() {
		case types.TypeDocumentProcess:
			require.NoError(t, h.service.ProcessDocument(h.ctx, task))
		case types.TypeManualProcess:
			require.NoError(t, h.service.ProcessManualUpdate(h.ctx, task))
		default:
			t.Fatalf("unexpected ingestion task %q", task.Type())
		}
	}
}

func (h *retainedIntakeHarness) article(t *testing.T, id string) *types.Entry {
	t.Helper()
	article, err := h.service.repo.GetEntryByID(h.ctx, h.library.SpaceID, id)
	require.NoError(t, err)
	return article
}

func TestUploadedMarkdownCompletesWithOneBodyColumnWithoutReview(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	body := "# Uploadfixture\n\nAcme published an original technical announcement with supported deployment capabilities and measured product improvements."
	article, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID, newMultipartFileHeader(t, "announcement.md", body),
		map[string]string{"publisher": "Acme", "original_url": "https://example.com/uploadfixture", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	completed := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, completed.SummaryStatus)
	chunks, err := h.service.chunkRepo.ListChunksByEntryID(h.ctx, h.library.SpaceID, article.ID)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	require.Contains(t, chunks[0].Content, "Uploadfixture")
	require.True(t, h.db.Migrator().HasColumn(&types.Chunk{}, "content"))
	for _, column := range []string{"source_content", "content_revision", "index_status", "last_editor_id"} {
		require.False(t, h.db.Migrator().HasColumn(&types.Chunk{}, column), column)
	}
	require.False(t, h.db.Migrator().HasTable("entry_strategy_versions"))
	require.Empty(t, h.model.prompts)
	require.NotContains(t, string(completed.Metadata), "classification")
}

func TestObsoleteReviewResponseDoesNotGateProcessing(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	h.model.malformed = true
	article, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
		newMultipartFileHeader(t, "draft.md", "Acme published an unfinished product announcement."),
		nil, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	saved := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, saved.SummaryStatus)
	require.Empty(t, h.model.prompts)
	intake, err := saved.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
}
