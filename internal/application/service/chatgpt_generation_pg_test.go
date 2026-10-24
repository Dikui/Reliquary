package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reliquary/internal/application/repository"
	"reliquary/internal/chatgpt"
	"reliquary/internal/config"
	"reliquary/internal/models/embedding"
	"reliquary/internal/testutil/redistest"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	taskqueue "reliquary/internal/queue"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type chatGPTGenerationInput struct {
	Role    string `json:"role"`
	Content []struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
	} `json:"content"`
}

type chatGPTGenerationRequest struct {
	Model        string                   `json:"model"`
	Instructions string                   `json:"instructions"`
	Input        []chatGPTGenerationInput `json:"input"`
	Store        bool                     `json:"store"`
	Stream       bool                     `json:"stream"`
}

func (r chatGPTGenerationRequest) stage() string {
	switch {
	case strings.HasPrefix(r.Instructions, intakeReviewPrompt()):
		return "intake_review"
	case strings.HasPrefix(r.Instructions, articleProcessingPrompt()):
		return "article_processing"
	case r.Instructions == topicBackfillPrompt():
		return "topic_backfill"
	case strings.HasPrefix(r.Instructions, "Decide whether the source mention is the exact same real-world entity"):
		return "entity_resolution"
	default:
		return "unknown"
	}
}

func (r chatGPTGenerationRequest) userText() string {
	var result strings.Builder
	for _, input := range r.Input {
		if input.Role == "user" {
			for _, part := range input.Content {
				if part.Type == "input_text" {
					result.WriteString(part.Text)
				}
			}
		}
	}
	return result.String()
}

type chatGPTGenerationTransport struct {
	server *url.URL
	base   http.RoundTripper
}

type chatGPTGenerationPooler struct{}

func (chatGPTGenerationPooler) BatchEmbedWithPool(ctx context.Context, model embedding.Embedder, texts []string) ([][]float32, error) {
	return model.BatchEmbed(ctx, texts)
}

func (tr chatGPTGenerationTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.String() != "https://api.openai.com/v1/responses" {
		return nil, fmt.Errorf("unexpected generation endpoint: %s", req.URL)
	}
	copy := req.Clone(req.Context())
	copy.URL.Scheme, copy.URL.Host = tr.server.Scheme, tr.server.Host
	copy.Host = tr.server.Host
	return tr.base.RoundTrip(copy)
}

type chatGPTGenerationHarness struct {
	*articleProcessingHarness
	models *modelService
	policy *ChatGPTPolicyService
	mu     sync.Mutex
	calls  []chatGPTGenerationRequest
}

