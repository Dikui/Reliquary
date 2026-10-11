package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/application/repository"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
)

type chatGPTTestTransport func(*http.Request) (*http.Response, error)

func (f chatGPTTestTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChatGPTQuotaPausesCompletedOnlyAndManualRecovery(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "api.openai.com,127.0.0.1")
	for _, code := range []string{"subscription_sharing_usage_limit_exceeded", "subscription_sharing_usage_unavailable"} {
		t.Run(code, func(t *testing.T) {
			db := pgtest.Open(t)
			policy := NewChatGPTPolicyService(db)
			now := time.Date(2026, 10, 11, 4, 0, 0, 0, time.UTC)
			policy.now = func() time.Time { return now }
			var requests atomic.Int32
			var recovered atomic.Bool
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				if recovered.Load() {
					fmt.Fprint(w, "data: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"{\\\"ok\\\":true}\"}]}]}}\n\n")
					return
				}
				payload, _ := json.Marshal(map[string]any{"type": "response.failed", "response": map[string]any{"error": map[string]any{"code": code, "message": "fixture limit"}}})
				fmt.Fprintf(w, "data: %s\n\n", payload)
			}))
			defer server.Close()
			base, _ := url.Parse(server.URL)
			client := &http.Client{Transport: chatGPTTestTransport(func(r *http.Request) (*http.Response, error) {
				copy := r.Clone(r.Context())
				u := *r.URL
				u.Scheme, u.Host = base.Scheme, base.Host
				copy.URL = &u
				return server.Client().Transport.RoundTrip(copy)
			})}
			inner, err := chat.NewChat(&chat.ChatConfig{Source: types.ModelSourceChatGPT, ModelName: "fixture", ChatGPTToken: func(context.Context) (string, error) { return "fixture-token", nil }, ChatGPTHTTPClient: client}, nil)
			require.NoError(t, err)
			model := &chatGPTBudgetChat{inner: inner, policy: policy}
			messages := []chat.Message{{Role: "user", Content: "test"}}
			_, err = model.Chat(context.Background(), messages, nil)
			require.True(t, types.IsChatGPTDeferred(err))
			require.True(t, types.IsChatGPTQuotaError(err))
			restart := NewChatGPTPolicyService(db)
			restart.now = func() time.Time { return now }
			p, err := restart.Get(context.Background())
			require.NoError(t, err)
			require.True(t, p.Paused)
			require.Equal(t, code, p.Reason)
			_, err = model.Chat(context.Background(), messages, nil)
			require.True(t, types.IsChatGPTDeferred(err))
			require.Equal(t, int32(1), requests.Load())
			recovered.Store(true)
			if code == "subscription_sharing_usage_unavailable" {
				now = now.Add(5 * time.Minute)
			} else {
				_, err = policy.Resume(context.Background())
				require.NoError(t, err)
			}
			response, err := model.Chat(context.Background(), messages, nil)
			require.NoError(t, err)
			require.JSONEq(t, `{"ok":true}`, response.Content)
			require.Equal(t, int32(2), requests.Load())
		})
	}
}

func TestChatGPTDailyLimitAtomicAndNextUTCDay(t *testing.T) {
	db := pgtest.Open(t)
	s := NewChatGPTPolicyService(db)
	now := time.Date(2026, 10, 11, 23, 59, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	limit := 3
	_, err := s.Update(context.Background(), &limit, "")
	require.NoError(t, err)
	var accepted atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Reserve(context.Background())
			if err == nil {
				accepted.Add(1)
			} else if !types.IsChatGPTDeferred(err) {
				t.Errorf("reserve: %v", err)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(3), accepted.Load())
	p, err := s.Resume(context.Background())
	require.NoError(t, err)
	require.True(t, p.Paused)
	require.Equal(t, 3, p.DailyUsed)
	now = now.Add(2 * time.Minute)
	p, err = s.Get(context.Background())
	require.NoError(t, err)
	require.False(t, p.Paused)
	require.Zero(t, p.DailyUsed)
	_, err = s.Update(context.Background(), nil, "")
	require.NoError(t, err)
	for i := 0; i < 5; i++ {
		_, err = s.Reserve(context.Background())
		require.NoError(t, err)
	}
	zero := 0
	_, err = s.Update(context.Background(), &zero, "")
	require.Error(t, err)
}

type chatGPTFallbackFixture struct{ calls int }

type chatGPTQuotaFixture struct{ chatGPTFallbackFixture }

func (*chatGPTQuotaFixture) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	return nil, &types.ChatGPTError{Kind: "failed", Code: "subscription_sharing_usage_limit_exceeded", Message: "fixture quota"}
}

func (c *chatGPTFallbackFixture) GetModelName() string { return "local" }
func (c *chatGPTFallbackFixture) GetModelID() string   { return "local" }
func (c *chatGPTFallbackFixture) Chat(context.Context, []chat.Message, *chat.ChatOptions) (*types.ChatResponse, error) {
	c.calls++
	return &types.ChatResponse{Content: `{"local":true}`, FinishReason: "stop"}, nil
}
func (c *chatGPTFallbackFixture) ChatStream(context.Context, []chat.Message, *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	return nil, errors.New("unused")
}

