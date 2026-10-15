package modelcalls_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"reliquary/internal/modelcalls"
	"reliquary/internal/models/chat"
	"reliquary/internal/models/embedding"
	"reliquary/internal/models/rerank"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

func TestMain(m *testing.M)    { os.Exit(pgtest.Run(m)) }
func price(v float64) *float64 { return &v }
func model(name string, typ types.ModelType) *types.Model {
	return &types.Model{ID: name, Name: name, SpaceID: 1, Type: typ, Source: types.ModelSourceRemote, Parameters: types.ModelParameters{Provider: "openai", Pricing: types.ModelPricing{InputPerMillion: price(2), OutputPerMillion: price(4), Currency: "USD"}}}
}
func accounting(m *types.Model, rows *[]types.ModelCall) *types.ModelCallAccounting {
	return &types.ModelCallAccounting{Model: m, Write: func(_ context.Context, c *types.ModelCall) error { *rows = append(*rows, *c); return nil }}
}
func callCtx(purpose string) context.Context {
	return types.WithModelCallEntry(types.WithLLMCallMetadata(context.Background(), purpose, ""), "entry-a")
}

func TestProviderPaths(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/chat/completions"):
			fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
		case strings.HasSuffix(r.URL.Path, "/embeddings"):
			fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"total_tokens":30}}`)
		case strings.HasSuffix(r.URL.Path, "/services/rerank/text-rerank") || r.URL.Path == "/api/v1":
			fmt.Fprint(w, `{"output":{"results":[{"index":0,"relevance_score":1}]},"usage":{"total_tokens":40}}`)
		case strings.HasSuffix(r.URL.Path, "/rerank"):
			fmt.Fprint(w, `{"results":[{"index":0,"relevance_score":1}],"usage":{"total_tokens":40}}`)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	rows := []types.ModelCall{}
	m := model("chat", types.ModelTypeGeneration)
	m.Parameters.BaseURL = server.URL + "/api/v1"
	cfg := chat.ConfigFromModel(m)
	cfg.Accounting = accounting(m, &rows)
	c, err := chat.NewChat(cfg, nil)
	require.NoError(t, err)
	_, err = c.Chat(callCtx("intake_review"), []chat.Message{{Role: "user", Content: "hello"}}, nil)
	require.NoError(t, err)
	_, err = c.Chat(callCtx("image_description"), []chat.Message{{Role: "user", Content: "describe", Images: []string{"data:image/png;base64,aGVsbG8="}}}, nil)
	require.NoError(t, err)
	m = model("embedding", types.ModelTypeEmbedding)
	m.Parameters.BaseURL = server.URL + "/v1"
	m.Parameters.EmbeddingParameters.Dimension = 3
	ec := embedding.ConfigFromModel(m)
	ec.Accounting = accounting(m, &rows)
	e, err := embedding.NewEmbedder(ec, nil, nil)
	require.NoError(t, err)
	_, err = e.Embed(callCtx("document_embedding"), "hello")
	require.NoError(t, err)
	m = model("rerank", types.ModelTypeRerank)
	m.Parameters.BaseURL = server.URL + "/v1"
	rc := rerank.ConfigFromModel(m, "")
	rc.Accounting = accounting(m, &rows)
	r, err := rerank.NewReranker(rc)
	require.NoError(t, err)
	_, err = r.Rerank(callCtx("rerank"), "hello", []string{"world"})
	require.NoError(t, err)
	require.Len(t, rows, 4)
	for i, p := range []types.ModelCallPurpose{types.CallPurposeIntakeReview, types.CallPurposeImageDescription, types.CallPurposeBodyEmbedding, types.CallPurposeRerank} {
		require.Equal(t, p, rows[i].Purpose)
		require.Equal(t, "entry-a", *rows[i].EntryID)
		require.True(t, rows[i].Success)
		require.Equal(t, "calculated", rows[i].CostStatus)
	}
	require.EqualValues(t, 100, *rows[0].InputTokens)
	require.InDelta(t, 0.00028, *rows[0].Cost, 1e-10)
	require.EqualValues(t, 30, *rows[2].InputTokens)
	require.Nil(t, rows[2].OutputTokens)
	m = model("aliyun", types.ModelTypeRerank)
	m.Parameters.BaseURL = server.URL + "/api/v1"
	m.Parameters.Provider = "aliyun"
	ac := rerank.ConfigFromModel(m, "")
	ac.Accounting = accounting(m, &rows)
	ar, err := rerank.NewReranker(ac)
	require.NoError(t, err)
	_, err = ar.Rerank(callCtx("rerank"), "hello", []string{"world"})
	require.NoError(t, err)
	require.Len(t, rows, 5)
	require.EqualValues(t, 40, *rows[4].InputTokens)
	require.Equal(t, "aliyun", rows[4].Provider)
	m = model("local-rerank", types.ModelTypeRerank)
	m.Source = types.ModelSourceLocal
	m.Parameters.BaseURL = server.URL + "/v1"
	lc := rerank.ConfigFromModel(m, "")
	lc.Accounting = accounting(m, &rows)
	lr, err := rerank.NewReranker(lc)
	require.NoError(t, err)
	_, err = lr.Rerank(callCtx("rerank"), "hello", []string{"world"})
	require.NoError(t, err)
	require.Len(t, rows, 6)
	require.Equal(t, "local-rerank", rows[5].ModelID)
	cfg.Accounting.Write = func(context.Context, *types.ModelCall) error { return errors.New("accounting database unavailable") }
	response, err := c.Chat(callCtx("other"), []chat.Message{{Role: "user", Content: "business succeeds"}}, nil)
	require.NoError(t, err)
	require.Equal(t, "ok", response.Content)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestRetriesStreamUnknownAndFailureIsolation(t *testing.T) {
	rows := []types.ModelCall{}
	m := model("chat", types.ModelTypeGeneration)
	a := accounting(m, &rows)
	tries := 0
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		tries++
		if tries < 3 {
			return nil, errors.New("connection failed")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"))}, nil
	})})
	ctx := types.WithModelCallAccounting(callCtx("article_processing"), a)
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(ctx, "POST", "https://models.example/v1/chat/completions", nil)
		resp, err := client.Do(req)
		if err == nil {
			_, _ = io.ReadAll(resp.Body)
			require.NoError(t, resp.Body.Close())
		}
	}
	require.Len(t, rows, 3)
	for i, c := range rows {
		require.Equal(t, i+1, c.Attempt)
		require.False(t, c.Success)
		require.Nil(t, c.InputTokens)
		require.Nil(t, c.Cost)
	}
	require.Equal(t, "interrupted", rows[2].ErrorCategory)
	// Write failure must leave the response and usage bytes intact.
	a.Write = func(context.Context, *types.ModelCall) error { return errors.New("database unavailable") }
	client = modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))}, nil
	})})
	req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("other"), a), "POST", "https://models.example", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Contains(t, string(raw), "prompt_tokens")
	require.NoError(t, resp.Body.Close())
}

func TestPriceSnapshotAndSubscription(t *testing.T) {
	db := pgtest.Open(t)
	store := modelcalls.NewStore(db)
	m := model("priced", types.ModelTypeGeneration)
	a := &types.ModelCallAccounting{Model: m, Write: store.Write}
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":1000000,"completion_tokens":1000000}}`))}, nil
	})})
	invoke := func() {
		req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("article_processing"), a), "POST", "https://models.example", nil)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	invoke()
	*m.Parameters.Pricing.InputPerMillion = 20
	invoke()
	m.Source = types.ModelSourceChatGPT
	invoke()
	rows, total, err := store.List(context.Background(), modelcalls.Filter{SpaceID: 1}, 100, 0)
	require.NoError(t, err)
	require.EqualValues(t, 3, total)
	require.Equal(t, "not_applicable", rows[0].CostStatus)
	require.Nil(t, rows[0].Cost)
	require.InDelta(t, 24, *rows[1].Cost, 1e-10)
	require.InDelta(t, 6, *rows[2].Cost, 1e-10)
	require.InDelta(t, 2, *rows[2].InputPrice, 1e-10)
}

