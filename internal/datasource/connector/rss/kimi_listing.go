package rss

import (
	"github.com/PuerkitoBio/goquery"
	"strings"
)

func kimiListingAdapter() SiteAdapter {
	return SiteAdapter{Name: "kimi-listing", Rules: []SiteRule{{Domains: []string{"www.kimi.ai", "kimi.ai"}, Path: "/blog"}}, listing: &listingProfile{date: func(link *goquery.Selection) string {
		return strings.TrimSpace(link.Closest(".menu-card").Find(".card-date").First().Text())
	}}}
}
