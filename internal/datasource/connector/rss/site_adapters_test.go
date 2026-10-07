package rss

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"reliquary/internal/types"
	"reliquary/internal/utils"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
)

type adapterOfflineTransport struct {
	respond func(*http.Request) (string, string)
}

func (s adapterOfflineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, contentType := s.respond(req)
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
}

func adapterFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func offlineAdapterClient(t *testing.T) *client {
	t.Helper()
	return &client{httpClient: &http.Client{Transport: adapterOfflineTransport{respond: func(req *http.Request) (string, string) {
		if req.URL.Hostname() != "127.0.0.1" || req.URL.Path != "/clip.mp4" {
			t.Fatalf("unexpected offline media request: %s", req.URL)
		}
		return "\x00", "video/mp4"
	}}}}
}

func TestSiteAdaptersSavedHTMLRegression(t *testing.T) {
	cases := []struct{ name, raw, path, token string }{
		{"kimi-k25", "https://www.kimi.ai/blog/kimi-k2-5", "kimi-k25.html", "Find path"},
		{"kimi-k26", "https://kimi.ai/blog/kimi-k2-6/", "kimi-k26.html", "Metric 4"},
		{"kimi-tables", "https://www.kimi.ai/blog/perception-bench", "kimi-tables.html", "Vision"},
		{"qwen", "https://qwen.ai/blog?id=release", "qwen.html", "Qwen"},
		{"meta", "https://ai.meta.com/blog/release/", "meta.html", "https://images.example/hero.png"},
		{"mistral", "https://mistral.ai/news/release", "mistral.html", "measured results"},
		{"seed", "https://seed.bytedance.com/zh/blog/release", "seed.html", "measured results"},
		{"openai", "https://openai.com/index/release/", "openai.html", "measured results"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pageURL, _ := url.Parse(tc.raw)
			matched := false
			for _, adapter := range defaultSiteAdapters().matching(pageURL) {
				if adapter.Name == tc.name {
					matched = true
				}
			}
			if !matched {
				t.Fatalf("saved page did not match %s", tc.name)
			}
			article, err := offlineAdapterClient(t).extractArticleBody(context.Background(), adapterFixture(t, tc.path), tc.raw)
			if err != nil || len(article.failures) != 0 || !strings.Contains(article.contentHTML, tc.token) {
				t.Fatalf("recovery failed: err=%v failures=%+v body=%s", err, article.failures, article.contentHTML)
			}
			if tc.name == "mistral" && (article.byline != "" || article.siteName != "Mistral and Mozilla") {
				t.Fatalf("publisher attribution lost: %+v", article)
			}
			if tc.name == "seed" && article.publishedAt != "2026-09-24" {
				t.Fatalf("page publication date lost: %+v", article)
			}
			if strings.HasPrefix(tc.name, "kimi-") && strings.Contains(article.contentHTML, "animate-pulse") {
				t.Fatal("unresolved widgets remained after successful recovery")
			}
		})
	}
}

func TestSiteDiscoveryAdaptersSavedSamples(t *testing.T) {
	cases := []struct{ name, raw, html, api, expected string }{
		{"qwen", "https://qwen.ai/blog", "qwen.html", "qwen-api.json", "Qwen release"},
		{"minimax", "https://www.minimax.io/news", "minimax.html", "minimax-api.json", "MiniMax release"},
		{"deepseek", "https://api-docs.deepseek.com/zh-cn/updates", "deepseek.html", "", "DeepSeek release"},
		{"zhipu", "https://www.zhipuai.cn/zh/research", "zhipu.html", "", "Research release"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			html := adapterFixture(t, tc.html)
			if _, err := goquery.NewDocumentFromReader(strings.NewReader(string(html))); err != nil {
				t.Fatal(err)
			}
			adapter := defaultSiteAdapters().discoveryFor(tc.raw)
			if adapter == nil || adapter.Name != tc.name {
				t.Fatalf("source adapter missing: %+v", adapter)
			}
			data := html
			if tc.api != "" {
				data = adapterFixture(t, tc.api)
			}
			items, err := adapter.discovery.parse(data, tc.raw)
			if err != nil || len(items) != 1 || items[0].Title != tc.expected || items[0].Link == "" {
				t.Fatalf("discovery lost original article: items=%+v err=%v", items, err)
			}
		})
	}
}

