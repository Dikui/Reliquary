package grokx

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"reliquary/internal/datasource"
	"reliquary/internal/types"
)

// X Search exposes citations and usage, not a guaranteed original post body or
// original image bytes. This connector records discoveries but cannot ingest
// them as knowledge until the API supplies verifiable originals.
type Connector struct {
	endpoint string
	client   *http.Client
}

func NewConnector() *Connector {
	return &Connector{endpoint: "https://api.x.ai", client: &http.Client{Timeout: 2 * time.Minute}}
}

func (c *Connector) Type() string { return types.ConnectorTypeGrokX }

func apiKey(config *types.DataSourceConfig) (string, error) {
	if config == nil {
		return "", datasource.ErrInvalidConfig
	}
	key, _ := config.Credentials["api_key"].(string)
	if strings.TrimSpace(key) == "" {
		return "", fmt.Errorf("%w: api_key is required", datasource.ErrInvalidCredentials)
	}
	return strings.TrimSpace(key), nil
}

func handle(config *types.DataSourceConfig) (string, error) {
	if config == nil {
		return "", datasource.ErrInvalidConfig
	}
	h, _ := config.Settings["handle"].(string)
	h = strings.TrimPrefix(strings.TrimSpace(h), "@")
	switch strings.ToLower(h) {
	case "bcherny", "dotey", "tencentglobal":
		return h, nil
	default:
		return "", fmt.Errorf("%w: select bcherny, dotey or TencentGlobal", datasource.ErrInvalidConfig)
	}
}

func (c *Connector) Validate(ctx context.Context, config *types.DataSourceConfig) error {
	key, err := apiKey(config)
	if err != nil {
		return err
	}
	// The credentials-only connection test has no settings; source creation does.
	if config.Settings != nil {
		if _, err := handle(config); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("Grok connection failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Grok connection returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func (c *Connector) ListResources(_ context.Context, config *types.DataSourceConfig, parentID string) ([]types.Resource, error) {
	if parentID != "" {
		return []types.Resource{}, nil
	}
	h, err := handle(config)
	if err != nil {
		return nil, err
	}
	return []types.Resource{{ExternalID: h, Name: "@" + h, Type: "x_account", URL: "https://x.com/" + h}}, nil
}

func (c *Connector) ResolveResourceAncestors(context.Context, *types.DataSourceConfig, []string) ([]string, error) {
	return []string{}, nil
}

func (c *Connector) FetchAll(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string) ([]types.FetchedItem, error) {
	if config == nil {
		return nil, datasource.ErrInvalidConfig
	}
	_, err := c.search(ctx, config, resourceIDs, config.IngestSince)
	return nil, err
}

func (c *Connector) FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	if config == nil {
		return nil, nil, datasource.ErrInvalidConfig
	}
	since := config.IngestSince
	if cursor != nil && cursor.LastSyncTime.After(since) {
		since = cursor.LastSyncTime
	}
	end, err := c.search(ctx, config, config.ResourceIDs, since)
	if end.IsZero() {
		return nil, nil, err
	}
	return nil, &types.SyncCursor{LastSyncTime: end}, err
}

func (c *Connector) search(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string, since time.Time) (time.Time, error) {
	key, err := apiKey(config)
	if err != nil {
		return time.Time{}, err
	}
	h, err := handle(config)
	if err != nil {
		return time.Time{}, err
	}
	if len(resourceIDs) > 0 && (len(resourceIDs) != 1 || !strings.EqualFold(resourceIDs[0], h)) {
		return time.Time{}, datasource.ErrResourceNotFound
	}
	if since.IsZero() {
		return time.Time{}, fmt.Errorf("%w: source enablement time is missing", datasource.ErrInvalidConfig)
	}
	end := time.Now().UTC()
	if since.After(end) {
		return time.Time{}, fmt.Errorf("%w: source enablement time is in the future", datasource.ErrInvalidConfig)
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": "grok-4.7",
		"input": []map[string]string{{"role": "user", "content": "Find recent original posts by @" + h + " in the specified date range. Cite each original post URL; do not include other accounts."}},
		"tools": []map[string]interface{}{{
			"type": "x_search", "allowed_x_handles": []string{h},
			"from_date": since.UTC().Format("2006-01-02"), "to_date": end.Format("2006-01-02"),
		}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v1/responses", bytes.NewReader(body))
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return time.Time{}, fmt.Errorf("Grok X search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("Grok X search returned HTTP %d", resp.StatusCode)
	}
	var result struct {
		Status string `json:"status"`
		Output []struct {
			Type    string `json:"type"`
			Content []struct {
				Annotations []struct {
					URL string `json:"url"`
				} `json:"annotations"`
			} `json:"content"`
		} `json:"output"`
		Usage struct {
			InputTokens  *int64 `json:"input_tokens"`
			OutputTokens *int64 `json:"output_tokens"`
			CostTicks    *int64 `json:"cost_in_usd_ticks"`
			Tool         struct {
				SearchCalls  *int64 `json:"x_search_calls"`
				PostsFetched *int64 `json:"x_posts_fetched"`
			} `json:"server_side_tool_usage_details"`
		} `json:"usage"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return time.Time{}, fmt.Errorf("decode Grok X search response: %w", err)
	}
	if result.Status != "completed" {
		return time.Time{}, fmt.Errorf("Grok X search status %q", result.Status)
	}
	links := make(map[string]bool)
	for _, out := range result.Output {
		if out.Type != "message" {
			continue
		}
		for _, content := range out.Content {
			for _, annotation := range content.Annotations {
				if originalPostURL(annotation.URL, h) {
					links[annotation.URL] = true
				}
			}
		}
	}
	// The provider's generated text and citation links are never treated as
	// original post content. These short warnings are the only sync output.
	details := []string{fmt.Sprintf("@%s: %d cited posts need verifiable original text and image bytes; none entered the knowledge base", h, len(links))}
	for link := range links {
		if len(details) >= 20 {
			break
		}
		details = append(details, link)
	}
	metrics := fmt.Sprintf("Grok X search duration_ms=%d", time.Since(started).Milliseconds())
	for _, field := range []struct {
		name  string
		value *int64
	}{{"input_tokens", result.Usage.InputTokens}, {"output_tokens", result.Usage.OutputTokens}, {"x_search_calls", result.Usage.Tool.SearchCalls}, {"x_posts_fetched", result.Usage.Tool.PostsFetched}, {"cost_in_usd_ticks_api_reported", result.Usage.CostTicks}} {
		if field.value != nil {
			metrics += fmt.Sprintf(" %s=%d", field.name, *field.value)
		}
	}
	details = append(details, metrics)
	return end, &datasource.PartialFetchError{Details: details}
}

func originalPostURL(raw, handle string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "x.com") {
		return false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || !strings.EqualFold(parts[0], handle) || parts[1] != "status" || parts[2] == "" {
		return false
	}
	for _, digit := range parts[2] {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}
