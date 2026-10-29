package modelcalls_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"reliquary/internal/maintenance"
	"reliquary/internal/modelcalls"
	"reliquary/internal/models/chat"
	"reliquary/internal/testutil/pgtest"
	"reliquary/internal/types"
	secutils "reliquary/internal/utils"
)

func TestEveryGenerationPurposePreservesActualWireBytes(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	secutils.ResetSSRFWhitelistForTest()
	t.Cleanup(secutils.ResetSSRFWhitelistForTest)
	var sent, returned string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		sent = string(b)
		returned = ` { "choices":[{"message":{"content":"invalid business JSON remains intact"},"finish_reason":"stop"}], "usage":{"prompt_tokens":3,"completion_tokens":2} } `
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, returned)
	}))
	defer server.Close()
	for _, purpose := range []string{"intake_review", "article_processing", "article_summary_merge", "image_description", "entity_resolution", "evaluation_generation"} {
		t.Run(purpose, func(t *testing.T) {
			rows := []types.ModelCall{}
			m := model(purpose, types.ModelTypeGeneration)
			m.Parameters.BaseURL = server.URL + "/v1"
			cfg := chat.ConfigFromModel(m)
			cfg.Accounting = accounting(m, &rows)
			c, err := chat.NewChat(cfg, nil)
			require.NoError(t, err)
			// Larger than the old accounting cap, including Unicode and whitespace.
			body := strings.Repeat("正文\n ABC\t", 1100000)
			ctx := types.WithModelCallTrace(callCtx(purpose), 4, "segment-span")
			_, err = c.Chat(ctx, []chat.Message{{Role: "system", Content: "unchanged prompt"}, {Role: "user", Content: body}}, nil)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			require.NotNil(t, rows[0].Content)
			require.Equal(t, sent, rows[0].Content.Request)
			require.Equal(t, returned, rows[0].Content.Response)
			require.Equal(t, 4, rows[0].ProcessingAttempt)
			require.Equal(t, "segment-span", rows[0].SpanID)
		})
	}
}
func TestRawFailuresRetriesAndPartialStreams(t *testing.T) {
	for _, tc := range []struct {
		name, response, typ string
		status              int
	}{
		{"invalid JSON", "  malformed\nresponse  ", "application/json", 200},
		{"error body", `{"error":"all error text"}`, "application/json", 500},
		{"interrupted stream", "data: {\"delta\":\"first\"}\n\ndata: {\"partial\":", "text/event-stream", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rows := []types.ModelCall{}
			ctx := types.WithModelCallAccounting(callCtx("article_processing"), accounting(model("raw", types.ModelTypeGeneration), &rows))
			var actual []string
			attempts := 0
			client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
				b, _ := io.ReadAll(r.Body)
				actual = append(actual, string(b))
				attempts++
				if attempts == 1 {
					return nil, fmt.Errorf("retry fixture")
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{"Content-Type": []string{tc.typ}}, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
			})})
			for i := 0; i < 2; i++ {
				req, _ := http.NewRequestWithContext(ctx, "POST", "https://model.example", strings.NewReader(` {"messages":[{"role":"user","content":"full body"}]} `))
				resp, err := client.Do(req)
				if err == nil {
					b, _ := io.ReadAll(resp.Body)
					require.Equal(t, tc.response, string(b))
					resp.Body.Close()
				}
			}
			require.Len(t, rows, 2)
			for i, row := range rows {
				require.Equal(t, i+1, row.Attempt)
				require.Equal(t, actual[i], row.Content.Request)
			}
			require.Empty(t, rows[0].Content.Response)
			require.Equal(t, tc.response, rows[1].Content.Response)
			require.Equal(t, tc.typ, rows[1].Content.ResponseType)
			if tc.typ == "text/event-stream" {
				require.False(t, rows[1].Content.Complete)
			}
		})
	}
}
func TestCredentialsAndImageBinariesExcluded(t *testing.T) {
	rows := []types.ModelCall{}
	m := model("security", types.ModelTypeGeneration)
	m.Parameters.APIKey = "model-private-key"
	m.Parameters.CustomHeaders = map[string]string{"X-Custom": "custom-secret"}
	hash := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	ctx := types.WithModelCallImages(callCtx("image_description"), []types.ModelCallImageReference{{Path: "attachments/entry-a/chart.png", SHA256: hash}})
	ctx = types.WithModelCallAccounting(ctx, accounting(m, &rows))
	request := `{"messages":[{"role":"user","content":"actual full text","image_url":"data:image/png;base64,aGVsbG8="}],"api_key":"body-secret","\u0061pi_key":"unicode-secret","nested":{"access_token":"nested-secret"}}`
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		b, _ := io.ReadAll(r.Body)
		require.Equal(t, request, string(b))
		return &http.Response{StatusCode: 401, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":"header-secret custom-secret model-private-key","token":"response-secret"}`))}, nil
	})})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://model.example?api_key=query-secret", strings.NewReader(request))
	req.Header.Set("Authorization", "Bearer header-secret")
	req.Header.Set("X-Custom", "custom-secret")
	resp, err := client.Do(req)
	require.NoError(t, err)
	io.ReadAll(resp.Body)
	resp.Body.Close()
	require.Len(t, rows, 1)
	encoded, _ := json.Marshal(rows[0].Content)
	for _, secret := range []string{"header-secret", "custom-secret", "model-private-key", "body-secret", "nested-secret", "response-secret", "query-secret", "unicode-secret", "aGVsbG8="} {
		require.NotContains(t, string(encoded), secret)
	}
	require.Contains(t, rows[0].Content.Request, "actual full text")
	require.Contains(t, rows[0].Content.Request, hash)
	require.Contains(t, rows[0].Content.Request, "attachments/entry-a/chart.png")
}
func TestContentStorageRetentionDeletionAndAudit(t *testing.T) {
	db := pgtest.Open(t)
	ctx := context.Background()
	store := modelcalls.NewStore(db)
	require.NoError(t, db.Exec(`INSERT INTO spaces(id,name,business) VALUES(1,'Content','personal');
 INSERT INTO libraries(id,space_id,name,template,directory,embedding_model_id,summary_model_id)
 VALUES('content-lib',1,'Content','standard','content','','');
 INSERT INTO entries(id,space_id,library_id,type,title,source) VALUES('content-entry',1,'content-lib','manual','Content','');`).Error)
	entry := "content-entry"
	ids := []string{}
	for attempt := 1; attempt <= 4; attempt++ {
		// Each processing attempt contains two real requests.
		for retry := 1; retry <= 2; retry++ {
			id := uuid.NewString()
			ids = append(ids, id)
			call := &types.ModelCall{ID: id, SpaceID: 1, EntryID: &entry, RequestedAt: time.Now().UTC().AddDate(0, 0, -5+attempt), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeArticleProcessing, Currency: "USD", CostStatus: "unknown", Attempt: retry, ProcessingAttempt: attempt, SpanID: fmt.Sprintf("span-%d", attempt), Content: &types.ModelCallContent{Request: strings.Repeat("full 正文\n", 100000), Response: "raw invalid JSON\n", ResponseType: "application/json", HTTPStatus: 200, Complete: true}}
			require.NoError(t, store.Write(ctx, call))
			got, err := store.Content(ctx, 1, id)
			require.NoError(t, err)
			require.Equal(t, call.Content.Request, got.Request)
			require.Equal(t, call.Content.Response, got.Response)
			_, err = store.Content(ctx, 2, id)
			require.ErrorIs(t, err, gorm.ErrRecordNotFound)
		}
	}
	require.NoError(t, store.PruneContents(ctx))
	var count int64
	db.Table("model_call_contents").Count(&count)
	require.EqualValues(t, 8, count)
	rows, total, err := store.List(ctx, modelcalls.Filter{SpaceID: 1, EntryID: entry}, 100, 0)
	require.NoError(t, err)
	require.EqualValues(t, 8, total)
	encoded, _ := json.Marshal(rows)
	require.NotContains(t, string(encoded), "full 正文")
	require.NotContains(t, string(encoded), "request_gzip")
	attempt := 3
	_, total, err = store.List(ctx, modelcalls.Filter{SpaceID: 1, EntryID: entry, ProcessingAttempt: &attempt}, 100, 0)
	require.NoError(t, err)
	require.EqualValues(t, 2, total)
	audit, err := maintenance.Audit(ctx, db)
	require.NoError(t, err)
	columns := map[string]bool{}
	deletionColumns := map[string]bool{}
	for _, row := range audit {
		if row.Table == "public.model_call_content_deletions" {
			require.Equal(t, "backup_only", row.Owner)
			deletionColumns[row.Column] = true
		}
		if row.Table == "public.model_call_contents" {
			require.Equal(t, "backup_only", row.Owner)
			columns[row.Column] = true
		}
	}
	require.Len(t, columns, 8)
	require.Len(t, deletionColumns, 2)
	require.True(t, columns["request_gzip"])
	require.True(t, columns["response_gzip"])
	t.Setenv("RELIQUARY_MODEL_CALL_CONTENT_RETENTION_ATTEMPTS", "2")
	require.NoError(t, store.PruneContents(ctx))
	db.Table("model_call_contents").Count(&count)
	require.EqualValues(t, 4, count)
	_, err = store.Content(ctx, 1, ids[0])
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	t.Setenv("RELIQUARY_MODEL_CALL_CONTENT_RETENTION_DAYS", "2")
	require.NoError(t, store.PruneContents(ctx))
	db.Table("model_call_contents").Count(&count)
	require.EqualValues(t, 2, count)
	require.NoError(t, db.Exec("DELETE FROM entries WHERE id=?", entry).Error)
	db.Table("model_call_contents").Count(&count)
	require.Zero(t, count)
	db.Table("model_calls").Count(&count)
	require.EqualValues(t, 8, count)
	// A delayed asynchronous write must not recreate contents after deletion.
	late := rows[0]
	late.ID = uuid.NewString()
	late.Content = &types.ModelCallContent{Request: "late content", Response: "late response"}
	require.NoError(t, store.Write(ctx, &late))
	db.Table("model_call_contents").Count(&count)
	require.Zero(t, count)
}
func TestContentAsyncFailureDoesNotBlock(t *testing.T) {
	db := pgtest.Open(t)
	store := modelcalls.NewStore(db)
	ctx := context.Background()
	started := time.Now()
	require.NoError(t, store.Enqueue(ctx, &types.ModelCall{ID: uuid.NewString()}))
	require.Less(t, time.Since(started), 100*time.Millisecond)
	flush, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	require.NoError(t, modelcalls.FlushWrites(flush))
}

