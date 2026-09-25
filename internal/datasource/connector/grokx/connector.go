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
	"reliquary/internal/logger"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// Connector collects posts through Grok X Search and checks the returned
// structured post against the cited status URL before intake.
type Connector struct {
	endpoint string
	client   *http.Client
}

func NewConnector() *Connector {
	cfg := utils.DefaultSSRFSafeHTTPClientConfig()
	cfg.Timeout = 2 * time.Minute
	return &Connector{endpoint: "https://api.x.ai", client: utils.NewSSRFSafeHTTPClient(cfg)}
}

func (c *Connector) Type() string { return types.ConnectorTypeGrokX }

func (c *Connector) baseURL(config *types.DataSourceConfig) (string, error) {
	base := strings.TrimRight(c.endpoint, "/") + "/v1"
	configured, _ := config.Settings["base_url"].(string)
	if configured == "" {
		configured, _ = config.Credentials["base_url"].(string) // stateless connection test
	}
	if strings.TrimSpace(configured) == "" {
		return base, nil
	}
	base = strings.TrimRight(strings.TrimSpace(configured), "/")
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("%w: base_url must be an HTTPS API URL", datasource.ErrInvalidConfig)
	}
	if err := utils.ValidateURLForSSRF(base); err != nil {
		return "", fmt.Errorf("%w: base_url: %v", datasource.ErrInvalidConfig, err)
	}
	return base, nil
}

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
	base, err := c.baseURL(config)
	if err != nil {
		return err
	}
	// The credentials-only connection test has no settings; source creation does.
	if config.Settings != nil {
		if _, err := handle(config); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/models", nil)
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
	items, _, err := c.search(ctx, config, resourceIDs, config.IngestSince)
	return items, err
}

func (c *Connector) FetchIncremental(ctx context.Context, config *types.DataSourceConfig, cursor *types.SyncCursor) ([]types.FetchedItem, *types.SyncCursor, error) {
	if config == nil {
		return nil, nil, datasource.ErrInvalidConfig
	}
	since := config.IngestSince
	if cursor != nil && cursor.LastSyncTime.After(since) {
		since = cursor.LastSyncTime
	}
	items, end, err := c.search(ctx, config, config.ResourceIDs, since)
	if end.IsZero() {
		return items, nil, err
	}
	return items, &types.SyncCursor{LastSyncTime: end}, err
}

type xPost struct {
	TweetID      string `json:"tweet_id"`
	URL          string `json:"url"`
	AuthorHandle string `json:"author_handle"`
	Text         string `json:"text"`
	CreatedAt    string `json:"created_at"`
	PostType     string `json:"post_type"`
	Media        []struct {
		Type string `json:"type"`
		URL  string `json:"url"`
	} `json:"media"`
}

type xResponse struct {
	Status     string `json:"status"`
	OutputText string `json:"output_text"`
	Output     []struct {
		Type    string `json:"type"`
		Content []struct {
			Text        string `json:"text"`
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
			UsersFetched *int64 `json:"x_users_fetched"`
		} `json:"server_side_tool_usage_details"`
	} `json:"usage"`
}

