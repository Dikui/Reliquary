package rss

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"reliquary/internal/datasource"
	"reliquary/internal/types"
	"reliquary/internal/utils"
)

// TestMain whitelists loopback for SSRF so the httptest servers (127.0.0.1)
// are reachable. Production keeps the default strict SSRF policy.
func TestMain(m *testing.M) {
	_ = os.Setenv("SSRF_WHITELIST", "127.0.0.1,::1")
	utils.ResetSSRFWhitelistForTest()
	code := m.Run()
	os.Exit(code)
}

const longArticleBody = `<p>This is the first paragraph of a reasonably long article that the ` +
	`readability extractor should detect as the main content of the page. It contains ` +
	`enough words to clear the minimum content threshold used by the algorithm.</p>` +
	`<p>The second paragraph continues the discussion with more sentences so that the ` +
	`scoring heuristics confidently select this block over navigation and footer noise.</p>` +
	`<p>A third paragraph adds further substance, ensuring the article is unmistakably ` +
	`the dominant readable region of the document under test.</p>`

func TestFetchArticleForResearchUsesOriginalPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<html><head><title>Research Article</title><meta name="author" content="Ada Analyst"><meta property="og:site_name" content="Example Review"></head><body><article><h1>Research Article</h1>%s</article><p>Posted <a href="/2026/Sep/19/">19th September 2026</a></p></body></html>`, longArticleBody)
	}))
	defer server.Close()

	articleURL := server.URL + "/2026/Sep/19/example"
	article, err := FetchArticle(context.Background(), articleURL)
	if err != nil {
		t.Fatal(err)
	}
	if article.URL != articleURL || article.Metadata["published_at"] != "19th September 2026" ||
		article.Metadata["author"] != "Ada Analyst" || !strings.Contains(string(article.Content), "dominant readable region") {
		t.Fatalf("research article lost original page evidence: %+v", article)
	}
}

func TestExtractArticleUsesLargestImageVariant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, `<html><head><title>Image article</title></head><body><article><h1>Image article</h1>%s<img src="/blur.webp" data-srcset="/medium.webp 900w, /original.webp 2200w" alt="result"><img src="/blur2.webp" data-srcset="" srcset="/original2.webp 2200w" alt="second result"></article></body></html>`, longArticleBody)
	}))
	defer server.Close()

	content, _, _, _, _, err := newClient(nil).extractArticle(context.Background(), server.URL+"/article")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(content, server.URL+"/original.webp") || !strings.Contains(content, server.URL+"/original2.webp") || strings.Contains(content, server.URL+"/blur.webp") || strings.Contains(content, server.URL+"/blur2.webp") {
		t.Fatalf("article must preserve the largest available image instead of the blur preview: %s", content)
	}
}

func TestClientRejectsTruncatedArticle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", maxArticleSize+1)))
	}))
	defer server.Close()
	_, err := newClient(nil).fetch(context.Background(), server.URL, maxArticleSize, false)
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized article must fail rather than be treated as complete: %v", err)
	}
}

func TestSitemapUsesArticlePublicationDate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			fmt.Fprintf(w, `<?xml version="1.0"?><urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"><url><loc>http://%s/news/current</loc><lastmod>2026-09-24</lastmod></url><url><loc>http://%s/news/old</loc><lastmod>2026-09-24</lastmod></url><url><loc>http://%s/news/undated</loc><lastmod>2026-09-24</lastmod></url><url><loc>http://%s/about</loc><lastmod>2026-09-24</lastmod></url></urlset>`, r.Host, r.Host, r.Host, r.Host)
		case "/news/current", "/news/old", "/news/undated":
			date := map[string]string{"/news/current": "Sep 24, 2026", "/news/old": "Sep 20, 2026"}[r.URL.Path]
			fmt.Fprintf(w, `<html><body><main><h1>Article</h1><div>%s</div><article>%s</article></main></body></html>`, date, longArticleBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	baseURL, _ := url.Parse(server.URL + "/sitemap.xml")
	if sitemapArticleURL("https://outside.example/news/current", baseURL, "/news/") ||
		sitemapArticleURL(server.URL+"/about", baseURL, "/news/") {
		t.Fatal("sitemap discovery must stay on the configured site and article path")
	}
	config := &types.DataSourceConfig{Settings: map[string]interface{}{
		"feed_urls": server.URL + "/sitemap.xml", "article_path_prefix": "/news/", "publisher": "Example",
	}, IngestSince: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)}
	items, _, err := NewConnector().FetchIncremental(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected current article plus undated error, got %d items", len(items))
	}
	if items[0].Metadata["published_at"] != "Sep 24, 2026" || len(items[0].Content) == 0 {
		t.Fatalf("current article lost its actual publication evidence: %+v", items[0].Metadata)
	}
	if items[1].Metadata["error"] == "" || len(items[1].Content) != 0 || items[1].URL != "" {
		t.Fatalf("undated article must not enter intake: %+v", items[1])
	}
}

