package grokx

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"reliquary/internal/datasource"
	"reliquary/internal/types"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestGrokSearchCollectsCitedPostsAndRejectsUncitedPosts(t *testing.T) {
	since := time.Now().UTC().Add(-24 * time.Hour)
	published := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{"api_key": "fake-development-key"},
		Settings:    map[string]interface{}{"handle": "bcherny"},
		IngestSince: since,
	}
	envelope, err := json.Marshal(map[string]interface{}{
		"handle": "bcherny",
		"posts": []map[string]interface{}{
			{"tweet_id": "123", "url": "https://x.com/bcherny/status/123", "author_handle": "bcherny", "text": "Exact source text", "created_at": published.Format(time.RFC3339), "post_type": "original", "media": []map[string]string{{"type": "image", "url": "https://pbs.twimg.com/media/example.jpg"}}},
			{"tweet_id": "789", "url": "https://x.com/bcherny/status/789", "author_handle": "bcherny", "text": "Video post", "created_at": published.Format(time.RFC3339), "post_type": "original", "media": []map[string]string{{"type": "video", "url": "https://video.twimg.com/example.mp4"}}},
			{"tweet_id": "456", "url": "https://x.com/bcherny/status/456", "author_handle": "bcherny", "text": "Uncited text", "created_at": published.Format(time.RFC3339), "post_type": "original", "media": []interface{}{}},
			{"tweet_id": "999", "url": "https://x.com/bcherny/status/999", "author_handle": "bcherny", "text": "Original kept for review", "created_at": published.Format(time.RFC3339), "post_type": "original"},
		},
		"warnings": []string{},
	})
	if err != nil {
		t.Fatal(err)
	}
	c := NewConnector()
	c.client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer fake-development-key" {
			t.Fatal("API key not sent to xAI")
		}
		if req.URL.Path == "/v1/models" {
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"data":[]}`)), Header: make(http.Header)}, nil
		}
		if req.URL.Path != "/v1/responses" || req.Method != http.MethodPost {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
		}
		var body struct {
			Model string `json:"model"`
			Text  struct {
				Format struct {
					Type string `json:"type"`
				} `json:"format"`
			} `json:"text"`
			Tools []struct {
				Type    string   `json:"type"`
				Handles []string `json:"allowed_x_handles"`
				From    string   `json:"from_date"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Model != "grok-4.3" || body.Text.Format.Type != "json_object" || len(body.Tools) != 1 || body.Tools[0].Type != "x_search" || len(body.Tools[0].Handles) != 1 || body.Tools[0].Handles[0] != "bcherny" || body.Tools[0].From != since.Format("2006-01-02") {
			t.Fatalf("unexpected X Search scope: %+v", body.Tools)
		}
		response, err := json.Marshal(map[string]interface{}{"status": "completed", "usage": map[string]interface{}{"input_tokens": 42}, "output": []interface{}{map[string]interface{}{"type": "message", "content": []interface{}{map[string]interface{}{"text": string(envelope), "annotations": []interface{}{map[string]string{"url": "https://x.com/bcherny/status/123"}, map[string]string{"url": "https://x.com/bcherny/status/789"}, map[string]string{"url": "https://x.com/bcherny/status/999"}, map[string]string{"url": "https://x.com/dotey/status/456"}}}}}}})
		if err != nil {
			t.Fatal(err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(response))), Header: make(http.Header)}, nil
	})}
	if err := c.Validate(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	items, cursor, err := c.FetchIncremental(context.Background(), config, &types.SyncCursor{LastSyncTime: time.Now().UTC()})
	if len(items) != 4 || cursor == nil || cursor.LastSyncTime.Before(since) {
		t.Fatalf("cited post was not collected or cursor missing: items=%d cursor=%+v", len(items), cursor)
	}
	if items[0].ExternalID != "123" || items[0].URL != "https://x.com/bcherny/status/123" || !strings.Contains(string(items[0].Content), "Exact source text") || !strings.Contains(string(items[0].Content), "![X image](https://pbs.twimg.com/media/example.jpg)") || items[0].Metadata["author"] != "@bcherny" || items[0].Metadata["published_at"] != published.Format(time.RFC3339) {
		t.Fatalf("unexpected collected post: %+v", items[0])
	}
	if items[1].ExternalID != "789" || !strings.Contains(string(items[1].Content), "[X video](https://video.twimg.com/example.mp4)") {
		t.Fatalf("video post was not preserved: %+v", items[1])
	}
	if !strings.HasPrefix(items[3].ExternalID, "rejected:") || items[3].URL != "https://x.com/bcherny/status/999" || items[3].Metadata["intake_reject_reason"] != "media inventory is missing" || !strings.Contains(string(items[3].Content), "Original kept for review") {
		t.Fatalf("incomplete post was not retained for intake review: %+v", items[3])
	}
	if config.FetchMetrics["input_tokens"] != int64(42) || config.FetchMetrics["model"] != "grok-4.3" {
		t.Fatalf("Grok metrics were not exposed to sync result: %+v", config.FetchMetrics)
	}
	var partial *datasource.PartialFetchError
	if !errors.As(err, &partial) || !strings.Contains(err.Error(), "not cited") {
		t.Fatalf("uncited post was not rejected: %v", err)
	}
}

func TestGrokRejectsMissingKeyAndAPIFailure(t *testing.T) {
	c := NewConnector()
	if err := c.Validate(context.Background(), &types.DataSourceConfig{}); err == nil {
		t.Fatal("missing API key accepted")
	}
	if err := c.Validate(context.Background(), &types.DataSourceConfig{
		Credentials: map[string]interface{}{"api_key": "fake"},
		Settings:    map[string]interface{}{"base_url": "https://127.0.0.1/v1", "handle": "bcherny"},
	}); err == nil || !strings.Contains(err.Error(), "base_url") {
		t.Fatalf("local API URL was not rejected: %v", err)
	}
	c.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("unauthorized")), Header: make(http.Header)}, nil
	})}
	err := c.Validate(context.Background(), &types.DataSourceConfig{Credentials: map[string]interface{}{"api_key": "fake"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "fake") {
		t.Fatalf("unexpected auth failure: %v", err)
	}
}