func TestOptionalVectorAndRerankContentsAndLocalChat(t *testing.T) {
	for _, tc := range []struct {
		name, path, env, request, response string
		kind                               types.ModelType
		local                              bool
	}{
		{"embedding", "/v1/embeddings", "RELIQUARY_MODEL_CALL_CAPTURE_EMBEDDING", ` {"input":["full embedding text"]} `, `{"data":[{"embedding":[0.123,0.456,0.789]}]}`, types.ModelTypeEmbedding, false},
		{"rerank", "/v1/rerank", "RELIQUARY_MODEL_CALL_CAPTURE_RERANK", `{"query":"full query","documents":["full document"]}`, ` {"results":[{"index":0,"relevance_score":0.97}]} `, types.ModelTypeRerank, false},
		{"ollama", "/api/chat", "", `{"messages":[{"role":"user","content":"full local prompt"}]}`, `{"message":{"content":"full local output"},"done":true}`, types.ModelTypeGeneration, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, disabled := range []bool{false, true} {
				if tc.env == "" && disabled {
					continue
				}
				if tc.env != "" {
					if disabled {
						t.Setenv(tc.env, "false")
					} else {
						t.Setenv(tc.env, "")
					}
				}
				rows := []types.ModelCall{}
				m := model(tc.name, tc.kind)
				if tc.local {
					m.Source = types.ModelSourceLocal
				}
				client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
					raw, _ := io.ReadAll(req.Body)
					require.Equal(t, tc.request, string(raw))
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.response))}, nil
				})})
				ctx := types.WithModelCallAccounting(callCtx(tc.name), accounting(m, &rows))
				req, _ := http.NewRequestWithContext(ctx, "POST", "https://model.example"+tc.path, strings.NewReader(tc.request))
				resp, err := client.Do(req)
				require.NoError(t, err)
				raw, _ := io.ReadAll(resp.Body)
				resp.Body.Close()
				require.Equal(t, tc.response, string(raw))
				require.Len(t, rows, 1)
				if disabled {
					require.Nil(t, rows[0].Content)
					continue
				}
				require.Equal(t, tc.request, rows[0].Content.Request)
				if tc.kind == types.ModelTypeEmbedding {
					require.Contains(t, rows[0].Content.Response, `"dimensions":[3]`)
					require.NotContains(t, rows[0].Content.Response, "0.123")
				} else {
					require.Equal(t, tc.response, rows[0].Content.Response)
				}
			}
		})
	}
}
func TestResponseOverEightMiBAndUnreadDecoderTailArePreserved(t *testing.T) {
	rows := []types.ModelCall{}
	response := `{"long":"` + strings.Repeat("abcdef", 1500000) + `"}` + "\n   "
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(response))}, nil
	})})
	ctx := types.WithModelCallAccounting(callCtx("article_processing"), accounting(model("large", types.ModelTypeGeneration), &rows))
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://model.example", strings.NewReader("{}"))
	resp, err := client.Do(req)
	require.NoError(t, err)
	var decoded map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&decoded))
	require.NoError(t, resp.Body.Close())
	require.Len(t, rows, 1)
	require.Equal(t, response, rows[0].Content.Response)
	require.True(t, rows[0].Content.Complete)
}

