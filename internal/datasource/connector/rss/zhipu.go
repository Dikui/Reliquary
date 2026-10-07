package rss

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

func isZhipuResearchURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "www.zhipuai.cn") &&
		strings.TrimSuffix(u.Path, "/") == "/zh/research" && u.RawQuery == ""
}

// The research page renders only a few links, but its Next.js page data
// contains the current article IDs. The article page supplies publication evidence.
func parseZhipuResearch(data []byte) ([]*gofeed.Item, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var articles []struct {
		ID    int    `json:"id"`
		Title string `json:"title_zh"`
	}
	doc.Find("script").EachWithBreak(func(_ int, script *goquery.Selection) bool {
		const marker = "self.__next_f.push("
		content := script.Text()
		start := strings.Index(content, marker)
		if start < 0 || !strings.Contains(content, `\"blogsItems\"`) {
			return true
		}
		end := strings.LastIndex(content, ")")
		if end <= start+len(marker) {
			return true
		}
		var push []json.RawMessage
		if json.Unmarshal([]byte(content[start+len(marker):end]), &push) != nil || len(push) < 2 {
			return true
		}
		var flight string
		if json.Unmarshal(push[1], &flight) != nil {
			return true
		}
		const key = `"blogsItems":`
		at := strings.Index(flight, key)
		if at < 0 {
			return true
		}
		return json.NewDecoder(strings.NewReader(flight[at+len(key):])).Decode(&articles) != nil
	})
	if len(articles) == 0 {
		return nil, fmt.Errorf("Zhipu research page has no article data")
	}
	items := make([]*gofeed.Item, 0, len(articles))
	seen := make(map[int]bool)
	now := time.Now().UTC()
	for _, article := range articles {
		if article.ID <= 0 || seen[article.ID] {
			continue
		}
		seen[article.ID] = true
		link := "https://www.zhipuai.cn/zh/research/" + strconv.Itoa(article.ID)
		items = append(items, &gofeed.Item{GUID: link, Link: link, Title: article.Title,
			Updated: now.Format(time.RFC3339Nano), UpdatedParsed: &now})
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("Zhipu research page has no usable article IDs")
	}
	return items, nil
}

func zhipuAdapter() SiteAdapter {
	return SiteAdapter{Name: "zhipu", Rules: []SiteRule{{Domains: []string{"www.zhipuai.cn"}, Path: "/zh/research"}}, discovery: &siteDiscovery{
		sourceURL: "", inlineOriginal: false, needsArticleDate: true,
		parse: func(data []byte, raw string) ([]*gofeed.Item, error) { return parseZhipuResearch(data) },
	}}
}