func TestListingUsesArticlePublicationDateAndSameSiteLinks(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/articles":
			fmt.Fprint(w, `<html><body><a href="/articles/current?tracking=1"><h3>Technical blog</h3></a><div class="menu-card"><a href="/articles/current"><h3>Current</h3></a><p class="card-date">2026-09-24</p></div><a href="/articles/old">Old</a><a href="/articles/undated">Undated</a><a href="/about">About</a><a href="https://outside.example/articles/foreign">Foreign</a></body></html>`)
		case "/articles/current", "/articles/old", "/articles/undated":
			date := map[string]string{"/articles/old": "Sep 20, 2026"}[r.URL.Path]
			heading := "Article"
			if r.URL.Path == "/articles/current" {
				heading = "Current"
			}
			fmt.Fprintf(w, `<html><body><main><article><h1>%s</h1><div>%s</div>%s</article></main></body></html>`, heading, date, longArticleBody)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	config := &types.DataSourceConfig{Settings: map[string]interface{}{
		"feed_urls": server.URL + "/articles", "article_path_prefix": "/articles/", "publisher": "Example",
	}, IngestSince: time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)}
	if err := NewConnector().Validate(context.Background(), config); err != nil {
		t.Fatal(err)
	}
	items, _, err := NewConnector().FetchIncremental(context.Background(), config, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].URL != server.URL+"/articles/current" || items[0].Title != "Current" || items[0].Metadata["published_at"] != "2026-09-24" ||
		items[1].Metadata["error"] == "" || len(items[1].Content) != 0 {
		t.Fatalf("listing must keep current original, reject undated, skip old/off-site: %+v", items)
	}
}

func TestDeepSeekChangelogSplitsDatedEntries(t *testing.T) {
	const pageURL = "https://api-docs.deepseek.com/zh-cn/updates/"
	if !isDeepSeekUpdatesURL(pageURL) || isDeepSeekUpdatesURL("https://example.com/zh-cn/updates/") {
		t.Fatal("changelog parser must be limited to the official updates page")
	}
	page := `<div class="theme-doc-markdown markdown"><div class="col col--12">` +
		`<h2 id="date-one">时间: 2026-09-10</h2><h3 id="first">第一条<a href="#first">​</a></h3>` +
		`<p>完整更新内容 <a href="/zh-cn/news/first">详情</a></p><img src="/blur.webp" srcset="/original.webp 2200w">` +
		`<table><tr><th>Benchmark</th><th>Score</th></tr><tr><td>GPQA</td><td>90.9</td></tr></table>` +
		`<h2 id="date-two">时间: 2026-08-21</h2><h3 id="second">第二条</h3><p>第二条正文</p></div></div>`
	items, err := parseDeepSeekUpdates([]byte(page), pageURL)
	if err != nil || len(items) != 2 {
		t.Fatalf("expected two dated updates: items=%d err=%v", len(items), err)
	}
	if items[0].Link != pageURL+"#first" || items[0].Published != "2026-09-10" ||
		!strings.Contains(items[0].Content, "https://api-docs.deepseek.com/zh-cn/news/first") ||
		!strings.Contains(items[0].Content, "https://api-docs.deepseek.com/original.webp") ||
		strings.Contains(items[0].Content, "第二条正文") {
		t.Fatalf("first update lost its original boundary, date or resources: %+v", items[0])
	}
	resolved := NewConnector().resolveItem(context.Background(), newClient(nil), &gofeed.Feed{Title: "DeepSeek"}, items[0], pageURL, items[0].GUID, "DeepSeek", true)
	if resolved.item.Metadata["error"] != "" || resolved.item.Metadata["published_at"] != "2026-09-10" ||
		!strings.Contains(string(resolved.item.Content), "完整更新内容") ||
		!strings.Contains(string(resolved.item.Content), "| Benchmark") ||
		!strings.Contains(string(resolved.item.Content), "| GPQA") {
		t.Fatalf("inline original should use the shared intake item: %+v", resolved.item)
	}
	items[0].Content = `<p>Demo</p><iframe src="https://example.com/demo"></iframe>`
	resolved = NewConnector().resolveItem(context.Background(), newClient(nil), &gofeed.Feed{Title: "DeepSeek"}, items[0], pageURL, items[0].GUID, "DeepSeek", true)
	if resolved.item.Metadata["error"] == "" || len(resolved.item.Content) != 0 {
		t.Fatalf("unpreserved inline embed must not enter intake: %+v", resolved.item)
	}
}

