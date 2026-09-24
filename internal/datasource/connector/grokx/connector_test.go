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

func TestGrokDiscoveryNeverTreatsModelTextAsOriginal(t *testing.T) {
	since := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	config := &types.DataSourceConfig{
		Credentials: map[string]interface{}{"api_key": "fake-development-key"},
		Settings:    map[string]interface{}{"handle": "bcherny"},
		IngestSince: since,
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
			Tools []struct {
				Type    string   `json:"type"`
				Handles []string `json:"allowed_x_handles"`
				From    string   `json:"from_date"`
			} `json:"tools"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if len(body.Tools) != 1 || body.Tools[0].Type != "x_search" || len(body.Tools[0].Handles) != 1 || body.Tools[0].Handles[0] != "bcherny" || body.Tools[0].From != "2026-09-23" {
			t.Fatalf("unexpected X Search scope: %+v", body.Tools)
		}
		response := `{"status":"completed","output":[{"type":"message","content":[{"text":"Model-generated alleged original post","annotations":[{"url":"https://x.com/bcherny/status/123"},{"url":"https://x.com/other/status/456"}]}]}],"usage":{"input_tokens":13,"cost_in_usd_ticks":150000,"server_side_tool_usage_details":{"x_posts_fetched":2}}}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
	})}
	if err := c.Validate(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	items, cursor, err := c.FetchIncremental(context.Background(), config, nil)
	if len(items) != 0 || cursor == nil || cursor.LastSyncTime.Before(since) {
		t.Fatalf("model output was ingested or cursor missing: items=%d cursor=%+v", len(items), cursor)
	}
	var partial *datasource.PartialFetchError
	if !errors.As(err, &partial) || !strings.Contains(err.Error(), "https://x.com/bcherny/status/123") || !strings.Contains(err.Error(), "cost_in_usd_ticks_api_reported=150000") || strings.Contains(err.Error(), "https://x.com/other/status/456") {
		t.Fatalf("unexpected discovery warning: %v", err)
	}
}

func TestGrokRejectsMissingKeyAndAPIFailure(t *testing.T) {
	c := NewConnector()
	if err := c.Validate(context.Background(), &types.DataSourceConfig{}); err == nil {
		t.Fatal("missing API key accepted")
	}
	c.client = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Body: io.NopCloser(strings.NewReader("unauthorized")), Header: make(http.Header)}, nil
	})}
	err := c.Validate(context.Background(), &types.DataSourceConfig{Credentials: map[string]interface{}{"api_key": "fake"}})
	if err == nil || !strings.Contains(err.Error(), "HTTP 401") || strings.Contains(err.Error(), "fake") {
		t.Fatalf("unexpected auth failure: %v", err)
	}
}
