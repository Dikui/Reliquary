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
	base, err := url.Parse(listingURL)
	if err != nil || pathPrefix == "" {
		return nil, fmt.Errorf("invalid article listing configuration")
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool)
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
		if !sitemapArticleURL(absolute, base, pathPrefix) || seen[absolute] {
			return true
		}
		seen[absolute] = true
		items = append(items, &gofeed.Item{GUID: absolute, Link: absolute,
			Title: strings.TrimSpace(a.Text()), Updated: now.Format(time.RFC3339Nano), UpdatedParsed: &now})
		// ponytail: inspect only the newest visible links; expand if a source paginates.
		return len(items) < 50
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("no matching article links in listing")
	}
	return items, nil
}