func TestFailedDialPreservesCompleteRequestBeforeAnyTransportRead(t *testing.T) {
	rows := []types.ModelCall{}
	request := ` {"messages":[{"role":"user","content":"full input even before a dial failure"}]} `
	ctx := types.WithModelCallAccounting(callCtx("intake_review"), accounting(model("dial-failure", types.ModelTypeGeneration), &rows))
	client := modelcalls.WrapClient(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, fmt.Errorf("dial failed") })})
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://model.example", strings.NewReader(request))
	_, err := client.Do(req)
	require.Error(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, request, rows[0].Content.Request)
	require.Empty(t, rows[0].Content.Response)
}

func TestLedgerRemovalCascadesToContent(t *testing.T) {
	db := pgtest.Open(t)
	store := modelcalls.NewStore(db)
	ctx := context.Background()
	call := &types.ModelCall{ID: uuid.NewString(), SpaceID: 1, RequestedAt: time.Now(), ModelID: "m", ModelName: "m", Provider: "test", Purpose: types.CallPurposeOther, Currency: "USD", CostStatus: "unknown", Attempt: 1, Content: &types.ModelCallContent{Request: "full request", Response: "full response"}}
	require.NoError(t, store.Write(ctx, call))
	require.NoError(t, db.Delete(call).Error)
	var count int64
	require.NoError(t, db.Table("model_call_contents").Count(&count).Error)
	require.Zero(t, count)
}