func TestSiteListingAdaptersSavedHTML(t *testing.T) {
	cases := []struct{ name, raw, fixture, prefix, title, date string }{
		{"kimi-listing", "https://www.kimi.ai/blog/", "kimi-listing.html", "/blog/", "Navigation", "2026-09-24"},
		{"mistral", "https://mistral.ai/news/", "mistral.html", "/news/", "Release", "September 24, 2026"},
		{"meta", "https://ai.meta.com/blog/", "meta.html", "/blog/", "Meta release", "September 24, 2026"},
		{"cursor", "https://cursor.com/blog/", "cursor.html", "/blog/", "Cursor release", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			items, err := parseListing(adapterFixture(t, tc.fixture), tc.raw, tc.prefix)
			if err != nil || len(items) != 1 || items[0].Title != tc.title || items[0].Published != tc.date {
				t.Fatalf("listing recovery failed: err=%v items=%+v", err, items)
			}
			if tc.name == "cursor" && strings.Contains(items[0].Link, "/topic/") {
				t.Fatal("topic entered article discovery")
			}
		})
	}
}

func TestSiteAdapterFailureFallsBackWithoutPartialMutation(t *testing.T) {
	for _, raw := range []string{"https://kimi.ai/blog/kimi-k2-5", "https://kimi.ai/blog/kimi-k2-6", "https://kimi.ai/blog/perception-bench", "https://ai.meta.com/blog/release", "https://qwen.ai/blog?id=release"} {
		t.Run(raw, func(t *testing.T) {
			article, err := offlineAdapterClient(t).extractArticleBody(context.Background(), adapterFixture(t, "adapter-failure.html"), raw)
			if err != nil || len(article.failures) != 1 || !strings.Contains(article.contentHTML, "measured results") || strings.Contains(article.contentHTML, "animate-pulse") {
				t.Fatalf("generic fallback lost readable source: err=%v failures=%+v content=%s", err, article.failures, article.contentHTML)
			}
			failure := article.failures[0]
			u, _ := url.Parse(raw)
			if failure.URL != raw || failure.Site != u.Hostname() || failure.Adapter == "" || failure.Reason == "" {
				t.Fatalf("failure provenance incomplete: %+v", failure)
			}
		})
	}
	u, _ := url.Parse("https://example.com/blog/release")
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader("<article><p>Original fallback body</p></article>"))
	adapter := SiteAdapter{Name: "broken", Rules: []SiteRule{{Domains: []string{"example.com"}, Path: "/blog/", Prefix: true}}, Recover: func(_ context.Context, p *SitePage) SiteRecovery {
		p.Content.Find("p").SetText("Corrupted partial body")
		return SiteRecovery{Reason: "invalid widget"}
	}}
	page := &SitePage{Document: doc, Content: doc.Find("article"), URL: u}
	_, failures := NewSiteAdapterRegistry(adapter).recover(context.Background(), page)
	if len(failures) != 1 || page.Content.Text() != "Original fallback body" {
		t.Fatalf("failed adapter changed fallback: failures=%+v text=%q", failures, page.Content.Text())
	}
}

func TestSiteDiscoveryFailureFallsBackToGenericListing(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "127.0.0.1,::1,qwen.ai,www.minimax.io")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	for _, raw := range []string{"https://qwen.ai/blog", "https://www.minimax.io/news"} {
		t.Run(raw, func(t *testing.T) {
			pageURL, _ := url.Parse(raw)
			requests := 0
			cli := &client{httpClient: &http.Client{Transport: adapterOfflineTransport{respond: func(req *http.Request) (string, string) {
				requests++
				if strings.HasPrefix(req.URL.Path, "/api/") {
					return `{ "success":false, "data":[] }`, "application/json"
				}
				if req.URL.String() != raw {
					t.Fatalf("unexpected offline source URL: %s", req.URL)
				}
				return `<html><body><a href="` + pageURL.Path + `/release"><h3>Original release</h3></a></body></html>`, "text/html"
			}}}}
			source, err := NewConnector().readSource(context.Background(), cli, &Config{ArticlePathPrefix: pageURL.Path + "/"}, raw)
			if err != nil || source == nil {
				t.Fatalf("source fallback failed: source=%+v err=%v", source, err)
			}
			if len(source.failures) != 1 || source.inlineOriginal || len(source.feed.Items) != 1 || source.feed.Items[0].Link != raw+"/release" || requests != 2 {
				t.Fatalf("source fallback failed: source=%+v requests=%d", source, requests)
			}
		})
	}
}