func (c *Connector) search(ctx context.Context, config *types.DataSourceConfig, resourceIDs []string, since time.Time) ([]types.FetchedItem, time.Time, error) {
	key, err := apiKey(config)
	if err != nil {
		return nil, time.Time{}, err
	}
	h, err := handle(config)
	if err != nil {
		return nil, time.Time{}, err
	}
	if len(resourceIDs) > 0 && (len(resourceIDs) != 1 || !strings.EqualFold(resourceIDs[0], h)) {
		return nil, time.Time{}, datasource.ErrResourceNotFound
	}
	if since.IsZero() {
		return nil, time.Time{}, fmt.Errorf("%w: source enablement time is missing", datasource.ErrInvalidConfig)
	}
	end := time.Now().UTC()
	if since.After(end) {
		return nil, time.Time{}, fmt.Errorf("%w: source enablement time is in the future", datasource.ErrInvalidConfig)
	}
	base, err := c.baseURL(config)
	if err != nil {
		return nil, time.Time{}, err
	}
	body, _ := json.Marshal(map[string]interface{}{
		"model": "grok-4.3",
		"input": []map[string]string{{"role": "user", "content": fmt.Sprintf(
			"Use x_search to retrieve posts authored by @%s between %s and %s UTC. Return exact original text without translation, summary, or inference. Include replies and quotes, but not reposts of other authors. Never invent a status URL, author, timestamp, or media URL. Return only JSON: {\"handle\":%q,\"posts\":[{\"tweet_id\":\"123\",\"url\":\"https://x.com/%s/status/123\",\"author_handle\":%q,\"text\":\"exact post text\",\"created_at\":\"RFC3339 timestamp\",\"post_type\":\"original|reply|quote\",\"media\":[{\"type\":\"image|video|link\",\"url\":\"https://...\"}]}],\"warnings\":[]}. Return an empty posts array when none are found.",
			h, since.UTC().Format(time.RFC3339), end.Format(time.RFC3339), h, h, h)}},
		"tools": []map[string]interface{}{{
			"type": "x_search", "allowed_x_handles": []string{h},
			"from_date": since.UTC().Format("2006-01-02"), "to_date": end.AddDate(0, 0, 1).Format("2006-01-02"),
		}},
		"text": map[string]interface{}{"format": map[string]string{"type": "json_object"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, time.Time{}, err
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	started := time.Now()
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("Grok X search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, time.Time{}, fmt.Errorf("Grok X search returned HTTP %d", resp.StatusCode)
	}
	var result xResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&result); err != nil {
		return nil, time.Time{}, fmt.Errorf("decode Grok X search response: %w", err)
	}
	if result.Status != "completed" {
		return nil, time.Time{}, fmt.Errorf("Grok X search status %q", result.Status)
	}
	items, details, err := parsePosts(result, h, since, end)
	if err != nil {
		return nil, time.Time{}, err
	}
	metrics := fmt.Sprintf("Grok X search @%s model=grok-4.3 iterations=1 posts=%d warnings=%d duration_ms=%d", h, len(items), len(details), time.Since(started).Milliseconds())
	for _, field := range []struct {
		name  string
		value *int64
	}{{"input_tokens", result.Usage.InputTokens}, {"output_tokens", result.Usage.OutputTokens}, {"x_search_calls", result.Usage.Tool.SearchCalls}, {"x_posts_fetched", result.Usage.Tool.PostsFetched}, {"x_users_fetched", result.Usage.Tool.UsersFetched}, {"cost_in_usd_ticks_api_reported", result.Usage.CostTicks}} {
		if field.value != nil {
			metrics += fmt.Sprintf(" %s=%d", field.name, *field.value)
		}
	}
	logger.Infof(ctx, "%s", metrics)
	if len(details) > 0 {
		return items, end, &datasource.PartialFetchError{Details: details}
	}
	return items, end, nil
}

func parsePosts(result xResponse, handle string, since, end time.Time) ([]types.FetchedItem, []string, error) {
	var outputText strings.Builder
	cited := make(map[string]bool)
	for _, output := range result.Output {
		if output.Type != "message" {
			continue
		}
		for _, part := range output.Content {
			if part.Text != "" {
				outputText.WriteString(part.Text)
			}
			for _, annotation := range part.Annotations {
				if id := statusID(annotation.URL, handle); id != "" {
					cited[id] = true
				}
			}
		}
	}
	text := strings.TrimSpace(outputText.String())
	if text == "" {
		text = strings.TrimSpace(result.OutputText)
	}
	text = strings.TrimPrefix(strings.TrimPrefix(text, "```json"), "```")
	text = strings.TrimSpace(strings.TrimSuffix(text, "```"))
	var envelope struct {
		Handle   string   `json:"handle"`
		Posts    []xPost  `json:"posts"`
		Warnings []string `json:"warnings"`
	}
	if text == "" || json.Unmarshal([]byte(text), &envelope) != nil || !strings.EqualFold(strings.TrimPrefix(envelope.Handle, "@"), handle) {
		return nil, nil, fmt.Errorf("Grok X search returned invalid structured posts")
	}
	items := make([]types.FetchedItem, 0, len(envelope.Posts))
	details := make([]string, 0)
	seen := make(map[string]bool)
	for index, post := range envelope.Posts {
		id := statusID(post.URL, handle)
		reason := ""
		switch {
		case id == "" || !cited[id]:
			reason = "direct status URL is not cited by X Search"
		case post.TweetID != "" && post.TweetID != id:
			reason = "tweet ID differs from status URL"
		case !strings.EqualFold(strings.TrimPrefix(post.AuthorHandle, "@"), handle):
			reason = "author differs from source account"
		case strings.TrimSpace(post.Text) == "":
			reason = "original text is missing"
		case post.PostType != "original" && post.PostType != "reply" && post.PostType != "quote":
			reason = "unsupported post type"
		case post.Media == nil:
			reason = "media inventory is missing"
		}
		published, dateErr := time.Parse(time.RFC3339, post.CreatedAt)
		if reason == "" && (dateErr != nil || published.IsZero()) {
			reason = "publication timestamp is missing or invalid"
		}
		if reason != "" {
			if len(details) < 20 {
				details = append(details, fmt.Sprintf("post %d: %s", index+1, reason))
			}
			continue
		}
		if published.Before(since) || published.After(end) || seen[id] {
			continue
		}
		content := strings.TrimSpace(post.Text)
		for _, media := range post.Media {
			u, err := url.Parse(media.URL)
			if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || strings.ContainsAny(media.URL, "()\r\n") {
				reason = "media URL is invalid"
				break
			}
			switch media.Type {
			case "image":
				if !strings.EqualFold(u.Hostname(), "pbs.twimg.com") || u.Port() != "" {
					reason = "image is not on the original X media host"
					break
				}
				content += "\n\n![X image](" + media.URL + ")"
			case "video":
				if !strings.EqualFold(u.Hostname(), "video.twimg.com") || u.Port() != "" {
					reason = "video is not on the original X media host"
					break
				}
				content += "\n\n[X video](" + media.URL + ")"
			case "link":
				content += "\n\n[Linked URL](" + media.URL + ")"
			default:
				reason = "unsupported media type"
			}
			if reason != "" {
				break
			}
		}
		if reason != "" {
			if len(details) < 20 {
				details = append(details, fmt.Sprintf("post %d: %s", index+1, reason))
			}
			continue
		}
		seen[id] = true
		title := []rune(strings.Join(strings.Fields(post.Text), " "))
		items = append(items, types.FetchedItem{
			ExternalID: id, Title: "@" + handle + ": " + string(title[:min(len(title), 80)]),
			Content: []byte(content), ContentType: "text/markdown", FileName: id + ".md",
			URL: "https://x.com/" + handle + "/status/" + id, UpdatedAt: published,
			SourceResourceID: handle, Metadata: map[string]string{
				"channel": types.ConnectorTypeGrokX, "author": "@" + handle,
				"published_at": published.Format(time.RFC3339Nano), "x_post_type": post.PostType,
			},
		})
	}
	if len(envelope.Warnings) > 0 {
		details = append(details, fmt.Sprintf("Grok reported %d search warnings", len(envelope.Warnings)))
	}
	return items, details, nil
}

func statusID(raw, handle string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || !strings.EqualFold(u.Hostname(), "x.com") || u.User != nil || u.Port() != "" {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) != 3 || parts[1] != "status" || parts[2] == "" ||
		(handle != "" && !strings.EqualFold(parts[0], handle)) ||
		(handle == "" && parts[0] == "") {
		return ""
	}
	for _, digit := range parts[2] {
		if digit < '0' || digit > '9' {
			return ""
		}
	}
	return parts[2]
}
