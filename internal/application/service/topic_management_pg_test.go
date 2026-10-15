package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reliquary/internal/testutil/redistest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"reliquary/internal/models/chat"
	taskqueue "reliquary/internal/queue"
	"reliquary/internal/types"
)

// topicJudgementChat inspects the actual batch payload, with controllable failure.
type topicJudgementChat struct {
	retainedProcessingChat
	mu       sync.Mutex
	inputs   []topicBackfillInput
	failCall int
	block    chan struct{}
}

func (m *topicJudgementChat) Chat(ctx context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	if len(messages) != 2 || messages[0].Content != topicBackfillPrompt {
		return nil, errors.New("unexpected historical classification prompt")
	}
	var input topicBackfillInput
	if err := json.Unmarshal([]byte(messages[1].Content), &input); err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.inputs = append(m.inputs, input)
	call := len(m.inputs)
	fail := m.failCall == call
	block := m.block
	m.mu.Unlock()
	if block != nil {
		select {
		case <-block:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if fail {
		return nil, errors.New("fixture model temporarily unavailable")
	}
	decisions := make([]map[string]any, 0, len(input.Articles))
	for _, article := range input.Articles {
		decisions = append(decisions, map[string]any{"id": article.ID, "matches": input.Topic.Description != "none"})
	}
	raw, err := json.Marshal(map[string]any{"decisions": decisions})
	if err != nil {
		return nil, err
	}
	return &types.ChatResponse{Content: string(raw)}, nil
}
func (m *topicJudgementChat) calls() []topicBackfillInput {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]topicBackfillInput(nil), m.inputs...)
}
func newTopicManagementService(t *testing.T, h *articleProcessingHarness) (*TopicService, *topicJudgementChat, context.Context) {
	t.Helper()
	model := &topicJudgementChat{}
	svc := NewTopicService(h.db)
	svc.SetModelService(&summaryStateModelService{model: model})
	svc.SetOperationLogs(NewOperationLogService(h.db))
	ctx := types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalWebUser, ID: "topic-owner"})
	return svc, model, ctx
}
func historicalTopicArticles(t *testing.T, h *articleProcessingHarness, count int) []types.Entry {
	t.Helper()
	articles := make([]types.Entry, 0, count)
	for i := 0; i < count; i++ {
		article := types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", Title: fmt.Sprintf("Historical article %02d", i), Description: fmt.Sprintf("Historical summary %02d about products", i), SourceKind: "article", ParseStatus: types.ParseStatusCompleted, EnableStatus: "enabled"}
		require.NoError(t, h.db.Create(&article).Error)
		articles = append(articles, article)
	}
	return articles
}
func runTopicJob(t *testing.T, svc *TopicService, ctx context.Context, id string) *types.TopicBackfillJob {
	t.Helper()
	job, err := svc.BackfillStatus(ctx, id)
	require.NoError(t, err)
	require.NoError(t, svc.runBackfillJob(ctx, *job))
	job, err = svc.BackfillStatus(ctx, id)
	require.NoError(t, err)
	return job
}
func topicLinks(t *testing.T, svc *TopicService, topicID string) []types.EntryTopic {
	t.Helper()
	result := []types.EntryTopic{}
	require.NoError(t, svc.db.Where("topic_id=?", topicID).Order("entry_id").Find(&result).Error)
	return result
}
func operationCount(t *testing.T, svc *TopicService) int64 {
	t.Helper()
	var count int64
	require.NoError(t, svc.db.Model(&types.OperationLog{}).Count(&count).Error)
	return count
}