func TestChatGPTConfiguredFallbackAndPausePersists(t *testing.T) {
	db := pgtest.Open(t)
	s := NewChatGPTPolicyService(db)
	local := &types.Model{ID: "fallback", TenantID: types.SingleTenantID, Name: "local-fixture", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceLocal, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(local).Error)
	_, err := s.Update(context.Background(), nil, local.ID)
	require.NoError(t, err)
	_, err = s.Pause(context.Background(), &types.ChatGPTError{Code: "subscription_sharing_usage_limit_exceeded"})
	require.NoError(t, err)
	fallback := &chatGPTFallbackFixture{}
	model := &chatGPTBudgetChat{inner: fallback, policy: s, fallback: func(_ context.Context, id string) (chat.Chat, error) {
		require.Equal(t, local.ID, id)
		return fallback, nil
	}}
	response, err := model.Chat(context.Background(), nil, nil)
	require.NoError(t, err)
	require.JSONEq(t, `{"local":true}`, response.Content)
	require.Equal(t, 1, fallback.calls)
	p, err := s.Get(context.Background())
	require.NoError(t, err)
	require.True(t, p.Paused)
	require.Zero(t, p.DailyUsed)
	remote := *local
	remote.ID = "bad-fallback"
	remote.Source = types.ModelSourceRemote
	require.NoError(t, db.Create(&remote).Error)
	_, err = s.Update(context.Background(), nil, remote.ID)
	require.ErrorContains(t, err, "active local")
	_, err = s.Resume(context.Background())
	require.NoError(t, err)
	model.inner = &chatGPTQuotaFixture{}
	response, err = model.Chat(context.Background(), nil, nil)
	require.NoError(t, err, "the call that encounters the quota must switch to the configured fallback")
	require.JSONEq(t, `{"local":true}`, response.Content)
	require.Equal(t, 2, fallback.calls)
	p, err = s.Get(context.Background())
	require.NoError(t, err)
	require.True(t, p.Paused)
	require.Equal(t, 1, p.DailyUsed)
	model.fallback = func(context.Context, string) (chat.Chat, error) { return nil, errors.New("local service unavailable") }
	_, err = model.Chat(context.Background(), nil, nil)
	require.True(t, types.IsChatGPTDeferred(err), "an unavailable fallback must preserve the pending task during a quota pause")
	require.ErrorContains(t, err, "local service unavailable")
}

func TestChatGPTFallbackModelReferenceAndSeparateCredentials(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.WithValue(context.Background(), types.TenantIDContextKey, types.SingleTenantID)
	models := NewModelServiceWithRoutingDB(repository.NewModelRepository(db), repository.NewKnowledgeBaseRepository(db), nil, nil, nil, db)
	local := &types.Model{ID: "fallback-reference", TenantID: types.SingleTenantID, Name: "local-fixture", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceLocal, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(local).Error)
	policy := NewChatGPTPolicyService(db)
	_, err := policy.Update(ctx, nil, local.ID)
	require.NoError(t, err)
	require.ErrorContains(t, models.DeleteModel(ctx, local.ID), "fallback selection")
	for _, change := range []string{"source", "type", "status"} {
		invalid := *local
		switch change {
		case "source":
			invalid.Source = types.ModelSourceRemote
		case "type":
			invalid.Type = types.ModelTypeEmbedding
		case "status":
			invalid.Status = types.ModelStatusDownloadFailed
		}
		require.ErrorContains(t, models.UpdateModel(ctx, &invalid), "fallback selection")
	}
	_, err = policy.Update(ctx, nil, "")
	require.NoError(t, err)
	require.NoError(t, models.DeleteModel(ctx, local.ID))
	subscription := &types.Model{ID: "oauth-model", TenantID: types.SingleTenantID, Name: "fixture", Type: types.ModelTypeKnowledgeQA, Source: types.ModelSourceChatGPT, Status: types.ModelStatusActive}
	require.NoError(t, db.Create(subscription).Error)
	key := "unrelated-api-key"
	_, err = models.UpdateModelCredentials(ctx, subscription.ID, &key, nil)
	require.ErrorContains(t, err, "ChatGPT sign-in")
	var stored types.Model
	require.NoError(t, db.First(&stored, "id=?", subscription.ID).Error)
	require.Empty(t, stored.Parameters.APIKey)
}

func TestChatGPTQuotaRemainsDeferredWhenPausePersistenceFails(t *testing.T) {
	db := pgtest.Open(t)
	policy := NewChatGPTPolicyService(db)
	_, err := policy.Get(context.Background())
	require.NoError(t, err)
	require.NoError(t, db.Exec("ALTER TABLE chatgpt_policy ADD CONSTRAINT fixture_cannot_pause CHECK (NOT paused)").Error)
	model := &chatGPTBudgetChat{inner: &chatGPTQuotaFixture{}, policy: policy}
	_, err = model.Chat(context.Background(), nil, nil)
	require.True(t, types.IsChatGPTQuotaError(err))
	require.True(t, types.IsChatGPTDeferred(err), "a persistence fault must not turn a quota pause into a destructive ordinary failure")
	require.ErrorContains(t, err, "fixture_cannot_pause")
}
