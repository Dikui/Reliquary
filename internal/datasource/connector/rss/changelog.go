package rss

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

var changelogDate = regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}`)

func isDeepSeekUpdatesURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && strings.EqualFold(u.Hostname(), "api-docs.deepseek.com") &&
		strings.TrimSuffix(u.Path, "/") == "/zh-cn/updates"
}

// parseDeepSeekUpdates treats each dated heading and its following content as
// one original message. The official changelog has anchors, not article pages.
func parseDeepSeekUpdates(data []byte, pageURL string) ([]*gofeed.Item, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	page, _ := url.Parse(pageURL)
	body := doc.Find(".theme-doc-markdown .col--12").First()
	if body.Length() == 0 {
		return nil, fmt.Errorf("changelog body missing")
	}
	var date string
	var items []*gofeed.Item
	body.Children().Each(func(_ int, node *goquery.Selection) {
		if node.Is("h2") {
			date = changelogDate.FindString(node.Text())
			if _, valid := parseSiteDate(date); !valid {
				date = ""
			}
			return
		}
		if !node.Is("h3") || date == "" {
			return
		}
		id, _ := node.Attr("id")
		if id == "" {
			return
		}
		section := node.NextUntil("h2,h3")
		preferLargestImageVariant(section, page)
		section.Filter("a[href],img[src]").AddSelection(section.Find("a[href],img[src]")).Each(func(_ int, element *goquery.Selection) {
			attr := "href"
			if element.Is("img") {
				attr = "src"
			}
			raw, _ := element.Attr(attr)
			ref, err := url.Parse(strings.TrimSpace(raw))
			if err == nil && ref.Scheme == "" {
				element.SetAttr(attr, page.ResolveReference(ref).String())
			}
		})
		var html strings.Builder
		section.Each(func(_ int, part *goquery.Selection) {
			if part.Is("hr") {
				return
			}
			fragment, err := goquery.OuterHtml(part)
			if err == nil {
				html.WriteString(fragment)
			}
		})
		link := *page
		link.Fragment = id
		published, _ := parseSiteDate(date)
		items = append(items, &gofeed.Item{
			GUID: link.String(), Link: link.String(),
			Title:   strings.TrimSpace(strings.ReplaceAll(node.Text(), "\u200b", "")),
			Content: html.String(), Published: date, PublishedParsed: &published,
		})
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("no dated changelog entries")
	}
	return items, nil
}
