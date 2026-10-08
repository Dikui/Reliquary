package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	"reliquary/internal/types/interfaces"
)

type sourceChangesChat struct {
	*templateCaptureChatModel
	hook func()
	err  error
}

func (m *sourceChangesChat) Chat(ctx context.Context, messages []chat.Message, options *chat.ChatOptions) (*types.ChatResponse, error) {
	if m.hook != nil {
		m.hook()
	}
	if m.err != nil {
		return nil, m.err
	}
	return m.templateCaptureChatModel.Chat(ctx, messages, options)
}

func TestWikiSourceChangesRegenerateAndIgnore(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	chunk := sourceChangeChunk(t, db, "Immutable parser content")
	workingCopy := "Current editable working copy used for the Wiki page"
	require.NoError(t, db.Model(chunk).Update("content", workingCopy).Error)
	disabled := sourceChangeChunk(t, db, "Disabled chunk content must not be included")
	require.NoError(t, db.Model(disabled).Update("is_enabled", false).Error)
	page := sourceChangePage(t, db, "concept/source", chunk.ID, disabled.ID)
	repo := repository.NewWikiPageRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	changedAt := time.Now().UTC().Truncate(time.Microsecond)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID, disabled.ID}, changedAt))
	page, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	queue := &wikiGuardTaskQueue{}
	pending := &wikiKBGuardPendingRepo{accepted: true}
	wikiSvc := NewWikiPageService(repo, chunkRepo, nil, pending, nil)
	queued, err := wikiSvc.RegeneratePage(ctx, page.KnowledgeBaseID, page.Slug, queue)
	require.NoError(t, err)
	require.Len(t, queued.SourceChanges, 2, "queueing must preserve the warning")
	require.Len(t, pending.guardedOps, 1)
	require.Equal(t, WikiOpRegenerate, pending.guardedOps[0].Op)
	require.Len(t, queue.tasks, 1)
	require.Equal(t, types.TypeWikiIngest, queue.tasks[0].Type())
	var op WikiPendingOp
	require.NoError(t, json.Unmarshal(pending.guardedOps[0].Payload, &op))
	model := &sourceChangesChat{templateCaptureChatModel: &templateCaptureChatModel{response: "SUMMARY: refreshed summary\n# Updated Wiki\nGenerated from the current working copy."}}
	ingest := &wikiIngestService{wikiService: wikiSvc, chunkRepo: chunkRepo}
	require.NoError(t, ingest.regenerateSourcePage(ctx, model, WikiIngestPayload{TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID}, op, ingest.newWikiBatchContext(page.KnowledgeBaseID, nil)))
	require.Len(t, model.messages, 2)
	request := model.messages[1].Content
	require.Contains(t, request, workingCopy)
	require.NotContains(t, request, "Immutable parser content")
	require.NotContains(t, request, disabled.Content)
	require.NotContains(t, request, "Existing Wiki content")
	regenerated, err := wikiSvc.GetPageBySlug(ctx, page.KnowledgeBaseID, page.Slug)
	require.NoError(t, err)
	require.Empty(t, regenerated.SourceChanges)
	require.Contains(t, regenerated.Content, "Generated from the current working copy")
	require.Equal(t, page.ChunkRefs, regenerated.ChunkRefs, "disabled references remain to detect re-enabling")
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID}, changedAt.Add(time.Minute)))
	ignored, err := wikiSvc.IgnoreSourceChanges(ctx, page.KnowledgeBaseID, page.Slug)
	require.NoError(t, err)
	require.Empty(t, ignored.SourceChanges)
	require.Equal(t, regenerated.Content, ignored.Content)
	require.Equal(t, regenerated.Version, ignored.Version)
}