func TestQwenOfficialArticleBody(t *testing.T) {
	if !isQwenBlogURL("https://qwen.ai/blog") || isQwenBlogURL("https://example.com/blog") {
		t.Fatal("Qwen API route must be limited to the official blog")
	}
	response, _ := json.Marshal(map[string]interface{}{
		"success": true,
		"data": map[string]interface{}{"articles": []map[string]interface{}{
			{"path": "undated", "title": "Undated", "content": `<div class="post-content"><p>Original but no date</p></div>`},
			{"path": "release", "title": "Release", "content": `<article><div class="post-content"><p>Full original article</p><img src="/blur.png" srcset="/full.png 1800w"><table><tr><th>Model</th><th>Score</th></tr><tr><td>Qwen</td><td>95</td></tr></table></div></article>`, "extra": map[string]string{"date": "2026-09-20T20:00:00+08:00", "author": "QwenTeam"}},
			{"path": "video", "title": "Video", "content": `<article><div class="post-content"><p>Demo</p><video></video></div></article>`, "extra": map[string]string{"date": "2026-09-21T20:00:00+08:00", "author": "QwenTeam"}},
			{"path": "empty", "title": "Empty", "content": `<article><p>No original article body</p></article>`, "extra": map[string]string{"date": "2026-09-22T20:00:00+08:00"}},
			{"title": "No URL", "content": `<div class="post-content"><p>Original but no URL</p></div>`, "extra": map[string]string{"date": "2026-09-23T20:00:00+08:00"}},
		}},
	})
	items, err := parseQwenArticles(response)
	if err != nil || len(items) != 5 || items[0].Custom["error"] == "" || items[0].Content == "" ||
		items[3].Custom["error"] == "" || items[3].Content != "" || items[4].Custom["error"] == "" || items[4].Content == "" {
		t.Fatalf("malformed Qwen articles must fail independently: items=%+v err=%v", items, err)
	}
	good := NewConnector().resolveItem(context.Background(), newClient(nil), &gofeed.Feed{Title: "Qwen"}, items[1], "https://qwen.ai/blog", items[1].GUID, "Qwen", true).item
	if good.Metadata["error"] != "" || good.URL != "https://qwen.ai/blog?id=release" || good.Metadata["author"] != "QwenTeam" ||
		good.Metadata["published_at"] != "2026-09-20T20:00:00+08:00" ||
		!strings.Contains(string(good.Content), "https://qwen.ai/full.png") || !strings.Contains(string(good.Content), "| Model") {
		t.Fatalf("Qwen body lost original content or source fields: %+v", good)
	}
	bad := NewConnector().resolveItem(context.Background(), newClient(nil), &gofeed.Feed{Title: "Qwen"}, items[2], "https://qwen.ai/blog", items[2].GUID, "Qwen", true).item
	if bad.Metadata["error"] == "" || len(bad.Content) != 0 {
		t.Fatalf("embedded media must not be silently dropped: %+v", bad)
	}
}

func TestQwenMissingPathIdentitySurvivesReorder(t *testing.T) {
	missingPath := map[string]interface{}{"title": "No URL", "content": `<div class="post-content"><p>Original body</p></div>`,
		"extra": map[string]string{"date": "2026-09-23"}}
	other := map[string]interface{}{"path": "other", "title": "Other", "content": `<div class="post-content"><p>Other body</p></div>`,
		"extra": map[string]string{"date": "2026-09-24"}}
	parse := func(articles []map[string]interface{}) []*gofeed.Item {
		data, err := json.Marshal(map[string]interface{}{"success": true, "data": map[string]interface{}{"articles": articles}})
		if err != nil {
			t.Fatal(err)
		}
		items, err := parseQwenArticles(data)
		if err != nil {
			t.Fatal(err)
		}
		return items
	}
	first := parse([]map[string]interface{}{missingPath, other})
	reordered := parse([]map[string]interface{}{other, missingPath})
	if first[0].GUID == "" || first[0].GUID != reordered[1].GUID {
		t.Fatalf("missing-path identity changed with list order: %q != %q", first[0].GUID, reordered[1].GUID)
	}
}

