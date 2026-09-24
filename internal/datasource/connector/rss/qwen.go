package rss

import (
	"crypto/sha256"
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
		item := &gofeed.Item{Title: article.Title, Published: article.Extra.Date,
			Author: &gofeed.Person{Name: article.Extra.Author}}
		if valid {
			item.PublishedParsed = &published
		}
		if article.Path == "" {
			item.GUID = fmt.Sprintf("qwen:entry:%x", sha256.Sum256([]byte(article.Title+"\n"+article.Extra.Date+"\n"+article.Content)))
		} else {
			item.Link = "https://qwen.ai/blog?id=" + url.QueryEscape(article.Path)
			item.GUID = item.Link
		}
		var reason string
		switch {
		case article.Path == "":
			reason = "Qwen article missing original URL"
		case article.Title == "":
			reason = "Qwen article missing title"
		case !valid:
			reason = "Qwen article missing publication date"
		case article.Content == "":
			reason = "Qwen article has no original body"
		}
		if article.Content != "" {
			pageURL, _ := url.Parse(firstNonEmpty(item.Link, "https://qwen.ai/blog"))
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(article.Content))
			if err != nil {
				reason = fmt.Sprintf("parse Qwen article %s: %v", article.Path, err)
			} else {
				body := doc.Find(".post-content").First()
				if body.Length() == 0 {
					reason = "Qwen article has no original body"
				} else if contentErr := articleContentError(body); contentErr != nil {
					reason = fmt.Sprintf("Qwen article content failed: %v", contentErr)
				} else {
					preferLargestImageVariant(body, pageURL)
					absolutizeContentURLs(body, pageURL)
					item.Content, err = goquery.OuterHtml(body)
					if err != nil {
						reason = fmt.Sprintf("render Qwen article %s: %v", article.Path, err)
					}
				}
			}
		}
		if reason != "" {
			item.Custom = map[string]string{"error": reason}
		}
		items = append(items, item)
	}
	return items, nil
}