func TestWikiSourceRegenerationKeepsFailedAndNewerChanges(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(map[bool]string{false: "new_edit_during_generation", true: "generation_failure"}[failure], func(t *testing.T) {
			db := pgtest.Open(t)
			ctx := sourceChangeContext()
			chunk := sourceChangeChunk(t, db, "Current source content")
			page := sourceChangePage(t, db, "concept/racing", chunk.ID)
			repo := repository.NewWikiPageRepository(db)
			chunkRepo := repository.NewChunkRepository(db)
			initial := time.Now().UTC().Truncate(time.Microsecond)
			require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID}, initial))
			page, err := repo.GetByID(ctx, page.ID)
			require.NoError(t, err)
			model := &sourceChangesChat{templateCaptureChatModel: &templateCaptureChatModel{response: "SUMMARY: refreshed\nRegenerated content"}}
			if failure {
				model.err = errors.New("provider denied request")
			} else {
				model.hook = func() {
					require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID}, initial.Add(time.Minute)))
				}
			}
			wikiSvc := NewWikiPageService(repo, chunkRepo, nil, nil, nil)
			ingest := &wikiIngestService{wikiService: wikiSvc, chunkRepo: chunkRepo}
			err = ingest.regenerateSourcePage(ctx, model, WikiIngestPayload{TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID}, WikiPendingOp{PageSlug: page.Slug, SourceChanges: page.SourceChanges}, ingest.newWikiBatchContext(page.KnowledgeBaseID, nil))
			updated, readErr := repo.GetByID(ctx, page.ID)
			require.NoError(t, readErr)
			require.Contains(t, updated.SourceChanges, chunk.ID)
			if failure {
				require.Error(t, err)
				require.Equal(t, page.Content, updated.Content)
				require.Equal(t, initial, updated.SourceChanges[chunk.ID])
			} else {
				require.NoError(t, err)
				require.Equal(t, initial.Add(time.Minute), updated.SourceChanges[chunk.ID])
			}
		})
	}
}

func TestWikiSourceRegenerationWithOnlyDisabledChunks(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	chunk := sourceChangeChunk(t, db, "Disabled source")
	require.NoError(t, db.Model(chunk).Update("is_enabled", false).Error)
	page := sourceChangePage(t, db, "concept/disabled", chunk.ID)
	repo := repository.NewWikiPageRepository(db)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID}, time.Now()))
	page, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	chunkRepo := repository.NewChunkRepository(db)
	wikiSvc := NewWikiPageService(repo, chunkRepo, nil, nil, nil)
	ingest := &wikiIngestService{wikiService: wikiSvc, chunkRepo: chunkRepo}
	require.NoError(t, ingest.regenerateSourcePage(ctx, nil, WikiIngestPayload{TenantID: 1, KnowledgeBaseID: page.KnowledgeBaseID}, WikiPendingOp{PageSlug: page.Slug, SourceChanges: page.SourceChanges}, nil))
	updated, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, updated.SourceChanges)
	require.Empty(t, updated.Content)
	require.Equal(t, page.ChunkRefs, updated.ChunkRefs)
}

func TestWikiLintReportsModifiedSourcesAndDeletedChunkReferences(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	live := sourceChangeChunk(t, db, "Live source")
	deleted := sourceChangeChunk(t, db, "Deleted source")
	page := sourceChangePage(t, db, "concept/stale", live.ID, deleted.ID)
	repo := repository.NewWikiPageRepository(db)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{live.ID}, time.Now()))
	chunkRepo := repository.NewChunkRepository(db)
	require.NoError(t, chunkRepo.DeleteChunk(ctx, 1, deleted.ID))
	wikiSvc := NewWikiPageService(repo, chunkRepo, nil, nil, nil)
	kbSvc := &wikiGuardKBService{kb: &types.KnowledgeBase{ID: page.KnowledgeBaseID, IndexingStrategy: types.IndexingStrategy{WikiEnabled: true}}}
	report, err := NewWikiLintService(wikiSvc, kbSvc, nil).RunLint(ctx, page.KnowledgeBaseID)
	require.NoError(t, err)
	modified, missing := false, false
	for _, issue := range report.Issues {
		if issue.Type == LintIssueSourceModified && issue.PageSlug == page.Slug {
			modified = true
			require.False(t, issue.AutoFixable)
		}
		if issue.Type == LintIssueStaleRef && issue.TargetSlug == deleted.ID {
			missing = true
			require.True(t, strings.Contains(issue.Description, "deleted chunk"))
			require.False(t, issue.AutoFixable)
		}
	}
	require.True(t, modified)
	require.True(t, missing, "deleting an individual chunk must surface through stale_ref lint")
	marked, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.NotContains(t, marked.SourceChanges, deleted.ID, "deletion uses lint instead of a source warning")
}