func TestZhipuResearchDiscoversUnlinkedCurrentArticles(t *testing.T) {
	const listingURL = "https://www.zhipuai.cn/zh/research"
	if !isZhipuResearchURL(listingURL) || isZhipuResearchURL("https://example.com/zh/research") {
		t.Fatal("Zhipu page-data parser must be limited to the official research listing")
	}
	flight := `6:["$","div",null,{"blogsItems":[{"id":163,"title_zh":"GLM-5.3-Flash","createAt":"2026-08-26T14:00:00.000Z"},{"id":162,"title_zh":"GLM-5.3","createAt":"2026-08-14T06:00:00.000Z"}]}]`
	push, err := json.Marshal([]interface{}{1, flight})
	if err != nil {
		t.Fatal(err)
	}
	page := []byte("<html><script>self.__next_f.push(" + string(push) + ")</script></html>")
	items, err := parseListing(page, listingURL, "/zh/research/")
	if err != nil || len(items) != 2 {
		t.Fatalf("expected both current articles without static anchors: items=%+v err=%v", items, err)
	}
	if items[0].Link != listingURL+"/163" || items[0].Title != "GLM-5.3-Flash" ||
		items[0].Published != "" || items[0].UpdatedParsed == nil ||
		time.Since(*items[0].UpdatedParsed) > time.Minute {
		t.Fatalf("article discovery lost ID or used a discovery date as publication evidence: %+v", items[0])
	}
}

func TestMiniMaxNewsDiscoversCurrentArticles(t *testing.T) {
	const newsURL = "https://www.minimax.io/news"
	if !isMiniMaxNewsURL(newsURL) || isMiniMaxNewsURL("https://example.com/news") {
		t.Fatal("MiniMax news API must be limited to the official listing")
	}
	items, err := parseMiniMaxNews([]byte(`{"data":[{"slug":"minimax-h3-open-source","title":"MiniMax H3"},{"slug":"minimax-at-humanx-2026","title":"MiniMax at HumanX"}]}`))
	if err != nil || len(items) != 2 {
		t.Fatalf("expected current news links: items=%+v err=%v", items, err)
	}
	if items[0].Link != newsURL+"/minimax-h3-open-source" || items[0].Title != "MiniMax H3" ||
		items[0].Published != "" || items[0].UpdatedParsed == nil ||
		time.Since(*items[0].UpdatedParsed) > time.Minute {
		t.Fatalf("news discovery lost original link or claimed an unverified publication date: %+v", items[0])
	}
}

func TestArticlePublishedAtReadsStructuredDateAndFallsBackToHero(t *testing.T) {
	seed, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><h1>EdgeBench</h1><aside><p>日期</p><p>2026-07-07</p></aside></body></html>`))
	if err != nil || articlePublishedAt(seed, "https://seed.bytedance.com/zh/blog/edgebench") != "2026-07-07" {
		t.Fatalf("Seed sidebar publication date not found: %v", err)
	}
	structured, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><script type="application/ld+json">{"@type":"BlogPosting","mainEntityOfPage":"https://mistral.ai/news/other/","datePublished":"2026-01-01"}</script><script type="application/ld+json">{"@type":"BlogPosting","mainEntityOfPage":"https://mistral.ai/news/ocr-4/","datePublished":"2026-06-23T12:00:48.000Z","dateModified":"2026-09-10T14:01:55.122Z"}</script></head><body><article><h1>OCR</h1></article></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := articlePublishedAt(structured, "https://mistral.ai/news/ocr-4/"); got != "2026-06-23T12:00:48.000Z" {
		t.Fatalf("structured publication date = %q", got)
	}
	graph, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><script type="application/ld+json">{"@graph":[{"@type":"NewsArticle","@id":"https://www.minimax.io/news/other#article","datePublished":"2026-02-01"},{"@type":"NewsArticle","@id":"https://www.minimax.io/news/minimax-speech-28#article","mainEntityOfPage":{"@type":"WebPage","@id":"https://www.minimax.io/news/minimax-speech-28"},"datePublished":"2026-01-23T00:00:00.000Z"}]}</script></head></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := articlePublishedAt(graph, "https://www.minimax.io/news/minimax-speech-28"); got != "2026-01-23T00:00:00.000Z" {
		t.Fatalf("graph publication date = %q", got)
	}
	hero, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><head><script type="application/ld+json">{"@type":"BlogPosting","mainEntityOfPage":"https://ai.meta.com/blog/model-release/","datePublished":"invalid"}</script></head><body><main><div><h1>Model release</h1><div><span>July 9, 2026</span><span>10 minute read</span></div></div></main></body></html>`))
	if err != nil {
		t.Fatal(err)
	}
	if got := articlePublishedAt(hero, "https://ai.meta.com/blog/model-release/"); got != "July 9, 2026" {
		t.Fatalf("hero publication date = %q", got)
	}
}