func TestTopicManagementUniqueScopeDeletionRefreshAndUserLogs(t *testing.T) {
	h, v := newVaultArticleHarness(t)
	article := h.upload(t, "Managed topic", "# Managed article\n\nThe original body survives all topic changes.")
	_, _, originalBody := readVaultArticle(t, v, article)
	svc, _, ctx := newTopicManagementService(t, h)
	svc.SetHeaderRefresher(v)
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "  Research  ", Description: "Published research"})
	require.NoError(t, err)
	require.Equal(t, "Research", topic.Name)
	var persisted types.OperationLog
	require.NoError(t, svc.db.Where("operation_type=? AND object_id=?", "create_topic", topic.ID).First(&persisted).Error)
	require.Contains(t, string(persisted.After), `"tenant_id": 1`)
	require.NotContains(t, string(persisted.After), `"space_id"`)
	_, err = svc.CreateTopic(ctx, &types.Topic{Name: "Research"})
	require.Error(t, err)
	otherCtx := context.WithValue(ctx, types.SpaceIDContextKey, uint64(2))
	other, err := svc.CreateTopic(otherCtx, &types.Topic{Name: "Research"})
	require.NoError(t, err)
	require.Error(t, svc.DeleteTopic(otherCtx, topic.ID))
	require.Error(t, svc.SetUserEntryTopics(ctx, article.ID, []string{other.ID}))
	job := runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Len(t, topicLinks(t, svc, topic.ID), 1)
	require.Equal(t, int64(2), operationCount(t, svc), "automatic model association must not add a user log")
	items, err := svc.ListTopics(ctx)
	require.NoError(t, err)
	require.Len(t, items, 1)
	require.Equal(t, int64(1), items[0].EntryCount)
	name := "Research renamed"
	_, err = svc.UpdateTopic(ctx, topic.ID, &name, nil)
	require.NoError(t, err)
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
		return err == nil && strings.Contains(string(raw), "Research renamed")
	}, time.Second, 10*time.Millisecond)
	_, _, body := readVaultArticle(t, v, article)
	require.Equal(t, originalBody, body)
	_, err = svc.UpdateTopic(ctx, topic.ID, nil, nil)
	require.Error(t, err)
	emptyName := " "
	_, err = svc.UpdateTopic(ctx, topic.ID, &emptyName, nil)
	require.Error(t, err)
	apiCtx := types.WithPrincipal(ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "fixture-key"})
	require.NoError(t, svc.SetUserEntryTopics(apiCtx, article.ID, []string{topic.ID}))
	links := topicLinks(t, svc, topic.ID)
	require.Len(t, links, 1)
	require.Equal(t, types.TopicSourceUser, links[0].Source)
	require.NoError(t, svc.ReplaceEntryTopics(ctx, article.ID, nil))
	require.Len(t, topicLinks(t, svc, topic.ID), 1, "reprocessing keeps a manual topic")
	var apiLog types.OperationLog
	require.NoError(t, h.db.Where("operation_type='correct_topics'").First(&apiLog).Error)
	require.Equal(t, "api_key:fixture-key", apiLog.Actor)
	require.NoError(t, svc.DeleteTopic(ctx, topic.ID))
	require.Empty(t, topicLinks(t, svc, topic.ID))
	require.Eventually(t, func() bool {
		raw, err := os.ReadFile(filepath.Join(v.root, article.VaultPath))
		return err == nil && !strings.Contains(string(raw), "Research renamed")
	}, time.Second, 10*time.Millisecond)
	_, _, body = readVaultArticle(t, v, article)
	require.Equal(t, originalBody, body)
	var jobs int64
	require.NoError(t, h.db.Model(&types.TopicBackfillJob{}).Where("topic_id=?", topic.ID).Count(&jobs).Error)
	require.Zero(t, jobs)
	var records []types.OperationLog
	require.NoError(t, h.db.Order("created_at").Find(&records).Error)
	var actions []string
	for _, record := range records {
		actions = append(actions, record.OperationType)
	}
	require.Equal(t, []string{"create_topic", "create_topic", "update_topic", "correct_topics", "delete_topic"}, actions)
}

