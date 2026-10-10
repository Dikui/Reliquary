package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/application/repository"
	"reliquary/internal/config"
	"reliquary/internal/datasource"
	"reliquary/internal/datasource/connector/rss"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type retainedIntakeFiles struct{ intakeFileService }

func (s *retainedIntakeFiles) SaveFile(ctx context.Context, header *multipart.FileHeader, tenant uint64, id string) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	body, err := io.ReadAll(file)
	if err != nil {
		return "", err
	}
	return s.SaveBytes(ctx, body, tenant, id, false)
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
		return &types.ChatResponse{Content: "Fixture publisher reports an original product announcement and its deployment capabilities."}, nil
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
	kb        *types.KnowledgeBase
	service   *knowledgeService
	post      *KnowledgePostProcessService
	queue     *retainedProcessingQueue
	model     *retainedIntakeModel
	processed int
}

func newRetainedIntakeHarness(t *testing.T) *retainedIntakeHarness {
	t.Helper()
	db := pgtest.Open(t)
	tenant := &types.Tenant{ID: types.SingleTenantID, Name: "Personal space"}
	require.NoError(t, db.Create(tenant).Error)
	kb := &types.KnowledgeBase{ID: "kb", TenantID: tenant.ID, Name: "Articles", Type: types.KnowledgeBaseTypeDocument,
		SummaryModelID: "local-fixture", ChunkingConfig: types.ChunkingConfig{ChunkSize: 1024,
			ParserEngineRules: []types.ParserEngineRule{{FileTypes: []string{"md", "txt"}, Engine: "simple"}}}}
	require.NoError(t, db.Create(kb).Error)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, tenant.ID)
	ctx = context.WithValue(ctx, types.TenantInfoContextKey, tenant)
	ctx = context.WithValue(ctx, types.UserIDContextKey, "personal-user")
	repo := repository.NewKnowledgeRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	chunks := &chunkService{chunkRepository: chunkRepo}
	queue := &retainedProcessingQueue{}
	model := &retainedIntakeModel{}
	bases := retainedProcessingKB{kb: kb}
	svc := &knowledgeService{db: db, repo: repo, chunkRepo: chunkRepo, chunkService: chunks,
		kbService: bases, modelService: &summaryStateModelService{model: model}, task: queue,
		tenantRepo: repository.NewTenantRepository(db), fileSvc: &retainedIntakeFiles{},
		config: &config.Config{ArticleProcessing: &config.ArticleProcessingConfig{GenerateSummaryPrompt: "Summarize the original document."}}}
	return &retainedIntakeHarness{db: db, ctx: ctx, kb: kb, service: svc, queue: queue, model: model,
		post: &KnowledgePostProcessService{knowledgeRepo: repo, kbService: bases, chunkService: chunks, taskEnqueuer: queue}}
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
		case types.TypeKnowledgePostProcess:
			require.NoError(t, h.post.Handle(h.ctx, task))
		case types.TypeSummaryGeneration:
			require.NoError(t, h.service.ProcessSummaryGeneration(h.ctx, task))
		default:
			t.Fatalf("unexpected ingestion task %q", task.Type())
		}
	}
}

func (h *retainedIntakeHarness) article(t *testing.T, id string) *types.Knowledge {
	t.Helper()
	article, err := h.service.repo.GetKnowledgeByID(h.ctx, h.kb.TenantID, id)
	require.NoError(t, err)
	return article
}

func TestUploadedMarkdownCompletesWithOneBodyColumnAndBuiltinReview(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	body := "# Uploadfixture\n\nAcme published an original technical announcement with supported deployment capabilities and measured product improvements."
	article, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID, newMultipartFileHeader(t, "announcement.md", body),
		map[string]string{"publisher": "Acme", "original_url": "https://example.com/uploadfixture", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	h.drain(t)
	completed := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, completed.SummaryStatus)
	chunks, err := h.service.chunkRepo.ListChunksByKnowledgeID(h.ctx, h.kb.TenantID, article.ID)
	require.NoError(t, err)
	require.NotEmpty(t, chunks)
	require.Contains(t, chunks[0].Content, "Uploadfixture")
	require.True(t, h.db.Migrator().HasColumn(&types.Chunk{}, "content"))
	for _, column := range []string{"source_content", "content_revision", "index_status", "last_editor_id"} {
		require.False(t, h.db.Migrator().HasColumn(&types.Chunk{}, column), column)
	}
	require.False(t, h.db.Migrator().HasTable("knowledge_strategy_versions"))
	require.Equal(t, []string{intakeReviewPrompt()}, h.model.prompts)
	require.NotContains(t, string(completed.Metadata), "classification")
}