type sourceChangesKBService struct {
	*wikiGuardKBService
}

func (s sourceChangesKBService) GetKnowledgeBaseByID(context.Context, string) (*types.KnowledgeBase, error) {
	return s.kb, nil
}

type sourceChangesModelService struct {
	interfaces.ModelService
	model chat.Chat
}

func (s sourceChangesModelService) GetChatModel(context.Context, string) (chat.Chat, error) {
	return s.model, nil
}

func TestWikiSourceRegenerationWorkerDrainsDurableOperation(t *testing.T) {
	db := pgtest.Open(t)
	ctx := sourceChangeContext()
	chunk := sourceChangeChunk(t, db, "Current enabled working copy")
	page := sourceChangePage(t, db, "concept/worker", chunk.ID)
	require.NoError(t, db.Exec("INSERT INTO knowledge_domains (id, tenant_id, name) VALUES ('source-domain', 1, 'Source')").Error)
	require.NoError(t, db.Exec(`INSERT INTO knowledge_bases (id, tenant_id, name, domain_id, embedding_model_id, summary_model_id)
		VALUES (?, 1, 'Source', 'source-domain', '', '')`, page.KnowledgeBaseID).Error)
	repo := repository.NewWikiPageRepository(db)
	chunkRepo := repository.NewChunkRepository(db)
	pending := repository.NewTaskPendingOpsRepository(db)
	queue := &wikiGuardTaskQueue{}
	wikiSvc := NewWikiPageService(repo, chunkRepo, nil, pending, nil)
	require.NoError(t, repo.MarkSourceChanged(ctx, 1, []string{chunk.ID}, time.Now()))
	_, err := wikiSvc.RegeneratePage(ctx, page.KnowledgeBaseID, page.Slug, queue)
	require.NoError(t, err)
	count, err := pending.PendingCount(ctx, wikiTaskType, wikiTaskScope, page.KnowledgeBaseID)
	require.NoError(t, err)
	require.EqualValues(t, 1, count)
	model := &templateCaptureChatModel{response: "SUMMARY: Worker regenerated\nCurrent generated Wiki body"}
	kb := &types.KnowledgeBase{
		ID: page.KnowledgeBaseID, TenantID: 1,
		IndexingStrategy: types.IndexingStrategy{WikiEnabled: true},
		WikiConfig:       &types.WikiConfig{SynthesisModelID: "source-model"},
	}
	ingest := &wikiIngestService{
		db: db, wikiService: wikiSvc, chunkRepo: chunkRepo, pendingRepo: pending, task: queue,
		kbService:    sourceChangesKBService{wikiGuardKBService: &wikiGuardKBService{kb: kb}},
		modelService: sourceChangesModelService{model: model},
	}
	require.NoError(t, ingest.ProcessWikiIngest(ctx, asynq.NewTask(types.TypeWikiIngest, queue.tasks[0].Payload())))
	updated, err := repo.GetByID(ctx, page.ID)
	require.NoError(t, err)
	require.Empty(t, updated.SourceChanges)
	require.Contains(t, updated.Content, "Current generated Wiki body")
	count, err = pending.PendingCount(ctx, wikiTaskType, wikiTaskScope, page.KnowledgeBaseID)
	require.NoError(t, err)
	require.Zero(t, count, "successful explicit regeneration must consume its durable queue row")
}