func newChatGPTGenerationHarness(t *testing.T, reply func(chatGPTGenerationRequest) (string, string)) *chatGPTGenerationHarness {
	t.Helper()
	t.Setenv("SYSTEM_AES_KEY", "01234567890123456789012345678901")
	t.Setenv("SSRF_WHITELIST", "api.openai.com,127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	h := &chatGPTGenerationHarness{articleProcessingHarness: newArticleProcessingHarness(t, true)}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/embeddings" {
			var input struct {
				Input interface{} `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&input)
			count := 1
			if batch, ok := input.Input.([]interface{}); ok {
				count = len(batch)
			}
			data := make([]map[string]interface{}, 0, count)
			for index := range count {
				data = append(data, map[string]interface{}{"index": index, "embedding": []float32{1, 0, 0}, "object": "embedding"})
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"object": "list", "data": data, "usage": map[string]int{"prompt_tokens": 1, "total_tokens": 1}})
			return
		}
		var request chatGPTGenerationRequest
		var raw map[string]interface{}
		body, err := io.ReadAll(r.Body)
		if err != nil || json.Unmarshal(body, &request) != nil || json.Unmarshal(body, &raw) != nil {
			http.Error(w, "invalid test request", 400)
			return
		}
		if len(raw) != 5 || request.Store || !request.Stream || request.Model != "fixture-account-model" || r.Header.Get("Authorization") != "Bearer fixture-owned-oauth-token" {
			http.Error(w, "subscription request contract violation", 400)
			return
		}
		h.mu.Lock()
		h.calls = append(h.calls, request)
		h.mu.Unlock()
		output, code := reply(request)
		if code == "" && request.stage() == "article_processing" {
			var input articleProcessingInput
			var result map[string]any
			if json.Unmarshal([]byte(request.userText()), &input) == nil && json.Unmarshal([]byte(output), &result) == nil {
				descriptions := []extractedImage{}
				for _, image := range input.Images {
					descriptions = append(descriptions, extractedImage{Index: image.Index, Description: "Fixture chart contains calibration counts."})
				}
				result["image_descriptions"] = descriptions
				encoded, _ := json.Marshal(result)
				output = string(encoded)
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("x-request-id", "req_generation_fixture")
		if code != "" {
			data, _ := json.Marshal(map[string]interface{}{"type": "response.failed", "response": map[string]interface{}{"status": "failed", "error": map[string]interface{}{"code": code, "message": "Fixture ChatGPT quota is unavailable"}}})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			return
		}
		delta, _ := json.Marshal(map[string]string{"type": "response.output_text.delta", "delta": output})
		_, _ = fmt.Fprintf(w, "data: %s\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n", delta)
	}))
	t.Cleanup(server.Close)
	// OAuth grant-validation tests live in internal/chatgpt. This fixture is an
	// already-approved, encrypted session so every generation still loads its
	// credential through the real Manager and PostgreSQL repository.
	accountID := uuid.NewString()
	encrypted, err := utils.EncryptAESGCM(`{"access_token":"fixture-owned-oauth-token","refresh_token":"fixture-refresh-token","id_token":"fixture-saved-id-token"}`, utils.GetAESKey())
	require.NoError(t, err)
	require.NoError(t, h.db.Exec(`INSERT INTO chatgpt_accounts(id,client_id,issuer,subject,credentials,scopes,expires_at,status,welcome_acknowledged)
		VALUES(?,?,?,?,?,?,?,?,true)`, accountID, "client-"+accountID, "https://auth.openai.com", "fixture-subject", encrypted, chatgpt.RequestedScopes, time.Now().Add(time.Hour), chatgpt.StatusConnected).Error)
	manager := chatgpt.NewManager(h.db, chatgpt.WithHTTPClient(server.Client()), chatgpt.WithEndpoints(chatgpt.Endpoints{
		Issuer: server.URL, Authorization: server.URL + "/authorize", Token: server.URL + "/token", Discovery: server.URL + "/discovery", Models: server.URL + "/v1/models",
	}))
	h.policy = NewChatGPTPolicyService(h.db)
	h.models = NewModelServiceWithChatGPT(repository.NewModelRepository(h.db), repository.NewLibraryRepository(h.db), nil, chatGPTGenerationPooler{}, nil, h.db, manager, h.policy).(*modelService)
	parsed, err := url.Parse(server.URL)
	require.NoError(t, err)
	h.models.SetChatGPTTransport(&http.Client{Transport: chatGPTGenerationTransport{parsed, server.Client().Transport}})
	model := &types.Model{
		ID:      "chatgpt-generation",
		SpaceID: h.library.SpaceID,
		Name:    "fixture-account-model",
		Type:    types.ModelTypeGeneration,
		Source:  types.ModelSourceChatGPT,

		Parameters: types.ModelParameters{
			SupportsVision: true,
			ContextWindow:  32768,
			ExtraConfig:    map[string]string{"chatgpt_account_id": accountID},
		},
	}
	require.NoError(t, h.models.CreateModel(h.ctx, model))
	// Embeddings retain their own supported billing route and httptest fixture.
	var embedding types.Model
	require.NoError(t, h.db.First(&embedding, "id=?", "embedding").Error)
	embedding.Source = types.ModelSourceRemote
	embedding.Parameters.BaseURL, embedding.Parameters.Provider = server.URL+"/v1", "generic"
	require.NoError(t, h.db.Save(&embedding).Error)
	h.library.ModelTiers = types.ModelTierConfig{Light: model.ID, Generation: model.ID, EntityResolution: model.ID, Embedding: embedding.ID}
	h.library.SummaryModelID, h.library.EmbeddingModelID = model.ID, embedding.ID
	require.NoError(t, h.db.Save(h.library).Error)
	require.NoError(t, h.db.Model(&types.Space{}).Where("id=?", h.library.SpaceID).Update("embedding_model_id", h.library.EmbeddingModelID).Error)
	h.service.modelService = h.models
	h.service.chunkService.(*chunkService).modelService = h.models
	return h
}

func (h *chatGPTGenerationHarness) requests() []chatGPTGenerationRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]chatGPTGenerationRequest(nil), h.calls...)
}

func chatGPTGenerationSuccess(t *testing.T, request chatGPTGenerationRequest) (string, string) {
	t.Helper()
	switch request.stage() {
	case "intake_review":
		return `{"approved":true,"reason":"Complete original publication"}`, ""
	case "article_processing":
		return articleProcessingOutput(t, "event", []types.EntityCandidate{{Name: "Fixture Acme", EntityType: "company"}}, []string{}), ""
	case "entity_resolution":
		return `{"decision":"new","confidence":1}`, ""
	case "topic_backfill":
		var input topicBackfillInput
		if err := json.Unmarshal([]byte(request.userText()), &input); err != nil {
			return "invalid topic request", ""
		}
		decisions := make([]map[string]interface{}, 0, len(input.Articles))
		for _, article := range input.Articles {
			decisions = append(decisions, map[string]interface{}{"id": article.ID, "matches": true})
		}
		output, _ := json.Marshal(map[string]interface{}{"decisions": decisions})
		return string(output), ""
	default:
		return "unexpected original prompt", ""
	}
}

func TestChatGPTGenerationRunsProcessingEntityJudgementAndTopicHistory(t *testing.T) {
	h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
		if request.stage() == "entity_resolution" {
			return `{"decision":"match","entity_id":"existing-acme","confidence":0.99}`, ""
		}
		return chatGPTGenerationSuccess(t, request)
	})
	// Identity judgment needs a candidate; empty recall now creates directly.
	existing := entityRecord("existing-acme", "Fixture Acme", "organization")
	existing.SpaceID = h.library.SpaceID
	require.NoError(t, h.db.Create(&existing).Error)
	image := articleImageDataURI(t, 22)
	article := h.upload(t, "chatgpt-original", "# Original product announcement\n\nFixture Acme published measured product capabilities and deployment details.\n\n![chart]("+image+")")
	require.Equal(t, types.ParseStatusCompleted, article.ParseStatus)
	require.Equal(t, "event", article.ContentType)
	require.NotNil(t, article.StructuredSummary)
	require.Len(t, article.StructuredSummary.Points, 3)
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	var entities []types.Entity
	require.NoError(t, h.db.Find(&entities).Error)
	require.Len(t, entities, 1)
	require.Equal(t, "Fixture Acme", entities[0].CanonicalName)
	topics := NewTopicService(h.db)
	topics.SetModelService(h.models)
	topic, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Original product announcements", Description: "Measured product capabilities"})
	require.NoError(t, err)
	job := runTopicJob(t, topics, h.ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Len(t, topicLinks(t, topics, topic.ID), 1)

	requests := h.requests()
	require.Len(t, requests, 3, "each retained generation path must reach the real ChatGPT model service")
	stages := make([]string, 0, len(requests))
	for _, request := range requests {
		stages = append(stages, request.stage())
		for _, input := range request.Input {
			require.NotEqual(t, "system", input.Role)
		}
		if request.stage() == "article_processing" {
			require.True(t, strings.HasPrefix(request.Instructions, libraryProcessingPrompt(h.library, &types.Entry{})))
			require.Len(t, request.Input, 1)
			require.Len(t, request.Input[0].Content, 2)
			require.Equal(t, "input_image", request.Input[0].Content[1].Type)
			require.Equal(t, image, request.Input[0].Content[1].ImageURL)
		}
	}
	require.Equal(t, []string{"article_processing", "entity_resolution", "topic_backfill"}, stages)
	policy, err := h.policy.Get(h.ctx)
	require.NoError(t, err)
	require.Equal(t, 3, policy.DailyUsed)
	_, err = h.models.GetEmbeddingModel(h.ctx, "chatgpt-generation")
	require.Error(t, err, "subscription generation model cannot supply embedding")
}

func TestChatGPTQuotaDefersEnrichmentAndPreservesSearchableBody(t *testing.T) {
	for _, code := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable"} {
		for _, stage := range []string{"article_processing"} {
			t.Run(code+"/"+stage, func(t *testing.T) {
				var quota atomic.Bool
				quota.Store(true)
				h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
					if quota.Load() && request.stage() == stage {
						return "", code
					}
					return chatGPTGenerationSuccess(t, request)
				})
				article, err := h.service.createLegacyFileFixture(h.ctx, h.library.ID,
					newMultipartFileHeader(t, "quota-original.md", "Fixture Acme published an original product announcement with measured capabilities."),
					map[string]string{"publisher": "Fixture Acme", "original_url": "https://example.org/quota-original", "published_at": "2026-10-01"}, nil, "", types.ChannelWeb, nil)
				require.NoError(t, err)
				require.Len(t, h.queue.tasks, 1)
				task := h.queue.tasks[0]
				err = h.service.ProcessDocument(types.WithTaskRetryMetadata(h.ctx, 0, 3), task)
				var deferred *types.ChatGPTDeferredError
				require.ErrorAs(t, err, &deferred)
				require.True(t, deferred.RetryAt.After(time.Now()))
				require.Equal(t, code, deferred.Reason)
				pending := h.article(t, article.ID)
				require.Equal(t, types.ParseStatusCompleted, pending.ParseStatus)
				require.Equal(t, types.SummaryStatusFailed, pending.SummaryStatus)
				intake, err := pending.IntakeMetadata()
				require.NoError(t, err)
				require.NotEqual(t, "error", intake.Decision)
				require.NotEqual(t, "rejected", intake.Decision)
				require.Contains(t, pending.VaultBody, "Fixture Acme published an original product announcement with measured capabilities.")
				policy, err := h.policy.Get(h.ctx)
				require.NoError(t, err)
				require.True(t, policy.Paused)
				require.Equal(t, code, policy.Reason)
				before := len(h.requests())
				require.True(t, types.IsChatGPTDeferred(h.service.ProcessDocument(types.WithTaskRetryMetadata(h.ctx, 0, 3), task)))
				require.Len(t, h.requests(), before, "paused generation must not contact the external API")
				quota.Store(false)
				policy, err = h.policy.Resume(h.ctx)
				require.NoError(t, err)
				require.False(t, policy.Paused)
				require.NoError(t, h.service.ProcessDocument(types.WithTaskRetryMetadata(h.ctx, 0, 3), task))
				require.Equal(t, types.ParseStatusCompleted, h.article(t, article.ID).ParseStatus)
			})
		}
	}
}

func TestChatGPTQuotaDefersHistoricalTopicBatchWithoutOrdinaryFailureAttempt(t *testing.T) {
	var quota atomic.Bool
	h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
		if quota.Load() && request.stage() == "topic_backfill" {
			return "", "subscription_sharing_usage_limit_exceeded"
		}
		return chatGPTGenerationSuccess(t, request)
	})
	h.upload(t, "topic-quota", "Fixture Acme published an original technical announcement with complete source details and measured capabilities.")
	topics := NewTopicService(h.db)
	topics.SetModelService(h.models)
	topic, err := topics.CreateTopic(h.ctx, &types.Topic{Name: "Products", Description: "Product capabilities"})
	require.NoError(t, err)
	quota.Store(true)
	job := runTopicJob(t, topics, h.ctx, topic.ID)
	require.Equal(t, "running", job.Status)
	require.Zero(t, job.FailedBatches)
	var batch types.TopicBackfillBatch
	require.NoError(t, h.db.Where("job_id=?", job.ID).First(&batch).Error)
	require.Equal(t, "pending", batch.Status)
	require.Zero(t, batch.Attempts)
	require.NotNil(t, batch.RetryAt)
	require.True(t, batch.RetryAt.After(time.Now()))
	before := len(h.requests())
	require.NoError(t, topics.runBackfillJob(h.ctx, *job))
	require.Len(t, h.requests(), before, "a deferred historical batch must wait for its scheduled retry")
	quota.Store(false)
	_, err = h.policy.Resume(context.Background())
	require.NoError(t, err)
	// Simulate the scheduled delayed retry becoming due without waiting in a test.
	require.NoError(t, h.db.Model(&batch).Update("retry_at", time.Now().Add(-time.Second)).Error)
	job = runTopicJob(t, topics, h.ctx, topic.ID)
	require.Equal(t, "completed", job.Status)
	require.Zero(t, job.FailedBatches)
	require.Len(t, topicLinks(t, topics, topic.ID), 1)
}

func chatGPTScheduledQueue(t *testing.T, h *chatGPTGenerationHarness) *taskqueue.Client {
	t.Helper()
	client := redistest.NewClient(t, h.db)
	h.service.task = client
	return client
}

func chatGPTScheduledTasks(ctx context.Context, client *taskqueue.Client) ([]*taskqueue.TaskInfo, error) {
	var jobs []taskqueue.Job
	err := client.DB().WithContext(ctx).Where("queue=? AND state=?", types.QueueDefault, taskqueue.TaskStateScheduled).Order("created_at").Find(&jobs).Error
	result := make([]*taskqueue.TaskInfo, 0, len(jobs))
	for i := range jobs {
		result = append(result, jobs[i].Info())
	}
	return result, err
}

func TestChatGPTQuotaIsHandledAfterIngestAndKeepsBodySearchable(t *testing.T) {
	var quota atomic.Bool
	quota.Store(true)
	h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
		if quota.Load() && request.stage() == "article_processing" {
			return "", "subscription_sharing_usage_limit_exceeded"
		}
		return chatGPTGenerationSuccess(t, request)
	})
	h.service.libraryService = ingestFixtureLibrary{retainedProcessingLibrary: retainedProcessingLibrary{library: h.library}, db: h.db}
	spaceID := h.library.SpaceID
	require.NoError(t, h.db.Create(&types.APIKey{ID: 19, SpaceID: &spaceID, Name: "ChatGPT fixture collector", KeyHash: "chatgpt-fixture-collector"}).Error)
	h.ctx = types.WithPrincipal(h.ctx, types.Principal{Type: types.PrincipalAPIKey, ID: "19"})
	vault, err := NewVaultService(h.db, &config.Config{Vault: &config.VaultConfig{Root: t.TempDir()}})
	require.NoError(t, err)
	h.service.SetVault(vault)
	t.Cleanup(func() { require.NoError(t, vault.Close()) })
	inspector := chatGPTScheduledQueue(t, h)
	result, err := h.service.Ingest(h.ctx, &types.IngestRequest{Library: "official", Content: "Fixture Acme published original product measurements and deployment details."})
	require.NoError(t, err)
	require.Equal(t, "accepted", result.Status)
	require.Empty(t, h.requests(), "saving never calls the quota-limited model")
	var job taskqueue.Job
	require.NoError(t, inspector.DB().Where("object_id=? AND task_type=?", result.EntryID, types.TypeManualProcess).First(&job).Error)
	task := taskqueue.NewTask(job.TaskType, job.Payload)
	require.True(t, types.IsChatGPTDeferred(h.service.ProcessManualUpdate(h.ctx, task)))
	saved := h.article(t, result.EntryID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.Equal(t, types.SummaryStatusFailed, saved.SummaryStatus)
	require.Equal(t, "enabled", saved.EnableStatus)
	before := len(h.requests())
	require.True(t, types.IsChatGPTDeferred(h.service.ProcessManualUpdate(h.ctx, task)))
	require.Len(t, h.requests(), before, "paused generation must not contact the API")
	quota.Store(false)
	_, err = h.policy.Resume(h.ctx)
	require.NoError(t, err)
	require.NoError(t, h.service.ProcessManualUpdate(h.ctx, task))
	require.Equal(t, types.SummaryStatusCompleted, h.article(t, result.EntryID).SummaryStatus)
}

func TestLegacyPendingSourceSupplementDoesNotCallReviewModel(t *testing.T) {
	h := newChatGPTGenerationHarness(t, func(request chatGPTGenerationRequest) (string, string) {
		require.NotEqual(t, "intake_review", request.stage())
		return chatGPTGenerationSuccess(t, request)
	})
	article := h.upload(t, "legacy-source", "Fixture Acme published original measurements and deployment details.")
	intake, err := article.IntakeMetadata()
	require.NoError(t, err)
	intake.Decision, intake.Reason = "rejected", "Old missing-source policy"
	intake.SourceInfo.OriginalURL = ""
	article.Source, article.NormalizedURL = "", ""
	require.NoError(t, article.SetIntakeMetadata(intake))
	article.ParseStatus = types.ParseStatusRejected
	require.NoError(t, h.service.repo.UpdateEntry(h.ctx, article))
	original := article.VaultBody
	_, err = h.service.ReviewEntryIntake(h.ctx, article.ID, "supplement_url", types.SourceInfo{OriginalURL: "https://example.org/supplemented-original"})
	require.NoError(t, err)
	h.drain(t)
	saved := h.article(t, article.ID)
	require.Equal(t, types.ParseStatusCompleted, saved.ParseStatus)
	require.Equal(t, original, saved.VaultBody)
	intake, err = saved.IntakeMetadata()
	require.NoError(t, err)
	require.Equal(t, "approved", intake.Decision)
	require.Equal(t, "https://example.org/supplemented-original", intake.SourceInfo.OriginalURL)
}