func TestTopicHistoricalBackfillDefaultBatchSingleTopicAndDescriptionRecompute(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, model, ctx := newTopicManagementService(t, h)
	articles := historicalTopicArticles(t, h, 65)
	for _, kind := range []string{"mine", "report"} {
		require.NoError(t, h.db.Create(&types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: types.EntryTypeManual, SourceKind: kind, Title: kind + " document", Description: "Also about products", ParseStatus: types.ParseStatusCompleted}).Error)
	}
	require.NoError(t, h.db.Create(&types.Entry{ID: uuid.NewString(), SpaceID: h.library.SpaceID, LibraryID: h.library.ID, Type: "url", Title: "Rejected article", Description: "Also about products", ParseStatus: types.ParseStatusRejected}).Error)
	_, err := svc.CreateTopic(ctx, &types.Topic{Name: "Other topic", Description: "Other description"})
	require.NoError(t, err)
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Target topic", Description: "Product announcements"})
	require.NoError(t, err)
	job := runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Equal(t, 66, job.Total)
	require.Equal(t, 66, job.Processed)
	require.Zero(t, job.FailedEntries)
	calls := model.calls()
	require.Len(t, calls, 3)
	var sizes []int
	for _, call := range calls {
		require.Equal(t, topic.ID, call.Topic.ID)
		require.Equal(t, "Target topic", call.Topic.Name)
		require.Equal(t, topic.Description, call.Topic.Description)
		sizes = append(sizes, len(call.Articles))
		for _, article := range call.Articles {
			require.NotEmpty(t, article.Title)
			require.NotEmpty(t, article.Summary)
			require.NotEqual(t, "report document", article.Title)
			require.NotEqual(t, "Rejected article", article.Title)
		}
	}
	sort.Ints(sizes)
	require.Equal(t, []int{6, 30, 30}, sizes)
	links := topicLinks(t, svc, topic.ID)
	require.Len(t, links, 66)
	for _, link := range links {
		require.Equal(t, types.TopicSourceModel, link.Source)
	}
	require.Equal(t, int64(2), operationCount(t, svc))
	require.NoError(t, svc.SetUserEntryTopics(ctx, articles[0].ID, []string{topic.ID}))
	description := "none"
	_, err = svc.UpdateTopic(ctx, topic.ID, nil, &description)
	require.NoError(t, err)
	links = topicLinks(t, svc, topic.ID)
	require.Len(t, links, 1)
	require.Equal(t, types.TopicSourceUser, links[0].Source)
	job = runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Len(t, model.calls(), 6)
	links = topicLinks(t, svc, topic.ID)
	require.Len(t, links, 1)
	require.Equal(t, articles[0].ID, links[0].EntryID)
	require.Equal(t, int64(4), operationCount(t, svc), "background reclassification never writes an operation log")
}

func TestTopicHistoricalBackfillConfiguredBatchesFailureAndRetry(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, model, ctx := newTopicManagementService(t, h)
	svc.SetBackfillBatchSize(2)
	model.failCall = 2
	historicalTopicArticles(t, h, 5)
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Retry topic", Description: "Product announcements"})
	require.NoError(t, err)
	job := runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "failed", job.Status)
	require.Equal(t, 5, job.Total)
	require.Equal(t, 5, job.Processed)
	require.Equal(t, 2, job.FailedEntries)
	require.Equal(t, 1, job.FailedBatches)
	require.Len(t, topicLinks(t, svc, topic.ID), 3)
	require.Len(t, model.calls(), 3)
	require.Contains(t, job.Batches[1].Error, "temporarily unavailable")
	job, err = svc.RetryBackfill(ctx, topic.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", job.Status)
	require.Equal(t, 3, job.Processed)
	job = runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Equal(t, 5, job.Processed)
	require.Zero(t, job.FailedBatches)
	require.Zero(t, job.FailedEntries)
	require.Len(t, topicLinks(t, svc, topic.ID), 5)
	require.Len(t, model.calls(), 4)
	require.Len(t, model.calls()[3].Articles, 2)
	require.Equal(t, []int{1, 2, 1}, []int{job.Batches[0].Attempts, job.Batches[1].Attempts, job.Batches[2].Attempts})
	_, err = svc.RetryBackfill(ctx, topic.ID)
	require.Error(t, err)
	require.Equal(t, int64(1), operationCount(t, svc))
}

func TestTopicHistoricalBackfillWorkerRestartsPersistedRunningBatch(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, _, ctx := newTopicManagementService(t, h)
	queue := redistest.NewClient(t, h.db)
	svc.SetTaskEnqueuer(queue)
	historicalTopicArticles(t, h, 3)
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Restart topic", Description: "Product announcements"})
	require.NoError(t, err)
	job, err := svc.BackfillStatus(ctx, topic.ID)
	require.NoError(t, err)
	require.NoError(t, h.db.Model(&types.TopicBackfillJob{}).Where("id=?", job.ID).Update("status", "running").Error)
	require.NoError(t, h.db.Model(&types.TopicBackfillBatch{}).Where("job_id=?", job.ID).Update("status", "running").Error)
	require.NoError(t, h.db.Model(&taskqueue.Job{}).Where("task_type=?", types.TypeTopicBackfill).Updates(map[string]any{"state": taskqueue.TaskStateActive, "dispatched_at": time.Now().Add(time.Hour), "worker_pool": "crashed-process"}).Error)
	require.NoError(t, queue.Start(context.Background(), taskqueue.HandlerFunc(svc.HandleBackfill), taskqueue.Config{}))
	t.Cleanup(func() { require.NoError(t, queue.Close()) })
	require.Eventually(t, func() bool {
		state, err := svc.BackfillStatus(ctx, topic.ID)
		return err == nil && state.Status == "completed" && state.Processed == 3
	}, 2*time.Second, 20*time.Millisecond)
	require.Len(t, topicLinks(t, svc, topic.ID), 3)
}

