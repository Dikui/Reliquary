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
	if adapter := defaultSiteAdapters().discoveryFor(listingURL); adapter != nil {
		return adapter.discovery.parse(data, listingURL)
	}
	return parseGenericListing(data, listingURL, pathPrefix)
}

// parseGenericListing is the fallback after a site discovery adapter fails.
// It deliberately avoids dispatching the same failed discovery adapter again.
func parseGenericListing(data []byte, listingURL, pathPrefix string) ([]*gofeed.Item, error) {
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
	profile := defaultSiteAdapters().listingFor(base)
	links := doc.Find("a[href]")
	if profile != nil && profile.selector != "" {
		links = doc.Find(profile.selector)
	}
	links.EachWithBreak(func(_ int, a *goquery.Selection) bool {
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
		itemID := strings.TrimSuffix(absolute, "/")
		if profile != nil && profile.skip != nil && profile.skip(resolved) {
			return true
		}
		date := ""
		if profile != nil && profile.date != nil {
			date = profile.date(a)
		}
		listingUpdatedAt := now
		hasListingTime := false
		if parsed, valid := parseSiteDate(date); valid {
			listingUpdatedAt = parsed
			if !strings.Contains(date, ":") {
				// A day-only card cannot rule out an article published later that day.
				listingUpdatedAt = parsed.Add(24*time.Hour - time.Nanosecond)
			}
			hasListingTime = true
		} else {
			date = ""
		}
		if raw, ok := a.Find("time[datetime]").First().Attr("datetime"); ok {
			if parsed, valid := parseSiteDate(raw); valid {
				listingUpdatedAt = parsed
				hasListingTime = true
			}
		}
		title := strings.TrimSpace(a.Find("h1,h2,h3").First().Text())
		if title == "" {
			title = strings.TrimSpace(a.Text())
		}
		if existing := seen[itemID]; existing != nil {
			if profile != nil && profile.improveTitle != nil && profile.improveTitle(existing.Title, title) {
				existing.Title = title
			}
			if existing.Published == "" {
				existing.Published = date
			}
			if hasListingTime {
				existing.Updated = listingUpdatedAt.Format(time.RFC3339Nano)
				existing.UpdatedParsed = &listingUpdatedAt
			}
			return true
		}
		item := &gofeed.Item{GUID: itemID, Link: absolute, Title: title,
			Published: date, Updated: listingUpdatedAt.Format(time.RFC3339Nano), UpdatedParsed: &listingUpdatedAt,
			Custom: map[string]string{"from_listing": "true"}}
		seen[itemID] = item
		items = append(items, item)
		// ponytail: inspect only the newest visible links; expand if a source paginates.
		return len(items) < 50
	})
	if len(items) == 0 {
		return nil, fmt.Errorf("no matching article links in listing")
	}
	return items, nil
}
