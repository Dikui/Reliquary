package rss

import "net/http"

func openAIAdapter() SiteAdapter {
	return SiteAdapter{Name: "openai", Rules: []SiteRule{{Domains: []string{"openai.com"}, Path: "/index/", Prefix: true}}, requestHeaders: http.Header{
		"Accept":          {"text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"},
		"Accept-Language": {"en-US,en;q=0.9"}, "Sec-Fetch-Dest": {"document"}, "Sec-Fetch-Mode": {"navigate"}, "Sec-Fetch-Site": {"none"}, "Sec-Fetch-User": {"?1"},
	}}
}
