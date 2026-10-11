package rss

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	readability "codeberg.org/readeck/go-readability/v2"
	"github.com/PuerkitoBio/goquery"
	"github.com/mmcdole/gofeed"
	"reliquary/internal/types"
)

// SiteRule limits an adapter to an explicit domain and path.
type SiteRule struct {
	Domains      []string
	Path         string
	Prefix       bool
	ExcludePaths []string
}

func (r SiteRule) Matches(u *url.URL) bool {
	if u == nil {
		return false
	}
	found := false
	for _, domain := range r.Domains {
		if strings.EqualFold(u.Hostname(), domain) {
			found = true
			break
		}
	}
	if !found {
		return false
	}
	path := strings.TrimRight(u.Path, "/")
	for _, excluded := range r.ExcludePaths {
		if path == excluded {
			return false
		}
	}
	if r.Prefix {
		prefix := strings.TrimRight(r.Path, "/")
		return path == prefix || strings.HasPrefix(path, prefix+"/")
	}
	return path == strings.TrimRight(r.Path, "/")
}

// SitePage contains the original page and the generic readable article.
// Each recovery receives isolated trees, so a failed recovery cannot alter fallback content.
type SitePage struct {
	Document *goquery.Document
	Content  *goquery.Selection
	URL      *url.URL
	Article  *readability.Article
	client   *client
}

// SiteRecovery reports success explicitly, with a reason for every failure.
type SiteRecovery struct {
	Success     bool
	Reason      string
	ContentHTML string
	Author      string
	Publisher   string
	PublishedAt string
}

// SiteAdapter declares matching rules and one original-body recovery function.
// Discovery and request options are private extensions for listing-only sites.
type SiteAdapter struct {
	Name           string
	Rules          []SiteRule
	Recover        func(context.Context, *SitePage) SiteRecovery
	discovery      *siteDiscovery
	listing        *listingProfile
	requestHeaders http.Header
	publishedAt    func(*goquery.Document) string
}

func (a SiteAdapter) Matches(u *url.URL) bool {
	for _, rule := range a.Rules {
		if rule.Matches(u) {
			return true
		}
	}
	return false
}

type siteDiscovery struct {
	sourceURL        string
	inlineOriginal   bool
	needsArticleDate bool
	parse            func([]byte, string) ([]*gofeed.Item, error)
}

type listingProfile struct {
	selector     string
	skip         func(*url.URL) bool
	date         func(*goquery.Selection) string
	improveTitle func(string, string) bool
}

// SiteAdapterRegistry keeps page-specific entry outside the generic connector.
type SiteAdapterRegistry struct{ adapters []SiteAdapter }

func NewSiteAdapterRegistry(adapters ...SiteAdapter) *SiteAdapterRegistry {
	return &SiteAdapterRegistry{adapters: append([]SiteAdapter(nil), adapters...)}
}

func defaultSiteAdapters() *SiteAdapterRegistry {
	return NewSiteAdapterRegistry(kimiK25Adapter(), kimiK26Adapter(), kimiTablesAdapter(), kimiListingAdapter(), qwenAdapter(), minimaxAdapter(), deepSeekAdapter(), zhipuAdapter(), mistralAdapter(), metaAdapter(), cursorAdapter(), seedAdapter(), openAIAdapter())
}

func (r *SiteAdapterRegistry) matching(u *url.URL) []SiteAdapter {
	var out []SiteAdapter
	for _, adapter := range r.adapters {
		if adapter.Matches(u) {
			out = append(out, adapter)
		}
	}
	return out
}

func (c *client) adapterRegistry() *SiteAdapterRegistry {
	if c.adapters != nil {
		return c.adapters
	}
	return defaultSiteAdapters()
}

func sourceAdapterFailure(adapter string, u *url.URL, reason string) types.SourceAdapterFailure {
	raw, site := "", ""
	if u != nil {
		raw, site = u.String(), strings.ToLower(u.Hostname())
	}
	return types.SourceAdapterFailure{Adapter: adapter, Site: site, URL: raw, Reason: reason}
}

func genericArticleContent(doc *goquery.Document) *goquery.Selection {
	selection := doc.Find("article")
	if selection.Length() == 0 {
		selection = doc.Find("main")
	}
	if selection.Length() == 0 {
		selection = doc.Find("div#body.prose")
	}
	var content *goquery.Selection
	selection.Each(func(_ int, candidate *goquery.Selection) {
		if content == nil || len(candidate.Text()) > len(content.Text()) {
			content = candidate
		}
	})
	if content == nil {
		content = doc.Selection
	}
	if content.Is("main") {
		if body := content.Find(".blog-content"); body.Length() == 1 && body.Find("h1").Length() == 1 {
			content = body
		}
	}
	return content
}

func cloneSitePage(page *SitePage) (*SitePage, error) {
	raw, err := page.Document.Html()
	if err != nil {
		return nil, err
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(raw))
	if err != nil {
		return nil, err
	}
	copy := &SitePage{Document: doc, Content: genericArticleContent(doc), URL: page.URL, client: page.client}
	if page.Article != nil {
		article := *page.Article
		var body bytes.Buffer
		if err := article.RenderHTML(&body); err != nil {
			return nil, err
		}
		parsed, err := goquery.NewDocumentFromReader(&body)
		if err != nil {
			return nil, err
		}
		node := parsed.Find("body").Get(0)
		article.Node = node
		copy.Article = &article
	}
	return copy, nil
}

