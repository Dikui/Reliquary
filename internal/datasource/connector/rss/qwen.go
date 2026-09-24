package rss

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

const qwenArticlesURL = "https://qwen.ai/api/v2/article/retrieval?type=qwen_ai&language=en-US"

func isQwenBlogURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Host, "qwen.ai") &&
		strings.TrimSuffix(u.Path, "/") == "/blog" && u.RawQuery == ""
}

// parseQwenArticles uses the complete article HTML returned by Qwen's own
// blog API; the public /blog document is a client-side shell.
func parseQwenArticles(data []byte) ([]*gofeed.Item, error) {
	var response struct {
		Success bool `json:"success"`
		Data    struct {
			Articles []struct {
				Path, Title, Content string
				Extra                struct {
					Date, Author string
				}
			} `json:"articles"`
		} `json:"data"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode Qwen articles: %w", err)
	}
	if !response.Success || len(response.Data.Articles) == 0 {
		return nil, fmt.Errorf("Qwen article response is empty or unsuccessful")
	}
	items := make([]*gofeed.Item, 0, len(response.Data.Articles))
	for _, article := range response.Data.Articles {
		published, valid := parseSiteDate(article.Extra.Date)
		if article.Path == "" || article.Title == "" || !valid {
			return nil, fmt.Errorf("Qwen article missing path, title or publication date")
		}
		link := "https://qwen.ai/blog?id=" + url.QueryEscape(article.Path)
		pageURL, _ := url.Parse(link)
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.Content))
		if err != nil {
			return nil, fmt.Errorf("parse Qwen article %s: %w", article.Path, err)
		}
		body := doc.Find(".post-content").First()
		if body.Length() == 0 {
			return nil, fmt.Errorf("Qwen article %s has no original body", article.Path)
		}
		preferLargestImageVariant(body, pageURL)
		absolutizeContentURLs(body, pageURL)
		html, err := goquery.OuterHtml(body)
		if err != nil {
			return nil, fmt.Errorf("render Qwen article %s: %w", article.Path, err)
		}
		items = append(items, &gofeed.Item{
			GUID: link, Link: link, Title: article.Title, Content: html,
			Published: article.Extra.Date, PublishedParsed: &published,
			Author: &gofeed.Person{Name: article.Extra.Author},
		})
	}
	return items, nil
}