func TestSiteRuleDomainPathAndRequestScope(t *testing.T) {
	registry := defaultSiteAdapters()
	for _, adapter := range registry.adapters {
		for _, rule := range adapter.Rules {
			page, _ := url.Parse("https://" + strings.ToUpper(rule.Domains[0]) + rule.Path)
			if !rule.Matches(page) {
				t.Fatalf("domain matching should ignore case: %s", adapter.Name)
			}
			page.Host = "unrelated.example"
			if adapter.Matches(page) {
				t.Fatalf("adapter leaked into unrelated domain: %s", adapter.Name)
			}
		}
	}
	req, _ := http.NewRequest(http.MethodGet, "https://openai.com/index/release", nil)
	registry.configureRequest(req)
	if req.Header.Get("Sec-Fetch-Dest") != "document" || req.Header.Get("Accept-Language") == "" {
		t.Fatalf("document request options lost: %+v", req.Header)
	}
	req, _ = http.NewRequest(http.MethodGet, "https://openai.com/news/rss.xml", nil)
	registry.configureRequest(req)
	if req.Header.Get("Sec-Fetch-Dest") != "" {
		t.Fatal("article adapter changed feed request")
	}
}

func TestRSSUndatedOriginalRemainsIngestible(t *testing.T) {
	item := NewConnector().resolveItem(context.Background(), offlineAdapterClient(t), &gofeed.Feed{}, &gofeed.Item{Link: "https://example.com/blog/undated", Content: "<article><p>Original without publication evidence.</p></article>"}, "https://example.com/feed", "undated", "Example", true).item
	if item.Metadata["error"] != "" || item.Metadata["published_at"] != "" || item.Metadata["published_at_missing"] != "true" || len(item.Content) == 0 || item.URL == "" {
		t.Fatalf("undated original was lost or assigned a collection date: %+v", item)
	}
}

func TestRSSUndatedFeedAndSitemapEntriesSurviveTimeFilter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch req.URL.Path {
		case "/feed.xml":
			_, _ = io.WriteString(w, `<rss version="2.0"><channel><title>Undated source</title><item><guid>original</guid><title>Original</title><link>http://`+req.Host+`/articles/original</link></item></channel></rss>`)
		case "/sitemap.xml":
			_, _ = io.WriteString(w, `<urlset><url><loc>http://`+req.Host+`/articles/original</loc></url></urlset>`)
		case "/articles/original":
			_, _ = io.WriteString(w, `<html><body><article><h1>Undated original</h1>`+longArticleBody+`</article></body></html>`)
		default:
			t.Fatalf("unexpected offline request: %s", req.URL)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/feed.xml", "/sitemap.xml"} {
		t.Run(path, func(t *testing.T) {
			config := &types.DataSourceConfig{Settings: map[string]interface{}{"feed_urls": server.URL + path, "article_path_prefix": "/articles/"}, IngestSince: time.Now().UTC()}
			items, _, err := NewConnector().FetchIncremental(context.Background(), config, nil)
			if err != nil || len(items) != 1 {
				t.Fatalf("undated original disappeared: items=%+v err=%v", items, err)
			}
			if items[0].Metadata["error"] != "" || items[0].Metadata["published_at"] != "" || items[0].Metadata["published_at_missing"] != "true" || len(items[0].Content) == 0 {
				t.Fatalf("missing time was discarded or inferred: %+v", items[0])
			}
		})
	}
}