func (r *SiteAdapterRegistry) recover(ctx context.Context, page *SitePage) (SiteRecovery, []types.SourceAdapterFailure) {
	result := SiteRecovery{Success: true}
	var failures []types.SourceAdapterFailure
	for _, adapter := range r.matching(page.URL) {
		if adapter.Recover == nil {
			continue
		}
		isolated, err := cloneSitePage(page)
		recovered := SiteRecovery{Reason: "clone article: " + fmt.Sprint(err)}
		if err == nil {
			recovered = adapter.Recover(ctx, isolated)
		}
		if !recovered.Success {
			reason := strings.TrimSpace(recovered.Reason)
			if reason == "" {
				reason = "site recovery returned no usable original content"
			}
			failures = append(failures, sourceAdapterFailure(adapter.Name, page.URL, reason))
			continue
		}
		page.Document, page.Content, page.Article = isolated.Document, isolated.Content, isolated.Article
		if recovered.ContentHTML != "" {
			result.ContentHTML = recovered.ContentHTML
		}
		if recovered.Author != "" || recovered.Publisher != "" {
			result.Author, result.Publisher = recovered.Author, recovered.Publisher
		}
		if recovered.PublishedAt != "" {
			result.PublishedAt = recovered.PublishedAt
		}
	}
	return result, failures
}

func (r *SiteAdapterRegistry) discoveryFor(raw string) *SiteAdapter {
	u, err := url.Parse(raw)
	if err != nil {
		return nil
	}
	for _, adapter := range r.matching(u) {
		if adapter.discovery != nil {
			return &adapter
		}
	}
	return nil
}

func (r *SiteAdapterRegistry) sourceURL(raw string) string {
	if adapter := r.discoveryFor(raw); adapter != nil && adapter.discovery.sourceURL != "" {
		return adapter.discovery.sourceURL
	}
	return raw
}

func (r *SiteAdapterRegistry) listingFor(u *url.URL) *listingProfile {
	for _, adapter := range r.matching(u) {
		if adapter.listing != nil {
			return adapter.listing
		}
	}
	return nil
}

func (r *SiteAdapterRegistry) configureRequest(req *http.Request) {
	for _, adapter := range r.matching(req.URL) {
		for name, values := range adapter.requestHeaders {
			req.Header[name] = append([]string(nil), values...)
		}
	}
}

type parsedSource struct {
	feed             *gofeed.Feed
	inlineOriginal   bool
	needsArticleDate bool
	failures         []types.SourceAdapterFailure
}

func (c *Connector) readSource(ctx context.Context, cli *client, cfg *Config, feedURL string) (*parsedSource, error) {
	data, err := cli.fetchSource(ctx, feedURL)
	if err != nil {
		return nil, err
	}
	source := &parsedSource{}
	if adapter := cli.adapterRegistry().discoveryFor(feedURL); adapter != nil {
		items, parseErr := adapter.discovery.parse(data, feedURL)
		if parseErr == nil {
			source.feed = &gofeed.Feed{Title: cfg.Publisher, Items: items}
			source.inlineOriginal, source.needsArticleDate = adapter.discovery.inlineOriginal, adapter.discovery.needsArticleDate
			return source, nil
		}
		u, _ := url.Parse(feedURL)
		source.failures = append(source.failures, sourceAdapterFailure(adapter.Name, u, parseErr.Error()))
		if adapter.discovery.sourceURL != "" {
			data, err = cli.fetchFeed(ctx, feedURL)
			if err != nil {
				return source, err
			}
		}
	}
	if feed, parseErr := gofeed.NewParser().Parse(bytes.NewReader(data)); parseErr == nil {
		source.feed = feed
		return source, nil
	}
	source.feed = &gofeed.Feed{Title: cfg.Publisher}
	source.needsArticleDate = true
	if entries, sitemapErr := parseSitemap(data); sitemapErr == nil && cfg.ArticlePathPrefix != "" {
		sitemapURL, _ := url.Parse(feedURL)
		for _, entry := range entries {
			if !sitemapArticleURL(entry.Loc, sitemapURL, cfg.ArticlePathPrefix) {
				continue
			}
			modified, valid := parseSiteDate(entry.LastMod)
			if !valid {
				source.feed.Items = append(source.feed.Items, &gofeed.Item{GUID: entry.Loc, Link: entry.Loc})
				continue
			}
			source.feed.Items = append(source.feed.Items, &gofeed.Item{GUID: entry.Loc, Link: entry.Loc, Updated: entry.LastMod, UpdatedParsed: &modified})
		}
		return source, nil
	}
	source.feed.Items, err = parseGenericListing(data, feedURL, cfg.ArticlePathPrefix)
	if err != nil {
		return source, fmt.Errorf("parse feed or article listing %s: %w", feedURL, err)
	}
	return source, nil
}

func (r *SiteAdapterRegistry) publicationDate(doc *goquery.Document, u *url.URL) string {
	for _, adapter := range r.matching(u) {
		if adapter.publishedAt != nil {
			if date := adapter.publishedAt(doc); date != "" {
				return date
			}
		}
	}
	return ""
}