func TestEmbeddingActualRetries(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			conn, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[{"index":0,"embedding":[1,0,0]}],"usage":{"prompt_tokens":5}}`)
	}))
	defer server.Close()
	rows := []types.ModelCall{}
	m := model("retry", types.ModelTypeEmbedding)
	m.Parameters.BaseURL = server.URL
	m.Parameters.EmbeddingParameters.Dimension = 3
	cfg := embedding.ConfigFromModel(m)
	cfg.Accounting = accounting(m, &rows)
	e, err := embedding.NewEmbedder(cfg, nil, nil)
	require.NoError(t, err)
	_, err = e.Embed(callCtx("entity_embedding"), "name")
	require.NoError(t, err)
	require.Len(t, rows, 3)
	for i, c := range rows {
		require.Equal(t, i+1, c.Attempt)
		require.Equal(t, types.CallPurposeEntityEmbedding, c.Purpose)
		require.Equal(t, "entry-a", *c.EntryID)
	}
	require.Equal(t, "transport", rows[0].ErrorCategory)
	require.Equal(t, "transport", rows[1].ErrorCategory)
	require.True(t, rows[2].Success)
	require.EqualValues(t, 5, *rows[2].InputTokens)
}

func TestStreamUsageAndUnknownZero(t *testing.T) {
	for _, tc := range []struct {
		name, body, contentType string
		known, success          bool
	}{
		{"unknown", `{"choices":[]}`, "application/json", false, true},
		{"zero", `{"usage":{"prompt_tokens":0,"completion_tokens":0}}`, "application/json", true, true},
		{"complete", "data: {\"usage\":{\"prompt_tokens\":2,\"completion_tokens\":3}}\n\ndata: [DONE]\n\n", "text/event-stream", true, true},
		{"responses", "data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":3}}}\n\n", "text/event-stream", true, true},
		{"anthropic", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":0}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"message_stop\"}\n\n", "text/event-stream", true, true},
		{"provider-error", "data: {\"type\":\"response.failed\"}\n\n", "text/event-stream", false, false},
		{"ollama", "{\"done\":true,\"prompt_eval_count\":2,\"eval_count\":3}\n", "application/x-ndjson", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []types.ModelCall{}
			a := accounting(model("a", types.ModelTypeGeneration), &rows)
			client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{tc.contentType}}, Body: io.NopCloser(strings.NewReader(tc.body))}, nil
			})})
			req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("article_processing"), a), "POST", "https://models.example", nil)
			resp, err := client.Do(req)
			require.NoError(t, err)
			// Tiny reads exercise events split across TCP reads.
			_, err = io.CopyBuffer(io.Discard, resp.Body, make([]byte, 3))
			require.NoError(t, err)
			require.NoError(t, resp.Body.Close())
			require.Len(t, rows, 1)
			require.Equal(t, tc.success, rows[0].Success)
			if tc.known {
				require.NotNil(t, rows[0].InputTokens)
				require.NotNil(t, rows[0].OutputTokens)
			} else {
				require.Nil(t, rows[0].InputTokens)
				require.Nil(t, rows[0].OutputTokens)
			}
		})
	}
}

func TestChatGPTLoginGenerationAccounting(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "api.openai.com")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	rows := []types.ModelCall{}
	var sent, received string
	m := model("subscription", types.ModelTypeGeneration)
	m.Source = types.ModelSourceChatGPT
	cfg := chat.ConfigFromModel(m)
	cfg.Accounting = accounting(m, &rows)
	cfg.ChatGPTToken = func(context.Context) (string, error) { return "surrogate-token", nil }
	cfg.ChatGPTHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(req.Body)
		sent = string(raw)
		response := &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"test\",\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":10,\"output_tokens\":3,\"total_tokens\":13}}}\n\n"))}
		rawResponse, _ := io.ReadAll(response.Body)
		received = string(rawResponse)
		response.Body = io.NopCloser(strings.NewReader(received))
		return response, nil
	})}
	c, err := chat.NewChat(cfg, nil)
	require.NoError(t, err)
	_, err = c.Chat(callCtx("evaluation_generation"), []chat.Message{{Role: "user", Content: "generate"}}, nil)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, sent, rows[0].Content.Request)
	require.Equal(t, received, rows[0].Content.Response)
	require.NotContains(t, rows[0].Content.Request, "surrogate-token")
	require.True(t, rows[0].Success)
	require.Equal(t, types.CallPurposeEvaluation, rows[0].Purpose)
	require.Equal(t, "not_applicable", rows[0].CostStatus)
	require.Nil(t, rows[0].Cost)
	require.EqualValues(t, 10, *rows[0].InputTokens)
}

func TestInterruptedProviderChatStream(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"id\":\"partial\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}]}\n\n")
	}))
	defer server.Close()
	rows := []types.ModelCall{}
	m := model("stream", types.ModelTypeGeneration)
	m.Parameters.BaseURL = server.URL + "/v1"
	cfg := chat.ConfigFromModel(m)
	cfg.Accounting = accounting(m, &rows)
	c, err := chat.NewChat(cfg, nil)
	require.NoError(t, err)
	stream, err := c.ChatStream(types.WithModelCallRetry(callCtx("image_description"), 2), []chat.Message{{Role: "user", Content: "describe"}}, nil)
	require.NoError(t, err)
	for range stream {
	}
	require.Len(t, rows, 1)
	require.Equal(t, "interrupted", rows[0].ErrorCategory)
	require.False(t, rows[0].Success)
	require.Equal(t, 2, rows[0].RetryNumber)
	require.Nil(t, rows[0].InputTokens)
}

func TestDecoderCloseAfterCompleteJSON(t *testing.T) {
	rows := []types.ModelCall{}
	a := accounting(model("decoder", types.ModelTypeGeneration), &rows)
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"usage":{"prompt_tokens":10,"completion_tokens":3}}`))}, nil
	})})
	req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("article_processing"), a), "POST", "https://models.example", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	var result map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.NoError(t, resp.Body.Close())
	require.Len(t, rows, 1)
	require.True(t, rows[0].Success)
	require.EqualValues(t, 10, *rows[0].InputTokens)
}

