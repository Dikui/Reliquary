package rss

import (
	"context"
	"github.com/PuerkitoBio/goquery"
	"net/url"
	"strings"
)

func mistralAdapter() SiteAdapter {
	return SiteAdapter{Name: "mistral", Rules: []SiteRule{{Domains: []string{"mistral.ai"}, Path: "/news", Prefix: true}},
		listing: &listingProfile{selector: "main article.js-post > a[href]", date: func(link *goquery.Selection) string {
			return strings.TrimSpace(link.Closest("article.js-post").Find("p.text-body-small.text-text-tertiary").First().Text())
		}},
		Recover: func(_ context.Context, page *SitePage) SiteRecovery {
			author, publisher := mistralPageAttribution(page.Document, page.URL)
			return SiteRecovery{Success: true, Author: author, Publisher: publisher}
		},
	}
}

func mistralPageAttribution(doc *goquery.Document, pageURL *url.URL) (author, publisher string) {
	doc.Find("h1").First().Parent().Find("p").EachWithBreak(func(_ int, paragraph *goquery.Selection) bool {
		text := strings.TrimSpace(paragraph.Text())
		if !strings.HasPrefix(text, "By ") {
			return true
		}
		byline := strings.TrimSpace(strings.TrimPrefix(text, "By "))
		if strings.HasPrefix(strings.ToLower(byline), "mistral") {
			publisher = byline
		} else {
			author = byline
		}
		return false
	})
	return author, publisher
}