// fakeFeed spins up an httptest server serving an RSS feed and article pages.
type fakeFeed struct {
	server             *httptest.Server
	feedTitle          string
	itemContent        string // optional <description>/content for items
	articleAuthHeaders []string
	articleFetches     atomic.Int32
	failFeed           atomic.Bool
	failArticle        atomic.Bool
	articleEmbed       string
	pageEmbed          string
	articleByline      string
}

func newFakeFeed(t *testing.T) *fakeFeed {
	t.Helper()
	f := &fakeFeed{feedTitle: "Test Feed"}
	mux := http.NewServeMux()

	mux.HandleFunc("/article/", func(w http.ResponseWriter, r *http.Request) {
		f.articleFetches.Add(1)
		if f.failArticle.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		for k, vals := range r.Header {
			if strings.EqualFold(k, "X-Test-Auth") && len(vals) > 0 && vals[0] != "" {
				f.articleAuthHeaders = append(f.articleAuthHeaders, vals[0])
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head><title>%s</title><meta name="author" content="%s"></head>`+
			`<body><nav>menu</nav><article><h1>Heading</h1>%s%s</article><footer>foot%s</footer></body></html>`,
			"Full "+r.URL.Path, f.articleByline, longArticleBody, f.articleEmbed, f.pageEmbed)
	})
	mux.HandleFunc("/video.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		if r.Method != http.MethodGet || r.Header.Get("Range") != "bytes=0-0" {
			http.Error(w, "expected bounded GET", http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write([]byte{0})
	})
	mux.HandleFunc("/head-only.mp4", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		if r.Method != http.MethodHead {
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		if f.failFeed.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if auth := r.Header.Get("X-Test-Auth"); f.itemContent == "needs-auth" && auth != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		base := "http://" + r.Host
		desc := "summary fallback"
		fmt.Fprintf(w, `<?xml version="1.0"?>
<rss version="2.0"><channel>
<title>%s</title>
<link>%s</link>
<description>A test feed</description>
<item>
  <title>Article One</title>
  <link>%s/article/a1</link>
  <guid>guid-1</guid>
  <pubDate>Mon, 02 Jan 2006 15:04:05 GMT</pubDate>
  <description>%s</description>
</item>
<item>
  <title>Article Two</title>
  <link>%s/article/a2</link>
  <guid>guid-2</guid>
  <pubDate>Tue, 03 Jan 2006 15:04:05 GMT</pubDate>
  <description>%s</description>
</item>
</channel></rss>`, f.feedTitle, base, base, desc, base, desc)
	})

	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeFeed) feedURL() string { return f.server.URL + "/feed.xml" }

func makeConfig(feedURLs string, headers string) *types.DataSourceConfig {
	cfg := &types.DataSourceConfig{
		Type: types.ConnectorTypeRSS,
		Settings: map[string]interface{}{
			"feed_urls": feedURLs,
		},
		Credentials: map[string]interface{}{},
	}
	if headers != "" {
		cfg.Credentials["auth_headers"] = headers
	}
	return cfg
}

func TestConnector_Type(t *testing.T) {
	if NewConnector().Type() != types.ConnectorTypeRSS {
		t.Fatalf("Type() = %q, want %q", NewConnector().Type(), types.ConnectorTypeRSS)
	}
}

func TestParseConfig_RequiresFeedURLs(t *testing.T) {
	if _, err := parseConfig(makeConfig("   ", "")); err == nil {
		t.Fatal("expected error when feed_urls is blank")
	}
}

func TestParseConfig_FeedURLsFromSettings(t *testing.T) {
	cfg, err := parseConfig(makeConfig("https://example.com/feed.xml", ""))
	if err != nil {
		t.Fatalf("parseConfig error: %v", err)
	}
	got := cfg.feedURLList()
	if len(got) != 1 || got[0] != "https://example.com/feed.xml" {
		t.Fatalf("feedURLList = %v", got)
	}
}

func TestParseConfig_LegacyFeedURLsInCredentials(t *testing.T) {
	legacy := &types.DataSourceConfig{
		Type: types.ConnectorTypeRSS,
		Credentials: map[string]interface{}{
			"feed_urls": "https://legacy.example/feed.xml",
		},
	}
	cfg, err := parseConfig(legacy)
	if err != nil {
		t.Fatalf("parseConfig error: %v", err)
	}
	if got := cfg.feedURLList(); len(got) != 1 || got[0] != "https://legacy.example/feed.xml" {
		t.Fatalf("feedURLList = %v", got)
	}
}

func TestConfig_FeedURLList_SplitsAndDedupes(t *testing.T) {
	cfg := &Config{FeedURLs: "https://a.com/f, https://b.com/f\nhttps://a.com/f\n"}
	got := cfg.feedURLList()
	want := []string{"https://a.com/f", "https://b.com/f"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("feedURLList = %v, want %v", got, want)
	}
}

func TestConfig_ParseHeaders(t *testing.T) {
	cfg := &Config{AuthHeaders: "Authorization: Bearer x\nX-Foo:  bar \nbroken-line\n: noname"}
	got := cfg.parseHeaders()
	if got["Authorization"] != "Bearer x" {
		t.Errorf("Authorization = %q", got["Authorization"])
	}
	if got["X-Foo"] != "bar" {
		t.Errorf("X-Foo = %q", got["X-Foo"])
	}
	if len(got) != 2 {
		t.Errorf("expected 2 headers, got %d: %v", len(got), got)
	}
}

func TestFeedSignalFingerprint(t *testing.T) {
	item := &gofeed.Item{GUID: "g1", Link: "https://example.com/a", Title: "t"}
	sig1 := feedSignalFingerprint(item, "body")
	sig2 := feedSignalFingerprint(item, "body")
	if sig1 == "" || sig1 != sig2 {
		t.Fatalf("feed signal unstable: %q vs %q", sig1, sig2)
	}
	sig3 := feedSignalFingerprint(item, "changed")
	if sig1 == sig3 {
		t.Fatal("expected different signal when feed content changes")
	}
}

func TestItemExternalID_ScopesByFeed(t *testing.T) {
	got := itemExternalID("https://a.com/feed", "guid-1")
	want := "https://a.com/feed:guid-1"
	if got != want {
		t.Fatalf("itemExternalID = %q, want %q", got, want)
	}
}

func TestConnector_Validate_Success(t *testing.T) {
	f := newFakeFeed(t)
	if err := NewConnector().Validate(context.Background(), makeConfig(f.feedURL(), "")); err != nil {
		t.Fatalf("Validate error: %v", err)
	}
}

func TestConnector_Validate_PrivateFeedWithHeader(t *testing.T) {
	f := newFakeFeed(t)
	f.itemContent = "needs-auth"

	// Without the header → 401.
	if err := NewConnector().Validate(context.Background(), makeConfig(f.feedURL(), "")); err == nil {
		t.Fatal("expected error without auth header")
	}
	// With the header → success.
	if err := NewConnector().Validate(
		context.Background(), makeConfig(f.feedURL(), "X-Test-Auth: secret"),
	); err != nil {
		t.Fatalf("Validate with header error: %v", err)
	}
}

func TestConnector_FetchAll_DoesNotSendAuthHeadersToArticles(t *testing.T) {
	f := newFakeFeed(t)
	f.itemContent = "needs-auth"
	_, err := NewConnector().FetchAll(
		context.Background(), makeConfig(f.feedURL(), "X-Test-Auth: secret"), nil,
	)
	if err != nil {
		t.Fatalf("FetchAll error: %v", err)
	}
	if len(f.articleAuthHeaders) != 0 {
		t.Fatalf("article requests must not carry feed auth headers, got %v", f.articleAuthHeaders)
	}
}

func TestConnector_ListResources(t *testing.T) {
	f := newFakeFeed(t)
	res, err := NewConnector().ListResources(context.Background(), makeConfig(f.feedURL(), ""), "")
	if err != nil {
		t.Fatalf("ListResources error: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(res))
	}
	if res[0].ExternalID != f.feedURL() {
		t.Errorf("ExternalID = %q, want %q", res[0].ExternalID, f.feedURL())
	}
	if res[0].Name != "Test Feed" {
		t.Errorf("Name = %q, want %q", res[0].Name, "Test Feed")
	}

	// Non-empty parentID → empty (feeds are flat).
	children, err := NewConnector().ListResources(context.Background(), makeConfig(f.feedURL(), ""), "feed-x")
	if err != nil {
		t.Fatalf("ListResources(parent) error: %v", err)
	}
	if len(children) != 0 {
		t.Fatalf("expected no children, got %d", len(children))
	}
}

func TestConnector_FetchAll_FullTextMarkdown(t *testing.T) {
	f := newFakeFeed(t)
	items, err := NewConnector().FetchAll(context.Background(), makeConfig(f.feedURL(), ""), nil)
	if err != nil {
		t.Fatalf("FetchAll error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	it := items[0]
	wantExternalID := itemExternalID(f.feedURL(), "guid-1")
	if it.ExternalID != wantExternalID {
		t.Errorf("ExternalID = %q, want %q", it.ExternalID, wantExternalID)
	}
	if it.ContentType != "text/markdown" {
		t.Errorf("ContentType = %q, want text/markdown", it.ContentType)
	}
	if it.Metadata["channel"] != types.ChannelRSS {
		t.Errorf("channel = %q, want %q", it.Metadata["channel"], types.ChannelRSS)
	}
	// Full text from the article page should be present (not the short summary).
	if !strings.Contains(string(it.Content), "first paragraph") {
		t.Errorf("expected full article text in content, got: %q", string(it.Content))
	}
	if !strings.HasSuffix(it.FileName, ".md") {
		t.Errorf("FileName = %q, want .md suffix", it.FileName)
	}
}

func TestConnector_FetchIncremental_StartsAtEnableTimeAndRetriesArticleFailure(t *testing.T) {
	f := newFakeFeed(t)
	cfg := makeConfig(f.feedURL(), "")
	cfg.Settings["publisher"] = "Example Institution"
	cfg.IngestSince = time.Date(2006, 1, 3, 0, 0, 0, 0, time.UTC)
	f.failArticle.Store(true)
	items, cursor, err := NewConnector().FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Metadata["error"] == "" || len(items[0].Content) != 0 || items[0].URL != "" {
		t.Fatalf("failed article must be logged and withheld, got %#v", items)
	}
	if got := f.articleFetches.Load(); got != 1 {
		t.Fatalf("article fetches = %d, want 1", got)
	}
	f.failArticle.Store(false)
	f.articleByline = "Article Byline"
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !strings.Contains(string(items[0].Content), "first paragraph") {
		t.Fatalf("next run should retry the article and keep the full body, got %#v", items)
	}
	if items[0].Metadata["author"] != "Article Byline" || items[0].Metadata["publisher"] != "Example Institution" || items[0].Metadata["published_at"] != "Tue, 03 Jan 2006 15:04:05 GMT" {
		t.Fatalf("publisher and original date must remain distinct from personal author: %#v", items[0].Metadata)
	}
	f.articleByline = "Example Institution"
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["author"] != "" || items[0].Metadata["publisher"] != "Example Institution" {
		t.Fatalf("organization byline should be recorded as publisher only: items=%#v err=%v", items, err)
	}
}

func TestConnector_FetchIncremental_RejectsUnpreservedEmbeddedMedia(t *testing.T) {
	f := newFakeFeed(t)
	f.articleEmbed = `<p>The next interactive chart is essential to understand this article and its results.</p><iframe src="https://example.com/important-chart"></iframe><p>The chart concludes the analysis.</p>`
	cfg := makeConfig(f.feedURL(), "")
	cfg.IngestSince = time.Date(2006, 1, 3, 0, 0, 0, 0, time.UTC)
	items, cursor, err := NewConnector().FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Metadata["error"] == "" || len(items[0].Content) != 0 {
		t.Fatalf("unpreserved embed must not enter knowledge intake: %#v", items)
	}
	f.articleEmbed = `<p>The chart below carries the measured results.</p><canvas></canvas>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["error"] == "" || len(items[0].Content) != 0 {
		t.Fatalf("unpreserved canvas must not enter knowledge intake: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<uni-inline-image or-mp4-video-url="/missing.mp4"><p>Watch the model demo.</p></uni-inline-image>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["error"] == "" || len(items[0].Content) != 0 {
		t.Fatalf("unpreserved custom video must not enter knowledge intake: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<video src="` + f.server.URL + `/head-only.mp4"></video>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["error"] == "" || len(items[0].Content) != 0 {
		t.Fatalf("HEAD-only video link must not enter knowledge intake: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<p>Before the demo.</p><video src="` + f.server.URL + `/video.mp4"></video><p>After the demo.</p>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["error"] != "" ||
		!strings.Contains(string(items[0].Content), f.server.URL+"/video.mp4") ||
		!strings.Contains(string(items[0].Content), "播放原始视频") ||
		!strings.Contains(string(items[0].Content), "Before the demo.") || !strings.Contains(string(items[0].Content), "After the demo.") {
		t.Fatalf("verified article video must remain playable in its original context: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<uni-inline-image or-mp4-video-url="` + f.server.URL + `/video.mp4"><p>Watch the model demo.</p></uni-inline-image>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || items[0].Metadata["error"] != "" || !strings.Contains(string(items[0].Content), f.server.URL+"/video.mp4") {
		t.Fatalf("verified custom video must survive extraction: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<h3>Full Benchmark Table</h3><div data-blog-hero-width="true"><div class="animate-pulse" style="min-height:360px"></div></div>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || !strings.Contains(items[0].Metadata["error"], "unresolved content placeholder") || len(items[0].Content) != 0 {
		t.Fatalf("unrendered benchmark table must not enter knowledge intake: items=%#v err=%v", items, err)
	}
	f.articleEmbed = `<a class="animate-pulse" href="/login"></a>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || len(items[0].Content) == 0 {
		t.Fatalf("empty animated link is not missing article content: items=%#v err=%v", items, err)
	}
	f.articleEmbed = ""
	f.pageEmbed = `<iframe src="https://example.com/footer-ad"></iframe>`
	items, _, err = NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil || len(items) != 1 || len(items[0].Content) == 0 {
		t.Fatalf("footer embed must not block complete article: items=%#v err=%v", items, err)
	}
}

func TestConnector_FetchIncremental_SkipsWithoutArticleFetch(t *testing.T) {
	f := newFakeFeed(t)
	cfg := makeConfig(f.feedURL(), "")
	cfg.ResourceIDs = []string{f.feedURL()}

	items, cursor, err := NewConnector().FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("first FetchIncremental error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items on first sync, got %d", len(items))
	}
	if got := f.articleFetches.Load(); got != 2 {
		t.Fatalf("first sync article fetches = %d, want 2", got)
	}

	f.articleFetches.Store(0)
	items2, _, err := NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil {
		t.Fatalf("second FetchIncremental error: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("expected 0 items on unchanged second sync, got %d", len(items2))
	}
	if got := f.articleFetches.Load(); got != 0 {
		t.Fatalf("unchanged incremental sync must not refetch articles, got %d fetches", got)
	}
}

func TestConnector_FetchIncremental_SkipsUnchanged(t *testing.T) {
	f := newFakeFeed(t)
	cfg := makeConfig(f.feedURL(), "")
	cfg.ResourceIDs = []string{f.feedURL()}

	// First sync: everything is new.
	items, cursor, err := NewConnector().FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("first FetchIncremental error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items on first sync, got %d", len(items))
	}
	if cursor == nil {
		t.Fatal("expected non-nil cursor")
	}

	// Second sync with the returned cursor: nothing changed → no items.
	items2, _, err := NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err != nil {
		t.Fatalf("second FetchIncremental error: %v", err)
	}
	if len(items2) != 0 {
		t.Fatalf("expected 0 items on unchanged second sync, got %d", len(items2))
	}
}

func TestConnector_Walk_PartialFeedFailure(t *testing.T) {
	f := newFakeFeed(t)
	cfg := makeConfig(f.feedURL()+", https://invalid.invalid/feed.xml", "")
	items, err := NewConnector().FetchAll(context.Background(), cfg, nil)
	var partial *datasource.PartialFetchError
	if !errors.As(err, &partial) {
		t.Fatalf("expected PartialFetchError, got %v", err)
	}
	if len(partial.Details) != 1 {
		t.Fatalf("expected 1 feed error, got %v", partial.Details)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items from healthy feed, got %d", len(items))
	}
}

func TestConnector_Walk_PreservesCursorOnFeedFailure(t *testing.T) {
	f := newFakeFeed(t)
	cfg := makeConfig(f.feedURL(), "")
	cfg.ResourceIDs = []string{f.feedURL()}

	_, cursor, err := NewConnector().FetchIncremental(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("first sync error: %v", err)
	}

	f.failFeed.Store(true)
	_, newCursor, err := NewConnector().FetchIncremental(context.Background(), cfg, cursor)
	if err == nil {
		t.Fatal("expected error when sole feed is unavailable")
	}
	if newCursor == nil || newCursor.ConnectorCursor == nil {
		t.Fatal("expected cursor to be preserved on feed failure")
	}

	var restored rssCursor
	b, err := json.Marshal(newCursor.ConnectorCursor)
	if err != nil {
		t.Fatalf("marshal cursor: %v", err)
	}
	if err := json.Unmarshal(b, &restored); err != nil {
		t.Fatalf("unmarshal cursor: %v", err)
	}
	items := restored.FeedItems[f.feedURL()]
	if len(items) != 2 {
		t.Fatalf("expected preserved fingerprints for 2 items, got %d", len(items))
	}
	if items["guid-1"] == "" || items["guid-2"] == "" {
		t.Fatalf("expected non-empty preserved fingerprints, got %+v", items)
	}
}

func TestConnector_ResolveResourceAncestors_Empty(t *testing.T) {
	got, err := NewConnector().ResolveResourceAncestors(
		context.Background(), makeConfig("https://a.com/f", ""), []string{"x"},
	)
	if err != nil {
		t.Fatalf("ResolveResourceAncestors error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty ancestors, got %v", got)
	}
}