func TestTopicHistoricalClassificationRejectsMalformedAndIncompleteDecisions(t *testing.T) {
	articles := []topicBackfillArticle{{ID: "one", Title: "One", Summary: "One summary"}, {ID: "two", Title: "Two", Summary: "Two summary"}}
	for _, raw := range []string{"not json", `{"decisions":[]}`, `{"decisions":[{"id":"unknown","matches":true},{"id":"two","matches":false}]}`, `{"decisions":[{"id":"one","matches":true},{"id":"one","matches":false}]}`, `{"decisions":[{"id":"one"},{"id":"two","matches":false}]}`} {
		_, err := parseTopicBackfillResult(raw, articles)
		require.Error(t, err)
	}
	matched, err := parseTopicBackfillResult(`{"decisions":[{"id":"one","matches":false},{"id":"two","matches":true}]}`, articles)
	require.NoError(t, err)
	require.Equal(t, []string{"two"}, matched)
}

func TestTopicHistoricalBackfillIgnoresSupersededInFlightDecisions(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, model, ctx := newTopicManagementService(t, h)
	historicalTopicArticles(t, h, 2)
	model.block = make(chan struct{})
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Changing topic", Description: "Product announcements"})
	require.NoError(t, err)
	original, err := svc.BackfillStatus(ctx, topic.ID)
	require.NoError(t, err)
	result := make(chan error, 1)
	go func() { result <- svc.runBackfillJob(ctx, *original) }()
	require.Eventually(t, func() bool { return len(model.calls()) == 1 }, time.Second, 10*time.Millisecond)
	description := "none"
	_, err = svc.UpdateTopic(ctx, topic.ID, nil, &description)
	require.NoError(t, err)
	close(model.block)
	select {
	case err := <-result:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("superseded batch did not finish")
	}
	require.Empty(t, topicLinks(t, svc, topic.ID), "an old model result cannot restore removed associations")
	var oldJob types.TopicBackfillJob
	require.NoError(t, h.db.First(&oldJob, "id=?", original.ID).Error)
	require.Equal(t, "cancelled", oldJob.Status)
	job := runTopicJob(t, svc, ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Empty(t, topicLinks(t, svc, topic.ID))
}

func TestTopicHistoricalBackfillDuplicateQueueTasksDoNotResetALiveBatch(t *testing.T) {
	h := newArticleProcessingHarness(t, false)
	svc, model, ctx := newTopicManagementService(t, h)
	historicalTopicArticles(t, h, 1)
	model.block = make(chan struct{})
	topic, err := svc.CreateTopic(ctx, &types.Topic{Name: "Serialized topic", Description: "Product history"})
	require.NoError(t, err)
	job, err := svc.BackfillStatus(ctx, topic.ID)
	require.NoError(t, err)
	raw, err := json.Marshal(types.TopicBackfillPayload{SpaceID: types.SingleSpaceID, JobID: job.ID})
	require.NoError(t, err)
	task := taskqueue.NewTask(types.TypeTopicBackfill, raw)
	first := make(chan error, 1)
	go func() { first <- svc.HandleBackfill(ctx, task) }()
	require.Eventually(t, func() bool { return len(model.calls()) == 1 }, time.Second, 10*time.Millisecond)
	secondCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	require.Error(t, svc.HandleBackfill(secondCtx, task), "the duplicate execution waits for its existing live worker")
	require.Len(t, model.calls(), 1)
	state, err := svc.BackfillStatus(ctx, topic.ID)
	require.NoError(t, err)
	require.Equal(t, "running", state.Batches[0].Status)
	require.Equal(t, 1, state.Batches[0].Attempts)
	close(model.block)
	require.NoError(t, <-first)
	require.NoError(t, svc.HandleBackfill(ctx, task))
	require.Len(t, model.calls(), 1, "the completed queue replay does not repeat classification")
}