func TestLocalURLArticleCompletes(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>URLfixture original publication</title></head><body><article><h1>URLfixture</h1><p>`+
			strings.Repeat("URLfixture Acme documents original deployment choices and measured widget product capabilities. ", 12)+`</p></article></body></html>`)
	}))
	defer server.Close()
	h := newRetainedIntakeHarness(t)
	article, err := h.service.CreateKnowledgeFromURL(h.ctx, h.kb.ID, server.URL+"/article", "", "", nil, "URLfixture", types.ChannelWeb, nil,
		types.SourceInfo{Publisher: "Acme", PublishedAt: "2026-10-01", OriginalURL: server.URL + "/article"})
	require.NoError(t, err)
	h.drain(t)
	completed := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusCompleted, completed.ParseStatus)
	require.Equal(t, types.SummaryStatusCompleted, completed.SummaryStatus)
	reader, _, err := h.service.GetKnowledgeBody(h.ctx, article.ID)
	require.NoError(t, err)
	defer reader.Close()
	body, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.Contains(t, string(body), "URLfixture")
}

func TestMalformedBuiltinReviewIsExecutionFailure(t *testing.T) {
	h := newRetainedIntakeHarness(t)
	h.model.malformed = true
	article, err := h.service.CreateKnowledgeFromFile(h.ctx, h.kb.ID,
		newMultipartFileHeader(t, "malformed.md", "Acme published the complete original product announcement with detailed deployment options."),
		map[string]string{"publisher": "Acme", "original_url": "https://example.com/malformedfixture", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
	require.NoError(t, err)
	require.Len(t, h.queue.tasks, 1)
	require.ErrorContains(t, h.service.ProcessDocument(h.ctx, h.queue.tasks[0]), "invalid intake review response")
	failed := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusFailed, failed.ParseStatus)
	intake, err := failed.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "error", intake.Decision)
	require.Empty(t, intake.OriginalPath)
	require.False(t, h.db.Migrator().HasTable("knowledge_strategy_versions"))
	require.Equal(t, []string{intakeReviewPrompt()}, h.model.prompts)
}

func TestRSSLocalFeedReviewRetryDiscardAndChangedURLSkip(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	h := newRetainedIntakeHarness(t)
	changed := false
	date := time.Now().UTC().Format(time.RFC1123Z)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feed.xml" {
			w.Header().Set("Content-Type", "application/rss+xml")
			fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Fixture publisher</title><link>http://%s</link><description>Original articles</description>`, r.Host)
			for _, marker := range []string{"approvedfixture", "rejectfixture", "discardfixture"} {
				fmt.Fprintf(w, `<item><title>%s</title><link>http://%s/%s</link><guid>%s</guid><pubDate>%s</pubDate><description>Read the complete original</description></item>`, marker, r.Host, marker, marker, date)
			}
			fmt.Fprint(w, `</channel></rss>`)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		text := "Acme publishes an original article explaining new widget features and deployment choices. "
		if r.URL.Path == "/rejectfixture" {
			text = "Independent commentators express unsupported opinions about the future of electric transportation and urban planning. "
		} else if r.URL.Path == "/discardfixture" {
			text = "A hobbyist discusses personal preferences about gardening herbs and the aesthetic qualities of seasonal flower arrangements. "
		}
		if changed {
			text = "Acme revised the source article to describe later product changes and fresh deployment options. "
		}
		fmt.Fprintf(w, `<html><head><title>%s</title><meta name="author" content="Ada"></head><body><article><h1>%s</h1><p>%s</p></article></body></html>`, r.URL.Path, r.URL.Path, strings.Repeat(text, 10))
	}))
	defer server.Close()
	registry := datasource.NewConnectorRegistry()
	require.NoError(t, registry.Register(rss.NewConnector()))
	dsRepo, logs := repository.NewDataSourceRepository(h.db), repository.NewSyncLogRepository(h.db)
	cfg := &types.DataSourceConfig{Type: types.ConnectorTypeRSS, Settings: map[string]interface{}{"feed_urls": server.URL + "/feed.xml", "publisher": "Fixture publisher"}}
	encoded, err := cfg.ToJSON()
	require.NoError(t, err)
	ds := &types.DataSource{ID: "feed", TenantID: h.kb.TenantID, KnowledgeBaseID: h.kb.ID, Type: types.ConnectorTypeRSS, Name: "Fixture publisher", Config: encoded,
		SyncMode: types.SyncModeFull, Status: types.DataSourceStatusActive, CreatedAt: time.Now().Add(-24 * time.Hour)}
	require.NoError(t, dsRepo.Create(h.ctx, ds))
	syncer := &DataSourceService{dsRepo: dsRepo, syncLogRepo: logs, knowledgeService: h.service, kbService: h.service.kbService,
		connectorRegistry: registry, tenantRepo: h.service.tenantRepo}
	sync := func(id string) *types.SyncLog {
		log := &types.SyncLog{ID: id, TenantID: h.kb.TenantID, DataSourceID: ds.ID, Status: types.SyncLogStatusRunning}
		require.NoError(t, logs.Create(h.ctx, log))
		payload, err := json.Marshal(types.DataSourceSyncPayload{TenantID: h.kb.TenantID, DataSourceID: ds.ID, SyncLogID: log.ID, ForceFull: true})
		require.NoError(t, err)
		require.NoError(t, syncer.ProcessSync(h.ctx, asynq.NewTask(types.TypeDataSourceSync, payload)))
		h.drain(t)
		log, err = logs.FindByID(h.ctx, id)
		require.NoError(t, err)
		return log
	}
	first := sync("first-sync")
	require.Equal(t, 3, first.ItemsCreated)
	var articles []types.Knowledge
	require.NoError(t, h.db.Order("title").Find(&articles).Error)
	require.Len(t, articles, 3)
	byTitle := make(map[string]types.Knowledge)
	for _, article := range articles {
		byTitle[article.Title] = article
	}
	approved, rejected, discarded := byTitle["approvedfixture"], byTitle["rejectfixture"], byTitle["discardfixture"]
	require.Equal(t, types.ParseStatusCompleted, approved.ParseStatus)
	require.Equal(t, types.ParseStatusRejected, rejected.ParseStatus)
	require.Equal(t, types.ParseStatusRejected, discarded.ParseStatus)
	waiting, err := h.service.ListPagedKnowledgeByKnowledgeBaseID(h.ctx, h.kb.ID, &types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{ParseStatus: types.ParseStatusRejected})
	require.NoError(t, err)
	require.EqualValues(t, 2, waiting.Total)
	original, _, err := h.service.GetKnowledgeBody(h.ctx, approved.ID)
	require.NoError(t, err)
	originalBody, err := io.ReadAll(original)
	require.NoError(t, err)
	require.NoError(t, original.Close())
	changed = true
	second := sync("second-sync")
	require.Equal(t, 3, second.ItemsSkipped)
	require.Zero(t, second.ItemsCreated)
	require.Zero(t, second.ItemsUpdated)
	var count int64
	require.NoError(t, h.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 3, count, "changed originals must not create review candidates")
	reader, _, err := h.service.GetKnowledgeBody(h.ctx, approved.ID)
	require.NoError(t, err)
	unchanged, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, reader.Close())
	require.Equal(t, originalBody, unchanged)
	// A changed feed entry identifier still resolves the existing normalized link.
	result := &types.SyncResult{}
	syncer.applyFetchedItem(h.ctx, ds, &types.FetchedItem{ExternalID: "different-guid", Title: "Changed original", Content: []byte(strings.Repeat("Changed complete original. ", 10)),
		URL: server.URL + "/approvedfixture?utm_source=feed", Metadata: map[string]string{"publisher": "Fixture publisher", "published_at": "2026-10-01"}}, result)
	require.Equal(t, 1, result.Skipped)
	require.NoError(t, h.db.Model(&types.Knowledge{}).Count(&count).Error)
	require.EqualValues(t, 3, count)
	h.model.approveRetry = true
	_, err = h.service.ReviewKnowledgeIntake(h.ctx, rejected.ID, "retry", types.SourceInfo{})
	require.NoError(t, err)
	h.drain(t)
	require.Equal(t, types.ParseStatusCompleted, h.article(t, rejected.ID).ParseStatus)
	_, err = h.service.ReviewKnowledgeIntake(h.ctx, discarded.ID, "discard", types.SourceInfo{})
	require.NoError(t, err)
	_, err = h.service.repo.GetKnowledgeByID(h.ctx, h.kb.TenantID, discarded.ID)
	require.ErrorIs(t, err, repository.ErrKnowledgeNotFound)
	waiting, err = h.service.ListPagedKnowledgeByKnowledgeBaseID(h.ctx, h.kb.ID, &types.Pagination{Page: 1, PageSize: 10}, types.KnowledgeListFilter{ParseStatus: types.ParseStatusRejected})
	require.NoError(t, err)
	require.Zero(t, waiting.Total)
}