func TestSiteAdapterRecoversWidgetOnlyPageBeforeGenericFailure(t *testing.T) {
	for _, name := range []string{"kimi-k25", "kimi-tables"} {
		t.Run(name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(adapterFixture(t, name+".html"))))
			if err != nil {
				t.Fatal(err)
			}
			doc.Find("p,h1").Remove()
			raw, _ := doc.Html()
			path := map[string]string{"kimi-k25": "/blog/kimi-k2-5", "kimi-tables": "/blog/perception-bench"}[name]
			article, err := offlineAdapterClient(t).extractArticleBody(context.Background(), []byte(raw), "https://kimi.ai"+path)
			if err != nil || len(article.failures) != 0 || !strings.Contains(article.contentHTML, "Vision") {
				t.Fatalf("widget-only original must recover before generic parse rejection: err=%v failures=%+v body=%s", err, article.failures, article.contentHTML)
			}
		})
	}
	doc, _ := goquery.NewDocumentFromReader(strings.NewReader(string(adapterFixture(t, "adapter-failure.html"))))
	doc.Find("p,h1").Remove()
	raw, _ := doc.Html()
	article, _ := offlineAdapterClient(t).extractArticleBody(context.Background(), []byte(raw), "https://kimi.ai/blog/kimi-k2-5")
	if len(article.failures) != 1 || article.failures[0].Reason == "" || article.failures[0].URL != "https://kimi.ai/blog/kimi-k2-5" {
		t.Fatalf("failed widget-only page must preserve diagnostic: %+v", article)
	}
}

func TestSiteAdapterFailuresSurvivePublicationTimeFiltering(t *testing.T) {
	t.Setenv("SSRF_WHITELIST", "kimi.ai,qwen.ai")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	cases := []struct{ name, feed string }{
		{"page-recovery", "https://kimi.ai/sitemap.xml"},
		{"source-discovery", "https://qwen.ai/blog"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cli := &client{httpClient: &http.Client{Transport: adapterOfflineTransport{respond: func(req *http.Request) (string, string) {
				switch req.URL.Path {
				case "/sitemap.xml":
					return `<urlset><url><loc>https://kimi.ai/blog/kimi-k2-5</loc><lastmod>2026-10-01</lastmod></url></urlset>`, "application/xml"
				case "/blog/kimi-k2-5":
					return strings.Replace(string(adapterFixture(t, "adapter-failure.html")), "<head>", `<head><meta property="article:published_time" content="2006-01-01">`, 1), "text/html"
				case "/api/v2/article/retrieval":
					return `{"success":false}`, "application/json"
				case "/blog":
					return `<rss version="2.0"><channel><title>Old source</title><item><guid>old</guid><title>Old original</title><link>https://qwen.ai/blog?id=old</link><pubDate>Sun, 01 Jan 2006 00:00:00 GMT</pubDate></item></channel></rss>`, "application/xml"
				default:
					t.Fatalf("unexpected offline source request: %s", req.URL)
					return "", ""
				}
			}}}}
			connector := &Connector{clientFactory: func(map[string]string) *client { return cli }}
			config := &types.DataSourceConfig{Settings: map[string]interface{}{"feed_urls": tc.feed, "article_path_prefix": "/blog/"}, IngestSince: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)}
			items, _, err := connector.FetchIncremental(context.Background(), config, nil)
			if err != nil || len(items) != 1 || items[0].Metadata["adapter_failure_only"] != "true" || len(items[0].AdapterFailures) != 1 || len(items[0].Content) != 0 {
				t.Fatalf("time filter lost adapter diagnostic: items=%+v err=%v", items, err)
			}
		})
	}
}

