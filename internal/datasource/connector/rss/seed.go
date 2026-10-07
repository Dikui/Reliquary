package rss

import (
	"context"
	"github.com/PuerkitoBio/goquery"
	"strings"
)

func seedAdapter() SiteAdapter {
	return SiteAdapter{Name: "seed", Rules: []SiteRule{{Domains: []string{"seed.bytedance.com"}, Path: "/zh/blog/", Prefix: true}}, publishedAt: seedPublishedAt, Recover: func(_ context.Context, page *SitePage) SiteRecovery {
		date := seedPublishedAt(page.Document)
		if date == "" && page.Document.Find("article, main").Length() == 0 {
			return SiteRecovery{Reason: "Seed source is a client-side shell without the original article or date label"}
		}
		return SiteRecovery{Success: true, PublishedAt: date}
	}}
}
func seedPublishedAt(doc *goquery.Document) string {
	var date string
	doc.Find("p").EachWithBreak(func(_ int, label *goquery.Selection) bool {
		if strings.TrimSpace(label.Text()) != "日期" {
			return true
		}
		candidate := strings.TrimSpace(label.Next().Text())
		if _, valid := parseSiteDate(candidate); valid {
			date = candidate
			return false
		}
		return true
	})
	return date
}
