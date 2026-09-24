package rss

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
)

const minimaxNewsAPIURL = "https://www.minimax.io/api/news?page=1&locale=en"

func isMiniMaxNewsURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "www.minimax.io") &&
		strings.TrimSuffix(u.Path, "/") == "/news" && u.RawQuery == ""
}

// The public news page loads its current links from this same-site JSON API.
func parseMiniMaxNews(data []byte) ([]*gofeed.Item, error) {
	var response struct {
		Data []struct {
			Slug  string `json:"slug"`
			Title string `json:"title"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode MiniMax news: %w", err)
	}
	if len(response.Data) == 0 {
		return nil, fmt.Errorf("MiniMax news response has no articles")
	}
	items := make([]*gofeed.Item, 0, len(response.Data))
	seen := make(map[string]bool)
	now := time.Now().UTC()
	for _, article := range response.Data {
		slug := strings.TrimSpace(article.Slug)
		if slug == "" || seen[slug] {
			continue
		}
		seen[slug] = true
		link := "https://www.minimax.io/news/" + url.PathEscape(slug)
		items = append(items, &gofeed.Item{GUID: link, Link: link, Title: article.Title,
			Updated: now.Format(time.RFC3339Nano), UpdatedParsed: &now})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("MiniMax news response has no usable article links")
	}
	// ponytail: read the newest page; add pagination if more than one page is published between daily syncs.
	return items, nil
}