func TestSiteAdaptersCapturedHTMLRegression(t *testing.T) {
	cases := []struct{ name, raw, fixture, token string }{
		{"kimi-k25", "https://www.kimi.ai/blog/kimi-k2-5", "live/kimi-k25.html", "data-reliquary-kimi-widget"},
		{"kimi-k26", "https://www.kimi.ai/blog/kimi-k2-6", "live/kimi-k26.html", "data-reliquary-kimi-widget"},
		{"kimi-tables", "https://www.kimi.ai/blog/perception-bench", "live/kimi-tables.html", "data-reliquary-kimi-table"},
		{"qwen", "https://qwen.ai/blog?id=qwen-deepresearch", "live/qwen-article.html", "DeepResearch"},
		{"meta", "https://ai.meta.com/blog/introducing-muse-spark-meta-model-api/", "live/meta.html", "Muse Spark"},
		{"mistral", "https://mistral.ai/news/ocr-4/", "live/mistral.html", "OCR"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := adapterFixture(t, tc.fixture)
			whitelist := []string{"127.0.0.1", "::1"}
			for _, value := range regexp.MustCompile(`https://[^"<>\s\\]+`).FindAllString(string(raw), -1) {
				if u, err := url.Parse(value); err == nil && u.Hostname() != "" {
					whitelist = append(whitelist, u.Hostname())
				}
			}
			t.Setenv("SSRF_WHITELIST", strings.Join(whitelist, ","))
			utils.ResetSSRFWhitelistForTest()
			t.Cleanup(utils.ResetSSRFWhitelistForTest)
			videoChecks := 0
			cli := &client{httpClient: &http.Client{Transport: adapterOfflineTransport{respond: func(req *http.Request) (string, string) {
				videoChecks++
				if req.Header.Get("Range") != "bytes=0-0" {
					t.Fatalf("unexpected offline media request: %s", req.URL)
				}
				return "\x00", "video/mp4"
			}}}}
			article, err := cli.extractArticleBody(context.Background(), raw, tc.raw)
			if err != nil || len(article.failures) != 0 || !strings.Contains(article.contentHTML, tc.token) {
				t.Fatalf("captured page recovery failed: err=%v failures=%+v", err, article.failures)
			}
			if tc.name == "kimi-k25" && strings.Count(article.contentHTML, "data-reliquary-kimi-widget") != 8 {
				t.Fatal("captured K2.5 widgets incomplete")
			}
			if tc.name == "kimi-k26" && strings.Count(article.contentHTML, "data-reliquary-kimi-widget") != 9 {
				t.Fatal("captured K2.6 widgets incomplete")
			}
			if strings.HasPrefix(tc.name, "kimi-") && strings.Contains(article.contentHTML, "animate-pulse") {
				t.Fatal("captured source retains unresolved widget")
			}
			if tc.name == "meta" && !strings.Contains(article.contentHTML, "<img") {
				t.Fatal("captured source hero image lost")
			}
			t.Logf("captured HTML recovered offline; media transport checks=%d", videoChecks)
		})
	}
}

func TestSiteListingAdaptersCapturedHTMLRegression(t *testing.T) {
	cases := []struct{ raw, fixture, prefix string }{
		{"https://www.kimi.ai/blog/", "live/kimi-listing.html", "/blog/"},
		{"https://mistral.ai/news/", "live/mistral-listing.html", "/news/"},
		{"https://ai.meta.com/blog/", "live/meta-listing.html", "/blog/"},
		{"https://cursor.com/blog/", "live/cursor.html", "/blog/"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			items, err := parseListing(adapterFixture(t, tc.fixture), tc.raw, tc.prefix)
			if err != nil || len(items) == 0 {
				t.Fatalf("captured listing lost original messages: count=%d err=%v", len(items), err)
			}
			for _, item := range items {
				if strings.Contains(item.Link, "/blog/topic/") {
					t.Fatal("captured Cursor topic entered intake")
				}
			}
		})
	}
}

func TestSiteDiscoveryAdaptersCapturedSamplesRegression(t *testing.T) {
	cases := []struct{ raw, fixture string }{
		{"https://qwen.ai/blog", "live/qwen-api.json"},
		{"https://www.minimax.io/news", "live/minimax-api.json"},
		{"https://api-docs.deepseek.com/zh-cn/updates", "live/deepseek.html"},
	}
	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			adapter := defaultSiteAdapters().discoveryFor(tc.raw)
			items, err := adapter.discovery.parse(adapterFixture(t, tc.fixture), tc.raw)
			if err != nil || len(items) == 0 {
				t.Fatalf("captured discovery failed: count=%d err=%v", len(items), err)
			}
		})
	}
}

func TestSeedCapturedClientShellRecordsRecoveryFailure(t *testing.T) {
	article, _ := (&client{}).extractArticleBody(context.Background(), adapterFixture(t, "live/seed.html"), "https://seed.bytedance.com/zh/blog/edgebench")
	if len(article.failures) != 1 || article.failures[0].Adapter != "seed" || !strings.Contains(article.failures[0].Reason, "client-side shell") {
		t.Fatalf("captured source shell must be observable: %+v", article)
	}
}
