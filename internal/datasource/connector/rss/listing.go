package rss

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

// parseListing discovers same-site article links from a public listing page.
// Publication evidence still comes from each article, never the listing run.
func parseListing(data []byte, listingURL, pathPrefix string) ([]*gofeed.Item, error) {
	if isZhipuResearchURL(listingURL) {
		return parseZhipuResearch(data)
	}
	base, err := url.Parse(listingURL)
	if err != nil || pathPrefix == "" {
		return nil, fmt.Errorf("invalid article listing configuration")
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]*gofeed.Item)
	items := make([]*gofeed.Item, 0)
	now := time.Now().UTC()
	doc.Find("a[href]").EachWithBreak(func(_ int, a *goquery.Selection) bool {
		link, _ := a.Attr("href")
		ref, err := url.Parse(strings.TrimSpace(link))
		if err != nil || ref.User != nil {
			return true
		}
		resolved := base.ResolveReference(ref)
		resolved.RawQuery, resolved.Fragment = "", ""
		absolute := resolved.String()
		if !sitemapArticleURL(absolute, base, pathPrefix) {
			return true
		}
		date := strings.TrimSpace(a.Closest(".menu-card").Find(".card-date").First().Text())
		if _, valid := parseSiteDate(date); !valid {
			date = ""
		}
		if existing := seen[absolute]; existing != nil {
			if existing.Published == "" {
				existing.Published = date
			}
			return true
		}
		title := strings.TrimSpace(a.Find("h1,h2,h3").First().Text())
		if title == "" {
			title = strings.TrimSpace(a.Text())
		}
		item := &gofeed.Item{GUID: absolute, Link: absolute, Title: title,
			Published: date, Updated: now.Format(time.RFC3339Nano), UpdatedParsed: &now,
			Custom: map[string]string{"listing_title_needs_article": "true"}}
		seen[absolute] = item
		items = append(items, item)
		// ponytail: inspect only the newest visible links; expand if a source paginates.
		return len(items) < 50
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("no matching article links in listing")
	}
	return items, nil
}