func TestAnthropicCacheInputTokens(t *testing.T) {
	rows := []types.ModelCall{}
	a := accounting(model("anthropic", types.ModelTypeGeneration), &rows)
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":10,"output_tokens":3,"cache_read_input_tokens":20,"cache_creation_input_tokens":5}}`))}, nil
	})})
	req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("article_processing"), a), "POST", "https://models.example", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, rows, 1)
	require.EqualValues(t, 35, *rows[0].InputTokens)
	require.EqualValues(t, 3, *rows[0].OutputTokens)
}

func TestCancelledStreamIsRecordedWithoutConsumerReads(t *testing.T) {
	recorded := make(chan types.ModelCall, 2)
	a := &types.ModelCallAccounting{Model: model("cancelled", types.ModelTypeGeneration), Write: func(_ context.Context, c *types.ModelCall) error { recorded <- *c; return nil }}
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {}\n\n"))}, nil
	})})
	ctx, cancel := context.WithCancel(types.WithModelCallAccounting(callCtx("article_processing"), a))
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://models.example", nil)
	resp, err := client.Do(req)
	require.NoError(t, err)
	cancel()
	select {
	case c := <-recorded:
		require.False(t, c.Success)
		require.Equal(t, "interrupted", c.ErrorCategory)
		require.Nil(t, c.InputTokens)
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled stream was not recorded")
	}
	require.NoError(t, resp.Body.Close())
	require.Empty(t, recorded)
}

func TestHTTPFailuresKeepTheirCategory(t *testing.T) {
	for code, category := range map[int]string{400: "http", 401: "authentication", 429: "rate_limit", 500: "provider"} {
		rows := []types.ModelCall{}
		a := accounting(model("http-error", types.ModelTypeGeneration), &rows)
		client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"upstream error"}}`))}, nil
		})})
		req, _ := http.NewRequestWithContext(types.WithModelCallAccounting(callCtx("other"), a), "POST", "https://models.example", nil)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_, err = io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Len(t, rows, 1)
		require.False(t, rows[0].Success)
		require.Equal(t, category, rows[0].ErrorCategory)
	}
}
